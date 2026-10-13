import 'language.dart';
import 'dart:async';
import 'dart:io';
import 'dart:typed_data';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:image_picker/image_picker.dart';
import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';
import 'core/client.dart';
import 'core/input.dart';
import 'core/local_speech.dart';
import 'design.dart';
import 'running_indicator.dart';
import 'attachment_page.dart' show ZoomableImage;

// Injectable native boundary. Recording uses PCM streaming, never a phone file.
abstract class InputServices {
  Future<List<DraftFile>> pickFiles();
  Future<DraftFile?> pickImage(bool camera);
  Future<Stream<Uint8List>> record();
  Future<void> stopRecording();
  Future<void> cancelRecording();
  Future<void> dispose();
}

abstract class LocalVoiceInputs {
  Future<void> prepareLocalVoice();
  Future<Stream<String>> startLocalVoice();
  Future<String> finishLocalVoice();
}

class NativeInputServices implements InputServices, LocalVoiceInputs {
  final _speech = LocalSpeechEngine.shared;
  StreamSubscription<String>? _localAudio;
  StreamController<String>? _localText;
  Completer<void>? _drained;
  int? _speechSession;
  int _localGeneration = 0;
  @override
  Future<void> prepareLocalVoice() => _speech.prepare();
  @override
  Future<Stream<String>> startLocalVoice() async {
    final generation = ++_localGeneration;
    final id = await _speech.start();
    if (generation != _localGeneration) {
      await _speech.cancel(id);
      throw const FormatException('录音已取消');
    }
    _speechSession = id;
    try {
      final audio = await record();
      if (generation != _localGeneration) {
        await cancelRecording();
        throw const FormatException('录音已取消');
      }
      final text = _localText = StreamController<String>();
      final done = _drained = Completer<void>();
      var accepted = 0;
      _localAudio = audio
          .asyncMap((bytes) {
            if (generation != _localGeneration) return Future<String>.value('');
            final remaining = 60 * 32000 - accepted;
            if (remaining <= 0) return Future<String>.value('');
            final take = bytes.length.clamp(0, remaining);
            accepted += take;
            return _speech.accept(id, Uint8List.sublistView(bytes, 0, take));
          })
          .listen(
            (value) {
              if (generation == _localGeneration && !text.isClosed) {
                text.add(value);
              }
            },
            onError: (Object e) {
              if (!text.isClosed) text.addError(e);
            },
            onDone: () {
              if (!done.isCompleted) done.complete();
            },
          );
      return text.stream;
    } catch (_) {
      await _speech.cancel(id);
      if (generation == _localGeneration) _speechSession = null;
      rethrow;
    }
  }

  @override
  Future<String> finishLocalVoice() async {
    final id = _speechSession, generation = _localGeneration;
    if (id == null) return '';
    final done = _drained, audio = _localAudio, output = _localText;
    await _recorder?.stop();
    await done?.future.timeout(const Duration(seconds: 10));
    final text = await _speech.finish(id);
    unawaited(
      audio?.cancel().catchError((Object _) {}) ?? Future<void>.value(),
    );
    unawaited(output?.close() ?? Future<void>.value());
    if (generation == _localGeneration) {
      _localAudio = null;
      _speechSession = null;
      _localText = null;
      _drained = null;
    }
    return text;
  }

  AudioRecorder? _recorder;
  @override
  Future<List<DraftFile>> pickFiles() async {
    try {
      final result = await FilePicker.pickFiles(
        allowMultiple: true,
        withData: false,
        withReadStream: true,
      );
      if (result == null) return [];
      if (result.files.length > 6) throw const FormatException('一次最多选择 6 个附件');
      final out = <DraftFile>[];
      int total = 0;
      for (final file in result.files) {
        if (file.size < 1 ||
            file.size > 8 << 20 ||
            total + file.size > 24 << 20) {
          throw const FormatException('单个附件最多 8 MiB，总计最多 24 MiB');
        }
        final stream =
            file.readStream ??
            (file.path == null ? null : File(file.path!).openRead());
        if (stream == null) throw const FileSystemException('无法读取所选文件');
        final bytes = BytesBuilder(copy: false);
        await for (final chunk in stream) {
          if (bytes.length + chunk.length > 8 << 20) {
            throw const FormatException('附件超过 8 MiB');
          }
          bytes.add(chunk);
        }
        final data = bytes.takeBytes();
        total += data.length;
        out.add(DraftFile(file.name, _mime(file.name), data));
      }
      return out;
    } finally {
      await FilePicker.clearTemporaryFiles();
    }
  }

  @override
  Future<DraftFile?> pickImage(bool camera) async {
    final image = await ImagePicker().pickImage(
      source: camera ? ImageSource.camera : ImageSource.gallery,
      maxWidth: 2048,
      maxHeight: 2048,
      imageQuality: 85,
    );
    if (image == null) return null;
    try {
      if (await image.length() > 8 << 20) {
        throw const FormatException('图片超过 8 MiB');
      }
      return DraftFile(
        image.name,
        _mime(image.name),
        await image.readAsBytes(),
      );
    } finally {
      // Delete only the picker's temporary copy, never an original library file.
      final temporary = await getTemporaryDirectory();
      final file = File(image.path);
      final root = await temporary.resolveSymbolicLinks();
      if (await file.exists()) {
        final path = await file.resolveSymbolicLinks();
        if (path.startsWith('$root${Platform.pathSeparator}')) {
          await file.delete();
        }
      }
    }
  }

  static String _mime(String name) {
    switch (name.split('.').last.toLowerCase()) {
      case 'jpg':
      case 'jpeg':
        return 'image/jpeg';
      case 'png':
        return 'image/png';
      case 'gif':
        return 'image/gif';
      case 'webp':
        return 'image/webp';
      case 'heic':
        return 'image/heic';
      default:
        return 'application/octet-stream';
    }
  }

  @override
  Future<Stream<Uint8List>> record() async {
    final generation = _localGeneration;
    final recorder = _recorder ??= AudioRecorder();
    if (!await recorder.hasPermission()) {
      throw const FormatException('请在系统设置允许 AgentDeck 使用麦克风');
    }
    if (generation != _localGeneration) throw const FormatException('录音已取消');
    final stream = await recorder.startStream(
      const RecordConfig(
        encoder: AudioEncoder.pcm16bits,
        sampleRate: 16000,
        numChannels: 1,
        streamBufferSize: 3200,
        autoGain: true,
        echoCancel: true,
        noiseSuppress: true,
      ),
    );
    if (generation != _localGeneration) {
      await recorder.cancel();
      throw const FormatException('录音已取消');
    }
    return stream;
  }

  @override
  Future<void> stopRecording() async {
    await _recorder?.stop();
  }

  @override
  Future<void> cancelRecording() async {
    _localGeneration++;
    final id = _speechSession,
        audio = _localAudio,
        text = _localText,
        done = _drained;
    _speechSession = null;
    _localAudio = null;
    _localText = null;
    _drained = null;
    await _recorder?.cancel();
    unawaited(
      audio?.cancel().catchError((Object _) {}) ?? Future<void>.value(),
    );
    if (id != null) await _speech.cancel(id);
    unawaited(text?.close() ?? Future<void>.value());
    if (done != null && !done.isCompleted) done.complete();
  }

  @override
  Future<void> dispose() async {
    await cancelRecording();
    await _recorder?.dispose();
  }
}

class InputComposer extends StatefulWidget {
  final AgentClient client;
  final String session, agent;
  final TextEditingController controller;
  final bool sending, running, canSend;
  final Future<bool> Function(TurnOptions, List<DraftFile>) onSend;
  final Future<bool> Function(TurnOptions, List<DraftFile>, String)? onDelivery;
  final VoidCallback? onStop;
  final InputServices? services;
  const InputComposer({
    super.key,
    required this.client,
    required this.session,
    required this.agent,
    required this.controller,
    required this.sending,
    required this.running,
    required this.canSend,
    required this.onSend,
    this.onStop,
    this.onDelivery,
    this.services,
  });
  @override
  State<InputComposer> createState() => _InputComposerState();
}

class _InputComposerState extends State<InputComposer>
    with WidgetsBindingObserver {
  late final InputServices services = widget.services ?? NativeInputServices();
  final files = <DraftFile>[];
  TurnOptions options = const TurnOptions();
  final textFocus = FocusNode();
  bool voiceMode = false;
  bool get canSendRunning =>
      widget.onDelivery != null &&
      ["codex", "claude", "opencode"].contains(widget.agent);
  bool picking = false,
      recording = false,
      starting = false,
      transcribing = false,
      finishing = false;
  bool localRecording = false, holdActive = false, slideCancel = false;
  StreamSubscription<String>? localSubscription;
  bool get canRecord =>
      !widget.sending &&
      !picking &&
      !starting &&
      !recording &&
      !finishing &&
      !transcribing &&
      (services is LocalVoiceInputs || widget.client.connected);
  Timer? timer;
  StreamSubscription<Uint8List>? subscription;
  BytesBuilder pcm = BytesBuilder(copy: false);
  Uint8List? retryAudio;
  int seconds = 0, recordGeneration = 0;
  Future<void>? previewTask;
  int previewBytes = 0;
  int committedVoiceBytes = 0;
  String committedVoiceText = '';
  TextEditingValue? voiceBase;
  String? voiceRendered;
  bool get enabled =>
      widget.client.connected &&
      !widget.sending &&
      !picking &&
      !starting &&
      !recording &&
      !transcribing &&
      !finishing;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    widget.controller.addListener(_changed);
    widget.client.addListener(_nativeChanged);
    _restore();
    if (services is LocalVoiceInputs) {
      unawaited(
        (services as LocalVoiceInputs).prepareLocalVoice().catchError(
          (Object _) {},
        ),
      );
    }
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  String generationLabel(BuildContext context) {
    final native = nativeGeneration;
    if (native == null || native.options.model.isEmpty) {
      return context.tr('会话模型 · 参数未提供');
    }
    final o = native.options;
    return '${o.model}${o.effort.isEmpty ? " · ${context.tr('Effort 未提供')}" : " · ${o.effort}"}${o.speed.isEmpty ? "" : " · ${context.tr(o.speed == 'fast' ? '快速' : '标准')}"}${native.source == 'last_turn' ? " · ${context.tr('上次使用')}" : ""}';
  }

  SessionGeneration? nativeGeneration;
  DateTime _generationRead = DateTime.fromMillisecondsSinceEpoch(0);
  bool _generationLoading = false;
  void _nativeChanged() {
    if (widget.client.connected &&
        DateTime.now().difference(_generationRead).inSeconds >= 15) {
      unawaited(_readNative());
    }
  }

  Future<void> _readNative() async {
    if (_generationLoading || !widget.client.connected) return;
    _generationLoading = true;
    _generationRead = DateTime.now();
    final session = widget.session;
    try {
      final value = await widget.client.sessionGeneration(session);
      if (mounted && widget.session == session) {
        setState(() => nativeGeneration = value);
      }
    } catch (_) {
    } finally {
      _generationLoading = false;
    }
  }

  Future<void> _restore() async {
    unawaited(_readNative());
    try {
      final value = await widget.client.loadTurnOptions(widget.session);
      if (mounted) setState(() => options = value);
    } catch (_) {
      /* Default remains the current CLI setting. */
    }
  }

  @override
  void didUpdateWidget(InputComposer old) {
    super.didUpdateWidget(old);
    if (old.session != widget.session) {
      nativeGeneration = null;
      options = const TurnOptions();
      _generationRead = DateTime.fromMillisecondsSinceEpoch(0);
      _restore();
      if (recording || starting || finishing || transcribing) _cancel();
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed && (recording || starting)) {
      _cancel();
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    widget.controller.removeListener(_changed);
    widget.client.removeListener(_nativeChanged);
    textFocus.dispose();
    timer?.cancel();
    recordGeneration++;
    subscription?.cancel();
    localSubscription?.cancel();
    services.cancelRecording().catchError((Object _) {});
    services.dispose().catchError((Object _) {});
    for (final file in files) {
      widget.client.removeDraftFile(file).catchError((Object _) {});
    }
    super.dispose();
  }

  void error(Object e) {
    if (mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(context.trError(e))));
    }
  }

  Future<void> _settings() async {
    final selected = await showModalBottomSheet<TurnOptions>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (_) => ModelSettings(
        client: widget.client,
        agent: widget.agent,
        session: widget.session,
        initial: options.inherited
            ? nativeGeneration?.options ?? const TurnOptions()
            : options,
      ),
    );
    if (selected != null && mounted) setState(() => options = selected);
  }

  Future<void> _pick() async {
    if (!enabled) return;
    textFocus.unfocus();
    FocusManager.instance.primaryFocus?.unfocus();
    setState(() => picking = true);
    final session = widget.session, agent = widget.agent;
    try {
      final choice = await showModalBottomSheet<int>(
        context: context,
        useSafeArea: true,
        requestFocus: true,
        showDragHandle: true,
        isScrollControlled: true,
        builder: (_) => SingleChildScrollView(
          child: _AttachmentSourceSheet(images: agent != 'workbuddy'),
        ),
      );
      if (choice == null ||
          !mounted ||
          widget.session != session ||
          widget.agent != agent) {
        return;
      }
      final picked = choice == 2
          ? await services.pickFiles()
          : [
              await services.pickImage(choice == 1),
            ].whereType<DraftFile>().toList();
      if (agent == 'workbuddy' && picked.any((file) => file.image)) {
        throw const FormatException('当前 Agent 接口未提供图片输入，请选择普通文件');
      }
      if (files.length + picked.length > 6 ||
          [...files, ...picked].fold<int>(0, (n, f) => n + f.bytes.length) >
              24 << 20) {
        throw const FormatException('一次最多 6 个附件，总计 24 MiB');
      }
      if (mounted && widget.session == session && widget.agent == agent) {
        setState(() => files.addAll(picked));
      }
    } catch (e) {
      error(e);
    } finally {
      if (mounted) setState(() => picking = false);
    }
  }

  void _setVoiceMode(bool value) {
    if (!canRecord) return;
    textFocus.unfocus();
    FocusManager.instance.primaryFocus?.unfocus();
    setState(() => voiceMode = value);
    if (!value) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted &&
            !voiceMode &&
            !picking &&
            ModalRoute.of(context)?.isCurrent == true) {
          textFocus.requestFocus();
        }
      });
    }
  }

  Widget _voiceHoldButton() => GestureDetector(
    key: const ValueKey('voice-hold'),
    behavior: HitTestBehavior.opaque,
    onTap: canRecord
        ? () => ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(context.tr('按住按钮说话，松开结束，上滑取消')),
              duration: Duration(seconds: 2),
            ),
          )
        : null,
    onLongPressStart: (_) {
      if (!canRecord) return;
      holdActive = true;
      slideCancel = false;
      unawaited(_start());
    },
    onLongPressMoveUpdate: (details) {
      if (!holdActive) return;
      final cancel = details.offsetFromOrigin.dy < -56;
      if (cancel != slideCancel) {
        setState(() => slideCancel = cancel);
      }
    },
    onLongPressEnd: (_) => _releaseHold(),
    onLongPressCancel: () => _releaseHold(cancel: true),
    child: Semantics(
      label: context.tr('按住说话'),
      button: true,
      enabled: canRecord || holdActive,
      child: Container(
        width: double.infinity,
        constraints: const BoxConstraints(minHeight: 52),
        padding: const EdgeInsets.symmetric(vertical: 12, horizontal: 16),
        decoration: BoxDecoration(
          color: slideCancel
              ? const Color(0xffffeeee)
              : holdActive
              ? DeckColors.ink
              : DeckColors.paper,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: slideCancel ? Colors.red : DeckColors.line),
        ),
        alignment: Alignment.center,
        child: Text(
          starting
              ? context.tr('准备中…')
              : finishing
              ? context.tr('正在校正…')
              : transcribing
              ? context.tr('正在识别…')
              : recording
              ? (slideCancel ? context.tr('松开取消录音') : context.tr('松开结束录音'))
              : context.tr('按住说话'),
          style: TextStyle(
            fontSize: 14,
            fontWeight: FontWeight.w600,
            color: slideCancel
                ? Colors.red
                : holdActive
                ? Colors.white
                : canRecord
                ? DeckColors.ink
                : DeckColors.muted,
          ),
        ),
      ),
    ),
  );

  Future<void> _send() async {
    try {
      final mode =
          widget.running && canSendRunning && widget.agent != 'opencode'
          ? 'queue'
          : 'send';
      final sent = widget.onDelivery == null
          ? await widget.onSend(options, List.of(files))
          : await widget.onDelivery!(options, List.of(files), mode);
      if (sent && mounted) {
        setState(() => files.clear());
      }
    } catch (e) {
      error(e);
    }
  }

  void _releaseHold({bool cancel = false}) {
    if (!holdActive) return;
    holdActive = false;
    if (cancel || slideCancel || starting) {
      unawaited(_cancel());
    } else if (recording) {
      unawaited(_finish());
    }
  }

  Future<void> _start() async {
    if (!canRecord) return;
    final generation = ++recordGeneration;
    setState(() => starting = true);
    try {
      if (services is LocalVoiceInputs) {
        final local = services as LocalVoiceInputs;
        await local.prepareLocalVoice();
        if (!mounted || generation != recordGeneration) return;
        voiceBase = widget.controller.value;
        voiceRendered = widget.controller.text;
        final stream = await local.startLocalVoice();
        if (!mounted || generation != recordGeneration) {
          await services.cancelRecording();
          return;
        }
        seconds = 0;
        retryAudio = null;
        localRecording = true;
        setState(() => recording = true);
        localSubscription = stream.listen(
          (text) {
            if (mounted &&
                recording &&
                !finishing &&
                generation == recordGeneration) {
              _applyVoiceText(text);
            }
          },
          onError: (Object e) {
            if (generation == recordGeneration) {
              error(e);
              _cancel();
            }
          },
        );
        timer = Timer.periodic(const Duration(seconds: 1), (_) {
          if (mounted) setState(() => seconds++);
          if (seconds >= 60) _finish();
        });
        return;
      }
      final capabilities = await widget.client.inputCapabilities(
        widget.agent,
        widget.session,
        media: false,
      );
      if (!capabilities.voice) {
        throw const FormatException('工作站尚未启用语音转写，请配置 voice-command-file');
      }
      if (!mounted || generation != recordGeneration) return;
      final stream = await services.record();
      if (!mounted || generation != recordGeneration) {
        await services.cancelRecording();
        return;
      }
      pcm = BytesBuilder(copy: false);
      seconds = 0;
      retryAudio = null;
      voiceBase = widget.controller.value;
      voiceRendered = widget.controller.text;
      previewBytes = 0;
      committedVoiceBytes = 0;
      committedVoiceText = '';
      setState(() => recording = true);
      subscription = stream.listen(
        (chunk) {
          if (!recording) return;
          final available = 60 * 32000 - pcm.length;
          if (available > 0) {
            pcm.add(
              chunk.length > available
                  ? Uint8List.sublistView(chunk, 0, available)
                  : chunk,
            );
          }
          if (pcm.length >= 60 * 32000) _finish();
        },
        onError: (Object e) {
          error(e);
          _cancel();
        },
      );
      timer = Timer.periodic(const Duration(seconds: 1), (_) {
        if (mounted) setState(() => seconds++);
        if (seconds % 2 == 0) _schedulePreview(generation);
        if (seconds >= 60) _finish();
      });
    } catch (e) {
      if (generation == recordGeneration) error(e);
    } finally {
      if (mounted && generation == recordGeneration) {
        setState(() => starting = false);
      }
    }
  }

  Future<void> _cancel() async {
    recordGeneration++;
    holdActive = false;
    slideCancel = false;
    localRecording = false;
    unawaited(localSubscription?.cancel() ?? Future<void>.value());
    localSubscription = null;
    timer?.cancel();
    if (mounted) {
      setState(() {
        recording = false;
        starting = false;
        finishing = false;
        retryAudio = null;
      });
    }
    final current = subscription;
    subscription = null;
    pcm.clear();
    if (voiceBase != null && widget.controller.text == voiceRendered) {
      widget.controller.value = voiceBase!;
    }
    voiceBase = null;
    voiceRendered = null;
    await services.cancelRecording().catchError((Object _) {});
    unawaited(current?.cancel().catchError((Object _) {}) ?? Future.value());
  }

  Future<void> _finish() async {
    if (!recording || finishing) return;
    final generation = recordGeneration;
    timer?.cancel();
    if (mounted) setState(() => finishing = true);
    try {
      if (localRecording) {
        final text = await (services as LocalVoiceInputs).finishLocalVoice();
        unawaited(
          localSubscription?.cancel().catchError((Object _) {}) ??
              Future<void>.value(),
        );
        localSubscription = null;
        if (!mounted || generation != recordGeneration) return;
        if (text.isNotEmpty) {
          _applyVoiceText(text);
        } else {
          error(const FormatException('未识别到文字，请再按住说话'));
        }
        voiceBase = null;
        voiceRendered = null;
        localRecording = false;
        setState(() => recording = false);
        return;
      }
      await services.stopRecording();
      final current = subscription;
      subscription = null;
      recording = false;
      unawaited(current?.cancel().catchError((Object _) {}) ?? Future.value());
      await previewTask;
      if (!mounted || generation != recordGeneration) return;
      final wav = recordingWav(pcm.takeBytes());
      if (!mounted) return;
      setState(() {
        recording = false;
        retryAudio = wav;
      });
      await _transcribe();
    } catch (e) {
      if (generation == recordGeneration) {
        error(e);
        if (localRecording) {
          await services.cancelRecording().catchError((Object _) {});
          unawaited(localSubscription?.cancel() ?? Future<void>.value());
          localSubscription = null;
          localRecording = false;
          voiceBase = null;
          voiceRendered = null;
        }
        if (mounted) setState(() => recording = false);
      }
    } finally {
      if (mounted && generation == recordGeneration) {
        setState(() => finishing = false);
      }
    }
  }

  Future<void> _transcribe() async {
    final audio = retryAudio;
    if (audio == null || transcribing) return;
    setState(() => transcribing = true);
    final generation = recordGeneration;
    try {
      final text = await widget.client.transcribeRecording(
        widget.session,
        audio,
      );
      if (!mounted || generation != recordGeneration) return;
      _applyVoiceText(text);
      retryAudio = null;
      voiceBase = null;
      voiceRendered = null;
    } catch (e) {
      if (generation == recordGeneration) error(e);
    } finally {
      if (mounted) setState(() => transcribing = false);
    }
  }

  void _applyVoiceText(String text) {
    if (text.isEmpty) return;
    final controller = widget.controller;
    // Never replace unrelated edits made by navigation or another action.
    if (voiceRendered != null && controller.text != voiceRendered) return;
    final base = voiceBase ?? controller.value;
    final selection = base.selection;
    final start = selection.isValid ? selection.start : base.text.length;
    final end = selection.isValid ? selection.end : start;
    final insert =
        (start > 0 && !base.text.substring(0, start).endsWith(' ') ? ' ' : '') +
        text;
    controller.value = TextEditingValue(
      text: base.text.replaceRange(start, end, insert),
      selection: TextSelection.collapsed(offset: start + insert.length),
    );
    voiceRendered = controller.text;
  }

  void _schedulePreview(int generation) {
    if (!recording ||
        finishing ||
        previewTask != null ||
        pcm.length - committedVoiceBytes < 32000) {
      return;
    }
    final bytes = pcm.toBytes();
    // Keep live work bounded even for a 60-second recording.
    final end = (committedVoiceBytes + 6 * 32000).clamp(0, bytes.length);
    if (end - previewBytes < 32000) return;
    final complete = end - committedVoiceBytes == 6 * 32000;
    final audio = Uint8List.sublistView(bytes, committedVoiceBytes, end);
    previewBytes = end;
    previewTask = _preview(recordingWav(audio), generation, end, complete);
  }

  Future<void> _preview(
    Uint8List wav,
    int generation,
    int end,
    bool complete,
  ) async {
    try {
      final text = await widget.client.transcribeRecording(
        widget.session,
        wav,
        preview: true,
      );
      if (mounted &&
          recording &&
          !finishing &&
          generation == recordGeneration) {
        final separator =
            committedVoiceText.isNotEmpty &&
                text.isNotEmpty &&
                RegExp(r'[a-zA-Z0-9]$').hasMatch(committedVoiceText) &&
                RegExp(r'^[a-zA-Z0-9]').hasMatch(text)
            ? ' '
            : '';
        final hypothesis = '$committedVoiceText$separator$text';
        _applyVoiceText(hypothesis);
        if (complete) {
          committedVoiceBytes = end;
          committedVoiceText = hypothesis;
        }
      }
    } catch (_) {
      if (generation == recordGeneration) previewBytes = committedVoiceBytes;
      // Keep recording; final transcription remains available after a dropout.
    } finally {
      previewTask = null;
    }
  }

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.fromLTRB(12, 4, 6, 4),
    decoration: BoxDecoration(
      color: Colors.white,
      borderRadius: BorderRadius.circular(16),
      border: Border.all(color: DeckColors.line, width: .8),
    ),
    child: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (files.isNotEmpty)
          SizedBox(
            height: 68,
            child: ListView.separated(
              scrollDirection: Axis.horizontal,
              itemCount: files.length,
              separatorBuilder: (_, _) => const SizedBox(width: 6),
              itemBuilder: (_, i) {
                final f = files[i];
                return Container(
                  width: 172,
                  padding: const EdgeInsets.only(left: 6),
                  decoration: BoxDecoration(
                    color: DeckColors.paper,
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: Row(
                    children: [
                      f.image
                          ? GestureDetector(
                              onTap: () => Navigator.push(
                                context,
                                MaterialPageRoute<void>(
                                  builder: (_) => Scaffold(
                                    appBar: AppBar(title: Text(f.name)),
                                    body: ZoomableImage(bytes: f.bytes),
                                  ),
                                ),
                              ),
                              child: ClipRRect(
                                borderRadius: BorderRadius.circular(5),
                                child: Image.memory(
                                  f.bytes,
                                  width: 38,
                                  height: 44,
                                  fit: BoxFit.cover,
                                  cacheWidth: 100,
                                  errorBuilder: (_, _, _) =>
                                      const Icon(Icons.broken_image_outlined),
                                ),
                              ),
                            )
                          : const Icon(
                              Icons.insert_drive_file_outlined,
                              size: 24,
                            ),
                      const SizedBox(width: 6),
                      Expanded(
                        child: Column(
                          mainAxisAlignment: MainAxisAlignment.center,
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              f.name,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: const TextStyle(fontSize: 12),
                            ),
                            Text(
                              f.sizeLabel,
                              style: const TextStyle(
                                fontSize: 10,
                                color: DeckColors.muted,
                              ),
                            ),
                          ],
                        ),
                      ),
                      IconButton(
                        tooltip: context.tr('移除 {0}', [f.name]),
                        onPressed: enabled
                            ? () {
                                setState(() => files.removeAt(i));
                                widget.client
                                    .removeDraftFile(f)
                                    .catchError(error);
                              }
                            : null,
                        icon: const Icon(Icons.close, size: 15),
                      ),
                    ],
                  ),
                );
              },
            ),
          ),
        if (recording || starting || finishing || transcribing)
          Row(
            children: [
              const SizedBox(width: 4),
              const RunningIndicator(),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  transcribing
                      ? context.tr('正在转写，文字将回到输入框…')
                      : starting
                      ? context.tr('准备麦克风…')
                      : finishing
                      ? (localRecording
                            ? context.tr('正在校正识别结果…')
                            : context.tr('正在收尾…'))
                      : slideCancel
                      ? context.tr('松开取消')
                      : context.tr('松开结束 · 上滑取消 · {0}s', [seconds]),
                  style: const TextStyle(fontSize: 12),
                ),
              ),
              if (recording || starting)
                IconButton(
                  tooltip: context.tr('取消录音'),
                  onPressed: _cancel,
                  icon: const Icon(Icons.close, size: 18),
                ),
            ],
          ),
        if (retryAudio != null && !transcribing && !recording)
          Row(
            children: [
              Expanded(
                child: TextButton.icon(
                  onPressed: widget.client.connected ? _transcribe : null,
                  icon: const Icon(Icons.refresh, size: 16),
                  label: Text(context.tr('重试语音转写')),
                ),
              ),
              IconButton(
                tooltip: context.tr('丢弃录音'),
                onPressed: () => setState(() => retryAudio = null),
                icon: const Icon(Icons.close, size: 18),
              ),
            ],
          ),
        if (!voiceMode || widget.controller.text.isNotEmpty)
          TextField(
            controller: widget.controller,
            focusNode: textFocus,
            readOnly: voiceMode,
            onTap: voiceMode ? () => _setVoiceMode(false) : null,
            minLines: 1,
            maxLines: 5,
            enabled:
                enabled ||
                (services is LocalVoiceInputs &&
                    !widget.sending &&
                    !picking &&
                    !starting &&
                    !recording &&
                    !finishing),
            style: const TextStyle(fontSize: 14),
            decoration: InputDecoration(
              hintText: context.tr('发消息给 {0}', [
                widget.agent == "codex"
                    ? "Codex"
                    : widget.agent == "claude"
                    ? "Claude"
                    : widget.agent,
              ]),
              filled: false,
              enabledBorder: InputBorder.none,
              focusedBorder: InputBorder.none,
              border: InputBorder.none,
              isDense: true,
              contentPadding: const EdgeInsets.symmetric(vertical: 10),
              hintStyle: const TextStyle(color: DeckColors.muted, fontSize: 14),
            ),
          ),
        if (voiceMode) _voiceHoldButton(),

        Row(
          children: [
            Expanded(
              child: Align(
                alignment: Alignment.centerLeft,
                child: TextButton.icon(
                  onPressed: enabled ? _settings : null,
                  icon: const Icon(Icons.tune, size: 15),
                  label: Text(
                    generationLabel(context),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(fontSize: 11),
                  ),
                  style: TextButton.styleFrom(
                    padding: const EdgeInsets.symmetric(horizontal: 5),
                    foregroundColor: DeckColors.muted,
                  ),
                ),
              ),
            ),
            IconButton(
              tooltip: context.tr('添加图片或文件'),
              onPressed: enabled ? _pick : null,
              icon: const Icon(Icons.add, size: 21),
            ),
            IconButton(
              tooltip: voiceMode ? context.tr('键盘输入') : context.tr('语音输入'),
              onPressed: canRecord ? () => _setVoiceMode(!voiceMode) : null,
              icon: Icon(
                voiceMode ? Icons.keyboard_alt_outlined : Icons.mic_none,
                size: 21,
              ),
            ),
            if (widget.running && widget.onStop != null)
              IconButton.filled(
                tooltip: context.tr('停止任务'),
                style: IconButton.styleFrom(
                  backgroundColor: DeckColors.ink,
                  foregroundColor: Colors.white,
                  disabledBackgroundColor: DeckColors.panel,
                  disabledForegroundColor: DeckColors.muted,
                ),
                onPressed: widget.client.connected ? widget.onStop : null,
                icon: Icon(
                  Icons.stop_rounded,
                  size: 20,
                  color: widget.client.connected
                      ? Colors.white
                      : DeckColors.muted,
                ),
              ),
            IconButton.filled(
              tooltip: context.tr(
                widget.running && canSendRunning && widget.agent != 'opencode'
                    ? '排队发送'
                    : '发送消息',
              ),
              style: IconButton.styleFrom(
                backgroundColor: DeckColors.ink,
                foregroundColor: Colors.white,
                disabledBackgroundColor: DeckColors.panel,
                disabledForegroundColor: DeckColors.muted,
              ),
              onPressed:
                  enabled &&
                      widget.canSend &&
                      (!widget.running || canSendRunning) &&
                      (widget.controller.text.trim().isNotEmpty ||
                          files.isNotEmpty)
                  ? _send
                  : null,
              icon: widget.sending
                  ? const RunningIndicator()
                  : Icon(
                      Icons.arrow_upward,
                      size: 20,
                      color:
                          enabled &&
                              widget.canSend &&
                              (!widget.running || canSendRunning) &&
                              (widget.controller.text.trim().isNotEmpty ||
                                  files.isNotEmpty)
                          ? Colors.white
                          : DeckColors.muted,
                    ),
            ),
          ],
        ),
      ],
    ),
  );
}

// Selecting a phone file is local. Remote capability discovery belongs to Send,
// where unsupported images can be rejected without blocking the OS picker.
class _AttachmentSourceSheet extends StatelessWidget {
  final bool images;
  const _AttachmentSourceSheet({required this.images});
  @override
  Widget build(BuildContext context) => SafeArea(
    top: false,
    child: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(20, 0, 20, 12),
          child: Text(
            context.tr('添加附件'),
            style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600),
          ),
        ),
        ListTile(
          leading: const Icon(Icons.photo_outlined),
          title: Text(context.tr('照片')),
          enabled: images,
          subtitle: !images
              ? Text(context.tr('当前会话暂不支持图片'), style: TextStyle(fontSize: 11))
              : null,
          onTap: images ? () => Navigator.pop(context, 0) : null,
        ),
        ListTile(
          leading: const Icon(Icons.camera_alt_outlined),
          title: Text(context.tr('拍照')),
          enabled: images,
          onTap: images ? () => Navigator.pop(context, 1) : null,
        ),
        ListTile(
          leading: const Icon(Icons.attach_file),
          title: Text(context.tr('文件')),
          subtitle: Text(
            context.tr('选择手机上的文件'),
            style: TextStyle(fontSize: 11),
          ),
          onTap: () => Navigator.pop(context, 2),
        ),
        const SizedBox(height: 8),
      ],
    ),
  );
}

class ModelSettings extends StatefulWidget {
  final AgentClient client;
  final String agent, session;
  final TurnOptions initial;
  const ModelSettings({
    super.key,
    required this.client,
    required this.agent,
    required this.session,
    required this.initial,
  });
  @override
  State<ModelSettings> createState() => _ModelSettingsState();
}

class _ModelSettingsState extends State<ModelSettings> {
  late Future<InputCatalog> catalog = widget.client.inputCatalog(
    widget.agent,
    widget.session,
  );
  late TurnOptions options = widget.initial;
  bool saving = false;
  Future<void> _save() async {
    setState(() => saving = true);
    try {
      await widget.client.saveTurnOptions(widget.session, options);
      if (mounted) Navigator.pop(context, options);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(context.tr('选择保存失败：{0}', [context.trError(e)])),
          ),
        );
      }
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  @override
  Widget build(BuildContext context) => SizedBox(
    height: MediaQuery.sizeOf(context).height * .75,
    child: Padding(
      padding: const EdgeInsets.fromLTRB(20, 18, 20, 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            context.tr('模型与思考强度'),
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 5),
          Text(
            context.tr('会话模型与参数'),
            style: TextStyle(fontSize: 12, color: DeckColors.muted),
          ),
          const SizedBox(height: 10),
          Expanded(
            child: FutureBuilder<InputCatalog>(
              future: catalog,
              builder: (context, snapshot) {
                if (snapshot.connectionState != ConnectionState.done) {
                  return const Center(child: RunningIndicator());
                }
                if (snapshot.hasError) {
                  return Center(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          context.tr('读取工作站模型失败\n{0}', [
                            context.trError(snapshot.error),
                          ]),
                          textAlign: TextAlign.center,
                        ),
                        TextButton(
                          onPressed: () => setState(
                            () => catalog = widget.client.inputCatalog(
                              widget.agent,
                              widget.session,
                            ),
                          ),
                          child: Text(context.tr('重试')),
                        ),
                      ],
                    ),
                  );
                }
                final models = snapshot.data!.models;
                final detail = snapshot.data!.detail;
                final selected = models
                    .where((m) => m.id == options.model)
                    .firstOrNull;
                final valid =
                    selected != null &&
                    (options.effort.isEmpty ||
                        selected.efforts.contains(options.effort));
                return Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Expanded(
                      child: ListView(
                        children: [
                          if (models.isEmpty)
                            Padding(
                              padding: const EdgeInsets.symmetric(vertical: 12),
                              child: Text(
                                detail.isEmpty
                                    ? context.tr('当前接口未提供模型设置。')
                                    : context.tr(detail),
                                style: const TextStyle(
                                  fontSize: 13,
                                  color: DeckColors.muted,
                                ),
                              ),
                            ),
                          for (final model in models)
                            ListTile(
                              contentPadding: EdgeInsets.zero,
                              leading: Icon(
                                options.model == model.id
                                    ? Icons.radio_button_checked
                                    : Icons.radio_button_off,
                                size: 20,
                              ),
                              title: Text(
                                model.name,
                                style: const TextStyle(fontSize: 14),
                              ),
                              subtitle: Text(
                                model.id,
                                style: const TextStyle(
                                  fontSize: 11,
                                  color: DeckColors.muted,
                                ),
                              ),
                              onTap: () => setState(
                                () => options = TurnOptions(
                                  model: model.id,
                                  effort: model.defaultEffort,
                                  speed:
                                      options.speed == 'fast' &&
                                          model.fastTier.isEmpty
                                      ? ''
                                      : options.speed,
                                ),
                              ),
                            ),
                        ],
                      ),
                    ),
                    if (selected != null && selected.efforts.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Text(
                        context.tr(
                          widget.agent == 'claude'
                              ? 'Claude Effort（原生目录）'
                              : widget.agent == 'codex'
                              ? 'Codex Reasoning Effort'
                              : snapshot.data!.effortLabel,
                        ),
                        style: const TextStyle(fontSize: 13),
                      ),
                      const SizedBox(height: 6),
                      Wrap(
                        spacing: 6,
                        runSpacing: 2,
                        children: [
                          for (final effort in selected.efforts)
                            ChoiceChip(
                              label: Text(effort),
                              selected: options.effort == effort,
                              onSelected: (_) => setState(
                                () => options = TurnOptions(
                                  model: options.model,
                                  effort: effort,
                                  speed: options.speed,
                                ),
                              ),
                            ),
                        ],
                      ),
                    ],
                    if (selected != null &&
                        (selected.fastTier.isNotEmpty ||
                            widget.agent == 'codex' ||
                            widget.agent == 'claude')) ...[
                      const SizedBox(height: 8),
                      Text(context.tr('速度（不改变 Effort）')),
                      Wrap(
                        spacing: 6,
                        children: [
                          for (final speed in [
                            'standard',
                            if (selected.fastTier.isNotEmpty) 'fast',
                          ])
                            ChoiceChip(
                              label: Text(
                                context.tr(speed == 'fast' ? '快速' : '标准'),
                              ),
                              selected: options.speed == speed,
                              onSelected: (_) => setState(
                                () => options = TurnOptions(
                                  model: options.model,
                                  effort: options.effort,
                                  speed: speed,
                                ),
                              ),
                            ),
                        ],
                      ),
                      if (options.speed == 'fast')
                        Text(
                          context.tr('原生快速模式可能增加用量或费用，是否可用由 Agent 和账号决定。'),
                          style: const TextStyle(
                            fontSize: 11,
                            color: DeckColors.muted,
                          ),
                        ),
                    ],
                    if (!valid && !options.inherited)
                      Padding(
                        padding: EdgeInsets.symmetric(vertical: 8),
                        child: Text(context.tr('之前选择的模型已不可用，请重新选择。')),
                      ),
                    const SizedBox(height: 14),
                    FilledButton(
                      onPressed: models.isEmpty
                          ? () => Navigator.pop(context)
                          : valid && !saving
                          ? _save
                          : null,
                      child: saving
                          ? const RunningIndicator()
                          : Text(context.tr(models.isEmpty ? '关闭' : '保存')),
                    ),
                  ],
                );
              },
            ),
          ),
        ],
      ),
    ),
  );
}
