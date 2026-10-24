import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'client.dart';
import 'conversation_presentation.dart';
import 'input.dart';
import 'model.dart';

class OutgoingMessage {
  final String id,
      binding,
      originalSession,
      agent,
      project,
      title,
      text,
      delivery;
  final TurnOptions options;
  final int created;
  String session, source, commandID, state, stage, error;
  bool managed, locked, definitelyNotSent;
  int attempts = 0, retryAfter = 0;
  List<Map<String, dynamic>> fileInfo;
  List<DraftFile>? files;
  List<String> uploads;
  InputReceipt receipt;
  int? _sendStarted;
  Map<String, int>? _sendTiming;
  bool _runningReported = false;
  OutgoingMessage({
    required this.id,
    required this.binding,
    required this.originalSession,
    required this.session,
    required this.agent,
    required this.project,
    required this.title,
    required this.text,
    required this.delivery,
    required this.options,
    required this.created,
    required this.source,
    required this.managed,
    required this.receipt,
    required this.commandID,
    this.state = 'pending',
    this.stage = '等待发送',
    this.error = '',
    this.locked = false,
    this.definitelyNotSent = false,
    this.fileInfo = const [],
    this.uploads = const [],
    this.files,
  });
  bool get pending => const [
    'pending',
    'preparing',
    'uploading',
    'sending',
    'retrying',
  ].contains(state);
  bool get running =>
      const ['preparing', 'uploading', 'sending'].contains(state);
  bool get needsAttention => state == 'failed' || state == 'uncertain';
  bool get showReceipt => state != 'done' && state != 'queued';
  bool matches(String id) => session == id || originalSession == id;
  Map<String, dynamic> toJson() => {
    'version': 1,
    'id': id,
    'binding': binding,
    'original_session': originalSession,
    'session': session,
    'agent': agent,
    'project': project,
    'title': title,
    'text': text,
    'delivery': delivery,
    'options': options.toJson(),
    'created': created,
    'source': source,
    'managed': managed,
    'command_id': commandID,
    'state': state,
    'stage': stage,
    'error': error,
    'locked': locked,
    'not_sent': definitelyNotSent,
    'attempts': attempts,
    'retry_after': retryAfter,
    'uploads': uploads,
    'files': fileInfo,
    'receipt': {
      'id': receipt.id,
      'text': receipt.text,
      'match': receipt.matchText,
      'cursor': receipt.cursor,
      'baseline': receipt.baselineUpdated,
      'native': receipt.nativeMessageID,
      'phase': receipt.phase.name,
    },
  };
  factory OutgoingMessage.fromJson(Map<String, dynamic> v) {
    final r = Map<String, dynamic>.from(v['receipt'] as Map);
    final receipt =
        InputReceipt(
            id: r['id'],
            text: r['text'],
            matchText: r['match'],
            cursor: r['cursor'],
            baselineUpdated: r['baseline'],
          )
          ..nativeMessageID = r['native'] as String?
          ..phase = InputDeliveryPhase.values.byName(r['phase']);
    return OutgoingMessage(
        id: v['id'],
        binding: v['binding'],
        originalSession: v['original_session'],
        session: v['session'],
        agent: v['agent'],
        project: v['project'],
        title: v['title'],
        text: v['text'],
        delivery: v['delivery'],
        options: TurnOptions.fromJson(Map<String, dynamic>.from(v['options'])),
        created: v['created'],
        source: v['source'],
        managed: v['managed'],
        commandID: v['command_id'],
        state: v['state'],
        stage: v['stage'],
        error: v['error'],
        locked: v['locked'] == true,
        definitelyNotSent: v['not_sent'] == true,
        receipt: receipt,
        fileInfo: (v['files'] as List)
            .map((x) => Map<String, dynamic>.from(x))
            .toList(),
        uploads: List<String>.from(v['uploads']),
      )
      ..attempts = v['attempts'] as int? ?? 0
      ..retryAfter = v['retry_after'] as int? ?? 0;
  }
}

// Owns the whole send lifecycle, independently of routes, controllers and watch.
// Retries replay the same persisted command ID and exact send parameters.
// A new ID is allowed only after a definite pre-write failure.
class OutboxManager {
  static const channel = MethodChannel('dev.agentdeck/outbox');
  final AgentClient client;
  final List<OutgoingMessage> tasks = [];
  final Set<String> _running = {};
  Future<void>? _restoring;
  Future<void> _writes = Future.value();
  Timer? _retry;
  bool _disposed = false,
      _loaded = false,
      _serviceActive = false,
      _leased = false,
      _timedOut = false;
  int _generation = 0;
  String restoreError = '';
  OutboxManager(this.client) {
    client.addListener(_changed);
    if (!kIsWeb && Platform.isAndroid) {
      channel.setMethodCallHandler((call) async {
        if (call.method == 'timeout') {
          await pauseForSystemTimeout();
        }
      });
    }
  }
  Future<void> pauseForSystemTimeout() async {
    _timedOut = true;
    _retry?.cancel();
    await _background(false);
  }

  String get binding =>
      '${client.device?['url'] ?? ''}|${client.device?['device'] ?? 'unpaired'}';
  List<OutgoingMessage> forSession(String id) => tasks
      .where((t) => t.binding == binding && t.matches(id) && t.state != 'done')
      .toList();
  List<OutgoingMessage> get outstanding => tasks
      .where((t) => t.binding == binding && (t.pending || t.needsAttention))
      .toList();
  void _notify() {
    if (!_disposed) client.notifyOutboxChanged();
  }

  void _changed() {
    if (_disposed) return;
    for (final t in tasks.where(
      (t) =>
          t.state == 'accepted' ||
          t.state == 'submitted' ||
          t.state == 'uncertain',
    )) {
      final blocks = client.transcripts[t.session]?.blocks;
      if (blocks == null) continue;
      t.receipt.reconcile(blocks);
      final session = client.sessions
          .where((s) => s.id == t.session)
          .firstOrNull;
      if (!t._runningReported &&
          t._sendStarted != null &&
          t._sendTiming != null &&
          session?.status == 'running' &&
          t.receipt.observed(blocks)) {
        t._runningReported = true;
        _reportSendTiming(t, {
          ...t._sendTiming!,
          'running_ms': client.clock.stamp - t._sendStarted!,
        });
      }
      if (t.receipt.responded(blocks) ||
          t.receipt.observed(blocks) &&
              !t.receipt.waiting(
                blocks,
                status: session?.status ?? '',
                updated: session?.updated ?? '',
              )) {
        t.state = 'done';
        t.error = '';
        unawaited(
          _save(t)
              .then((_) => client.vault.removeOutgoing(t.id))
              .catchError((Object _) {}),
        );
      }
    }
    _pump();
  }

  Future<void> restore() => _restoring ??= _restore().catchError((Object e) {
    restoreError = '发件箱读取失败，请重试';
    _restoring = null;
    _notify();
  });

  void _reportSendTiming(OutgoingMessage t, Map<String, int> timing) {
    if (timing['running_ms'] case final int running) {
      client.diagnosticLog.record(
        'send_running',
        request: t.commandID,
        duration: running,
      );
    } else {
      for (final entry in {
        'prepare_ms': 'send_prepared',
        'request_ms': 'send_round_trip',
        'total_ms': 'send_total',
      }.entries) {
        client.diagnosticLog.record(
          entry.value,
          request: t.commandID,
          duration: timing[entry.key] ?? 0,
        );
      }
    }
    unawaited(
      client
          .send({
            'method': 'client_timing',
            'kind': 'send',
            'id': t.commandID,
            'session': t.session,
            'send_timing': timing,
          })
          .catchError((Object _) {}),
    );
  }

  Future<void> _restore() async {
    final generation = _generation;
    final rows = await client.vault.loadOutgoing();
    if (_disposed || generation != _generation) return;
    final restored =
        rows
            .map(OutgoingMessage.fromJson)
            .where((t) => t.binding == binding)
            .toList()
          ..sort((a, b) => a.created.compareTo(b.created));
    for (final t in restored) {
      if (t.state == 'done' || t.state == 'queued') {
        unawaited(client.vault.removeOutgoing(t.id).catchError((Object _) {}));
        continue;
      }
      if (t.running) {
        t.state = 'pending';
        t.stage = t.locked ? '正在核对发送结果…' : '等待发送';
      }
      if (!tasks.any((v) => v.id == t.id)) tasks.add(t);
    }
    restoreError = '';
    _loaded = true;
    _notify();
    _pump();
    if (!tasks.any((t) => t.pending)) await _background(false, force: true);
  }

  Future<void> _save(OutgoingMessage t) {
    final value = jsonDecode(jsonEncode(t.toJson())) as Map<String, dynamic>;
    final generation = _generation;
    final next = _writes.then((_) async {
      if (!_disposed && generation == _generation) {
        await client.vault.saveOutgoing(t.id, value);
      }
    });
    _writes = next.catchError((Object _) {});
    return next;
  }

  List<Map<String, dynamic>> _info(List<DraftFile> files) => files
      .map(
        (f) => {
          'name': f.name,
          'mime': f.mime,
          'upload': f.upload,
          'session': f.uploadedSession,
          'offset': f.offset,
        },
      )
      .toList();
  Future<OutgoingMessage> enqueue(
    AgentSession s,
    String text,
    TurnOptions options,
    List<DraftFile> files,
    String delivery,
  ) async {
    final started = client.clock.stamp;
    if (s.issue?.blocksInput == true) {
      throw AgentRequestException(
        s.issue!.message,
        notSent: true,
        remote: true,
      );
    }
    await restore();
    if (restoreError.isNotEmpty) throw FileSystemException(restoreError);
    if (_disposed) throw const FileSystemException('发件箱已关闭');
    final id = AgentClient.requestID();
    final copies = files
        .map((f) => DraftFile(f.name, f.mime, Uint8List.fromList(f.bytes)))
        .toList();
    final t = OutgoingMessage(
      id: id,
      binding: binding,
      originalSession: s.id,
      session: s.id,
      agent: s.agent,
      project: s.project,
      title: s.title,
      text: text,
      delivery: delivery,
      options: options,
      created: DateTime.now().microsecondsSinceEpoch,
      source: s.source,
      managed: s.managed,
      commandID: id,
      files: copies,
      fileInfo: _info(copies),
      receipt: InputReceipt(
        id: id,
        text: [
          text,
          ...copies.map((f) => f.name),
        ].where((v) => v.isNotEmpty).join('\n'),
        matchText: text,
        cursor: client.transcripts[s.id]?.cursor ?? 0,
        baselineUpdated: s.updated,
      ),
    );
    t._sendStarted = started;
    // Commit ciphertext before clearing the input or making any agent call.
    final generation = _generation;
    try {
      final commit = _writes.then((_) async {
        if (_disposed || generation != _generation || t.binding != binding) {
          throw const FileSystemException('工作站配对已变化');
        }
        if (copies.isNotEmpty) {
          await client.vault.saveOutgoingFiles(
            id,
            copies
                .map(
                  (f) => {
                    'name': f.name,
                    'mime': f.mime,
                    'bytes': base64Encode(f.bytes),
                  },
                )
                .toList(),
          );
        }
        if (_disposed || generation != _generation || t.binding != binding) {
          throw const FileSystemException('工作站配对已变化');
        }
        await client.vault.saveOutgoing(t.id, t.toJson());
      });
      _writes = commit.catchError((Object _) {});
      await commit;
      if (_disposed || generation != _generation || t.binding != binding) {
        throw const FileSystemException('工作站配对已变化');
      }
    } catch (_) {
      throw const FileSystemException('发送记录保存失败，消息未提交，请重试');
    }
    // Waiting messages keep attachment bytes on encrypted disk, not in the
    // route or an ever-growing in-memory upload queue.
    t.files = null;
    tasks.add(t);
    _timedOut = false;
    _notify();
    _pump();
    return t;
  }

  void foreground() {
    _timedOut = false;
    _pump();
  }

  void _pump() {
    if (_disposed || !_loaded || _timedOut) return;
    if (client.device?['revoked'] == true) {
      _retry?.cancel();
      unawaited(_background(false));
      return;
    }
    _retry?.cancel();
    tasks.removeWhere(
      (t) =>
          (t.state == 'done' || t.state == 'queued') &&
          !_running.contains(t.id),
    );
    final now = DateTime.now().millisecondsSinceEpoch;
    final blocked = <String>{};
    bool automatic = false;
    bool waitingRetry = false;
    for (final t in tasks) {
      if (t.binding != binding ||
          t.state == 'done' ||
          t.state == 'accepted' ||
          t.state == 'submitted' ||
          t.state == 'queued') {
        continue;
      }
      if (!blocked.add(t.originalSession) || t.needsAttention) {
        continue;
      }
      automatic = true;
      if (_running.contains(t.id)) continue;
      if (!client.connected || t.retryAfter > now) {
        if (!client.connected) t.stage = '等待连接，恢复后自动重试';
        waitingRetry = true;
        continue;
      }
      _running.add(t.id);
      unawaited(_run(t));
    }
    if (automatic) {
      // Keep transport alive across Android backgrounding; the separate native
      // foreground service owns the cached engine and the sending notification.
      unawaited(_background(true));
      if (waitingRetry) _retry = Timer(const Duration(seconds: 2), _pump);
    } else if (_running.isEmpty) {
      unawaited(_background(false));
    }
  }

  Future<void> _background(bool active, {bool force = false}) async {
    if (_disposed && active) return;
    if (active && !_leased) {
      _leased = true;
      client.retainDownloadConnection();
    }
    if (!active && _leased) {
      _leased = false;
      client.releaseDownloadConnection();
    }
    if (_serviceActive == active && !force) return;
    _serviceActive = active;
    if (!kIsWeb && Platform.isAndroid) {
      try {
        await channel.invokeMethod('background', {'active': active});
      } catch (_) {
        _serviceActive = false;
      }
    }
  }

  bool _valid(OutgoingMessage t, int generation) =>
      !_disposed &&
      generation == _generation &&
      t.binding == binding &&
      tasks.contains(t);
  Future<void> _run(OutgoingMessage t) async {
    final generation = _generation;
    bool acknowledged = false;
    try {
      final current = client.sessions
          .where((s) => s.id == t.session)
          .firstOrNull;
      if ((!t.locked || t.attempts == 0) &&
          current?.issue?.blocksInput == true) {
        throw AgentRequestException(
          current!.issue!.message,
          notSent: true,
          remote: true,
        );
      }
      if (!t.locked) {
        t.state = 'preparing';
        t.stage = '正在准备发送…';
        _notify();
        // enqueue already committed the unchanged draft. The frozen command
        // below is still fsynced before any send; avoid another identical write.
        if (!_valid(t, generation)) return;
        if (t.files == null) {
          final rows = t.fileInfo.isEmpty
              ? <Map<String, dynamic>>[]
              : await client.vault.loadOutgoingFiles(t.id);
          t.files = rows.asMap().entries.map((entry) {
            final v = entry.value;
            final info = t.fileInfo[entry.key];
            return DraftFile(v['name'], v['mime'], base64Decode(v['bytes']))
              ..upload = info['upload'] as String? ?? ''
              ..uploadedSession = info['session'] as String? ?? ''
              ..offset = info['offset'] as int? ?? 0;
          }).toList();
        }
        if (t.files!.any((f) => f.image)) {
          t.stage = '正在检查图片支持…';
          _notify();
        }
        await client.validateAttachments(t.agent, t.session, t.files!);
        if (!_valid(t, generation)) return;
        final directOriginal = t.agent == 'codex' && client.atomicCodexSend;
        if (!t.managed && !directOriginal) {
          t.stage =
              (t.agent == 'codex' && client.codexDesktop || t.agent == 'claude')
              ? '正在连接电脑原会话…'
              : '正在准备发送…';
          _notify();
          final p = client.projects
              .where((p) => p.path == t.project)
              .firstOrNull;
          if (p == null) {
            throw const FormatException('此项目尚未授权，需在后端配置 project-roots');
          }
          final s = await client.prepareOutgoingSession(
            t.agent,
            p.id,
            t.session,
            '${t.id}/create',
          );
          if (!_valid(t, generation)) return;
          t.session = s.id;
          t.managed = true;
          t.source = s.source;
          t.receipt.cursor =
              client.transcripts[s.id]?.cursor ?? t.receipt.cursor;
          await _save(t);
        }
        if (!t.options.inherited) {
          await client.saveTurnOptions(t.session, t.options);
        }
        if (!_valid(t, generation)) return;
        final uploads = <String>[];
        var savedAt = 0;
        for (final f in t.files!) {
          t.state = 'uploading';
          t.stage = '正在上传附件…';
          _notify();
          uploads.add(
            await client.uploadFile(
              t.session,
              f,
              progress: (done, total) {
                if (!_valid(t, generation)) return;
                t.stage = '${(done * 100 / total).round()}% · ${f.name}';
                _notify();
                if (DateTime.now().millisecondsSinceEpoch - savedAt > 1000) {
                  savedAt = DateTime.now().millisecondsSinceEpoch;
                  t.fileInfo = _info(t.files!);
                  unawaited(_save(t).catchError((Object _) {}));
                }
              },
            ),
          );
          if (!_valid(t, generation)) return;
          t.fileInfo = _info(t.files!);
          await _save(t);
        }
        t.uploads = uploads;
        t.locked = true;
        await _save(
          t,
        ); // frozen IDs/options are replayed unchanged after an ACK loss
        t.files = null;
      }
      if (!_valid(t, generation)) return;
      t.state = 'sending';
      t.stage = t.attempts > 0 ? '正在核对发送结果…' : '正在发送…';
      if (t.attempts == 0 && t.source == 'desktop') t.stage = '正在发送到电脑原会话…';
      t.error = '';
      _notify();
      await _save(t);
      if (!_valid(t, generation)) return;
      final dispatched = client.clock.stamp;
      await client.sendDelivery(
        t.session,
        t.text,
        t.options,
        t.uploads,
        t.delivery,
        requestId: t.commandID,
      );
      if (!_valid(t, generation)) return;
      acknowledged = true;
      if (t._sendStarted != null && t.attempts == 0) {
        final ended = client.clock.stamp;
        final timing = <String, int>{
          'prepare_ms': dispatched - t._sendStarted!,
          'request_ms': ended - dispatched,
          'total_ms': ended - t._sendStarted!,
        };
        // Best-effort numeric diagnostics never gate delivery or trigger retry.
        t._sendTiming = timing;
        _reportSendTiming(t, timing);
      }
      t.receipt.nativeMessageID = client.consumeSubmittedMessageID(t.commandID);
      if (client.consumeQueuedInput(t.commandID)) {
        t.state = 'queued';
        t.stage = '已加入 Agent 队列';
      } else if (client.consumeSubmittedInput(t.commandID)) {
        t.state = 'submitted';
        t.stage = '已提交，等待原会话确认';
        t.receipt.phase = InputDeliveryPhase.submitted;
      } else {
        t.state = 'accepted';
        t.stage = '已送达工作站';
        t.receipt.phase = InputDeliveryPhase.accepted;
      }
      t.files = null;
      t.error = '';
      await _save(t);
      await client.vault.removeOutgoingFiles(t.id);
      if (t.state == 'queued') await client.vault.removeOutgoing(t.id);
    } catch (e) {
      if (!_valid(t, generation)) return;
      if (acknowledged) {
        t.error = '已送达，发送记录保存失败';
        return;
      }
      if (t.files != null) t.fileInfo = _info(t.files!);
      t.attempts++;
      if (e is AgentRequestException && (e.remote || !e.notSent)) {
        t.definitelyNotSent = e.notSent;
        t.state = e.notSent ? 'failed' : 'uncertain';
        t.error = e.message;
        t.stage = e.notSent ? '发送失败，消息已保留' : '发送结果待确认';
        t.receipt.phase = e.notSent
            ? InputDeliveryPhase.preparing
            : InputDeliveryPhase.uncertain;
      } else if (e is HttpException ||
          e is SocketException ||
          e is TimeoutException) {
        t.state = 'retrying';
        t.stage = '等待连接，恢复后自动重试';
        t.error = '';
        t.retryAfter =
            DateTime.now().millisecondsSinceEpoch +
            min(30000, 1000 * (1 << min(t.attempts, 5)));
      } else {
        t.state = 'failed';
        t.definitelyNotSent = !t.locked;
        t.error = e.toString();
        t.stage = '发送失败，消息已保留';
      }
      try {
        await _save(t);
      } catch (_) {
        t.stage = '发送记录保存失败，消息仍在发件箱';
      }
    } finally {
      _running.remove(t.id);
      if (!_disposed && generation == _generation) {
        _notify();
        _pump();
      }
    }
  }

  Future<void> retry(OutgoingMessage t) async {
    if (_running.contains(t.id) ||
        t.binding != binding ||
        !(t.needsAttention || t.pending)) {
      return;
    }
    if (t.definitelyNotSent) {
      t.commandID = AgentClient.requestID();
      t.receipt = InputReceipt(
        id: t.commandID,
        text: t.receipt.text,
        matchText: t.text,
        cursor: client.transcripts[t.session]?.cursor ?? t.receipt.cursor,
        baselineUpdated: t.receipt.baselineUpdated,
      );
      if (t.fileInfo.isNotEmpty) {
        t.locked = false;
        t.files = null;
        t.uploads = [];
        for (final file in t.fileInfo) {
          file['upload'] = '';
          file['session'] = '';
          file['offset'] = 0;
        }
      }
    }
    // Unknown outcomes always replay the original ID; never duplicate a turn.
    t.state = 'pending';
    t.retryAfter = 0;
    t.error = '';
    t.definitelyNotSent = false;
    await _save(t);
    _timedOut = false;
    _notify();
    _pump();
  }

  Future<void> remove(OutgoingMessage t) async {
    if (_running.contains(t.id)) throw const FileSystemException('正在发送，请稍后再操作');
    await client.vault.removeOutgoing(t.id);
    tasks.remove(t);
    _notify();
    _pump();
  }

  Future<void> reset() async {
    _generation++;
    _retry?.cancel();
    tasks.clear();
    await _writes;
    _restoring = null;
    _loaded = false;
    restoreError = '';
    await _background(false);
  }

  void dispose() {
    _disposed = true;
    _generation++;
    _retry?.cancel();
    client.removeListener(_changed);
    unawaited(_background(false));
  }
}
