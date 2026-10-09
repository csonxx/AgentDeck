import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:isolate';
import 'package:crypto/crypto.dart';
import 'package:flutter/services.dart';
import 'package:flutter/foundation.dart' show compute;
import 'package:path_provider/path_provider.dart';
import 'package:sherpa_onnx/sherpa_onnx.dart' as asr;
import 'speech_recognizer.dart';

// Public model weights may be cached. Audio and hypotheses never touch disk or
// the network. Native inference runs exclusively in the worker isolate.
class LocalSpeechEngine {
  static final shared = LocalSpeechEngine();
  Future<void>? _loading;
  Isolate? _worker;
  ReceivePort? _receive;
  SendPort? _send;
  final _pending = <int, Completer<dynamic>>{};
  int _requestId = 0, _sessionId = 0;
  bool _ready = false;

  Future<void> prepare() => _loading ??= _prepare().catchError((Object e) {
    _loading = null;
    throw e;
  });

  Future<void> _prepare() async {
    if (_ready) return;
    final manifest =
        jsonDecode(await rootBundle.loadString('assets/asr/manifest.json'))
            as Map<String, dynamic>;
    final base = await getApplicationSupportDirectory();
    final folder = Directory(
      '${base.path}/asr/${manifest['revision']}-${manifest['variant']}',
    );
    await folder.create(recursive: true);
    for (final entry in manifest['files'] as List) {
      final name = entry['asset'] as String;
      final file = File('${folder.path}/$name');
      // Model copies are immutable, keyed by revision. A newly copied asset is
      // checked before atomic replacement; the packaged manifest pins its hash.
      if (await compute(_verifyModelFile, [
        file.path,
        entry['bytes'],
        entry['sha256'],
      ])) {
        continue;
      }
      final data = await rootBundle.load('assets/asr/$name');
      final bytes = data.buffer.asUint8List(
        data.offsetInBytes,
        data.lengthInBytes,
      );
      if (!await compute(_verifyModelBytes, [bytes, entry['sha256']])) {
        throw const FormatException('手机语音模型校验失败');
      }
      final temp = File('${file.path}.part');
      await temp.writeAsBytes(bytes, flush: true);
      await temp.rename(file.path);
    }
    final receive = _receive = ReceivePort();
    final initialized = Completer<void>();
    receive.listen((dynamic message) {
      if (message is SendPort) {
        _send = message;
        return;
      }
      if (message == null || message is List) {
        _ready = false;
        _loading = null;
        _send = null;
        if (!initialized.isCompleted) {
          initialized.completeError(const FormatException('手机语音服务未启动'));
        }
        for (final c in _pending.values) {
          c.completeError(const FormatException('手机语音服务已停止，请重试'));
        }
        _pending.clear();
        receive.close();
        return;
      }
      final v = message as Map;
      if (v['ready'] == true) {
        _ready = true;
        initialized.complete();
      } else if (v['startup_error'] == true) {
        initialized.completeError(const FormatException('无法加载手机语音模型'));
      } else {
        final c = _pending.remove(v['id']);
        if (v['error'] == true) {
          c?.completeError(const FormatException('手机语音识别失败，请重试'));
        } else {
          c?.complete(v['text']);
        }
      }
    });
    try {
      _worker = await Isolate.spawn(
        _speechWorker,
        [receive.sendPort, folder.path],
        onExit: receive.sendPort,
        onError: receive.sendPort,
      );
      await initialized.future.timeout(const Duration(seconds: 30));
    } catch (_) {
      _worker?.kill();
      _worker = null;
      receive.close();
      _receive = null;
      _send = null;
      rethrow;
    }
  }

  Future<String> _call(String method, int session, [Uint8List? bytes]) async {
    final id = ++_requestId, c = Completer<dynamic>();
    _pending[id] = c;
    _send!.send({
      'id': id,
      'method': method,
      'session': session,
      if (bytes != null) 'pcm': TransferableTypedData.fromList([bytes]),
    });
    try {
      return (await c.future.timeout(const Duration(seconds: 10))) as String? ??
          '';
    } finally {
      _pending.remove(id);
    }
  }

  Future<int> start() async {
    await prepare();
    final id = ++_sessionId;
    await _call('start', id);
    return id;
  }

  Future<String> accept(int session, Uint8List pcm) =>
      _call('pcm', session, pcm);
  Future<String> finish(int session) => _call('finish', session);
  Future<void> cancel(int session) async {
    if (_ready) await _call('cancel', session);
  }

  Future<void> dispose() async {
    _worker?.kill();
    _worker = null;
    _receive?.close();
    _receive = null;
    _send = null;
    _ready = false;
    _loading = null;
    for (final c in _pending.values) {
      c.completeError(StateError('Voice worker closed'));
    }
    _pending.clear();
  }
}

bool _verifyModelBytes(List<dynamic> values) =>
    sha256.convert(values[0] as Uint8List).toString() == values[1];
bool _verifyModelFile(List<dynamic> values) {
  final file = File(values[0] as String);
  return file.existsSync() &&
      file.lengthSync() == values[1] &&
      sha256.convert(file.readAsBytesSync()).toString() == values[2];
}

void _speechWorker(List<dynamic> args) {
  final reply = args[0] as SendPort, path = args[1] as String;
  ClientSpeechRecognizer recognizer;
  try {
    asr.initBindings();
    recognizer = ClientSpeechRecognizer(path);
  } catch (_) {
    reply.send({'startup_error': true});
    return;
  }
  final commands = ReceivePort();
  reply.send(commands.sendPort);
  reply.send({'ready': true});
  int active = 0;
  commands.listen((dynamic message) {
    final v = message as Map;
    final session = v['session'] as int;
    try {
      var result = '';
      if (v['method'] == 'start') {
        recognizer.start();
        active = session;
      } else if (session == active) {
        if (v['method'] == 'pcm') {
          final bytes = (v['pcm'] as TransferableTypedData)
              .materialize()
              .asUint8List();
          result = recognizer.accept(bytes);
        } else if (v['method'] == 'finish') {
          result = recognizer.finish();
          active = 0;
        } else if (v['method'] == 'cancel') {
          recognizer.cancel();
          active = 0;
        }
      }
      reply.send({'id': v['id'], 'text': result});
    } catch (_) {
      recognizer.cancel();
      active = 0;
      reply.send({'id': v['id'], 'error': true});
    }
  });
}
