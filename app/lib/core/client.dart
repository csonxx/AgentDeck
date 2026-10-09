import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';
import 'package:cryptography/cryptography.dart';
import 'transport.dart';
import 'relay.dart';
import 'package:flutter/foundation.dart';
import 'crypto.dart';
import 'clock.dart';
import 'model.dart';
import 'vault.dart';
import 'agents.dart';
import 'input.dart';

class AgentClient extends ChangeNotifier {
  final DeviceVault vault;
  // Tests inject a local CA for the TLS-terminating proxy. Production relies on
  // platform roots and never accepts invalid certificates in system mode.
  @visibleForTesting
  final SecurityContext? trustedRoots;
  final ProtocolClock clock;
  AgentClient({DeviceVault? vault, this.trustedRoots, ProtocolClock? clock})
    : vault = vault ?? DeviceVault(),
      clock = clock ?? ProtocolClock();
  Map<String, dynamic>? device;
  List<AgentSession> sessions = [];
  final Map<String, List<QueuedMessage>> queues = {};
  final Map<String, String> queueErrors = {};
  bool codexDesktop = false;
  List<AgentInfo> agents = defaultAgents();
  AgentVisibility agentVisibility = AgentVisibility();
  AgentVisibility _savedAgentVisibility = AgentVisibility();
  Future<void> _agentSaveQueue = Future.value();
  List<AgentInfo> get visibleAgents =>
      agents.where((a) => !agentVisibility.hidden.contains(a.id)).toList();
  AgentInfo agentInfo(String id) =>
      agents.where((a) => a.id == id).firstOrNull ??
      AgentInfo(id: id, name: agentNames[id] ?? id);
  Future<void> loadAgentVisibility() async {
    try {
      agentVisibility = AgentVisibility.fromJson(
        await vault.loadAgentVisibility() ?? {},
      );
      _savedAgentVisibility = agentVisibility;
    } catch (_) {
      error = 'Agent 显示设置读取失败';
    }
    if (!_closed) notifyListeners();
  }

  Future<void> updateAgentVisibility(AgentVisibility value) async {
    agentVisibility = value;
    notifyListeners();
    final save = _agentSaveQueue.then((_) async {
      await vault.saveAgentVisibility(value.toJson());
      _savedAgentVisibility = value;
    });
    _agentSaveQueue = save.catchError((Object _) {});
    try {
      await save;
    } catch (_) {
      if (identical(agentVisibility, value) && !_closed) {
        agentVisibility = _savedAgentVisibility;
        notifyListeners();
      }
      throw const FileSystemException('Agent 显示设置保存失败，请重试');
    }
  }

  void _updateAgents(Map<String, dynamic> m) {
    if (m['agents'] is List) {
      agents = (m['agents'] as List)
          .whereType<Map>()
          .map((a) => AgentInfo.fromJson(Map<String, dynamic>.from(a)))
          .where((a) => RegExp(r'^[a-z][a-z0-9_-]{0,31}$').hasMatch(a.id))
          .toList();
      if (agents.isEmpty) agents = defaultAgents();
    }
  }

  Future<Map<String, dynamic>> agentLogin(String id) async =>
      Map<String, dynamic>.from(
        await request({'method': 'agent_login', 'agent': id}) as Map,
      );

  List<AgentProject> projects = [];
  ProjectLayout projectLayout = ProjectLayout();
  ProjectLayout _savedProjectLayout = ProjectLayout();
  Future<void> _layoutSaveQueue = Future.value();
  final Map<String, Completer<dynamic>> _requests = {};
  final Map<String, Transcript> transcripts = {};
  final Map<String, Uint8List> images = {};
  final Map<String, Completer<Uint8List>> _media = {};
  WebSocket? _socket;
  HttpClient? _http;
  Timer? _retry, _heartbeat, _cacheTimer, _notifyTimer;
  bool _cacheWriting = false;
  List<int> _salt = [];
  int _rotation = 900000,
      _sent = 0,
      _received = 0,
      _generation = 0,
      _attempt = 0;
  Future<void> _receiveQueue = Future.value(),
      _sendQueue = Future.value(),
      _saveQueue = Future.value();
  String state = '尚未配对', error = '', selected = '';
  bool connected = false,
      demo = false,
      _closed = false,
      _connecting = false,
      _suspended = false;
  int get epoch => clock.millis ~/ _rotation;
  List<int> get _root => base64Decode(device!['root'] as String);

  Future<void> initialize() async {
    await loadProjectLayout();
    await loadAgentVisibility();
    try {
      device = await vault.loadDevice();
      if (device != null) {
        final cache = await vault.readCache();
        sessions = (cache['sessions'] as List? ?? [])
            .map(
              (e) => AgentSession.fromJson(Map<String, dynamic>.from(e as Map)),
            )
            .toList();
        for (final e in Map<String, dynamic>.from(
          cache['version'] == 3 ? cache['transcripts'] as Map? ?? {} : {},
        ).entries.take(3)) {
          final t = Transcript();
          final cached = e.value as List;
          for (final v in cached.skip(max(0, cached.length - 40))) {
            t.add(Map<String, dynamic>.from(v as Map));
          }
          transcripts[e.key] = t;
        }
        await connect();
      }
    } catch (_) {
      error = '无法读取本机安全存储，请重试或重新配对';
    }
    notifyListeners();
  }

  Future<void> loadProjectLayout() async {
    try {
      projectLayout = ProjectLayout.fromJson(
        await vault.loadProjectLayout() ?? {},
      );
      _savedProjectLayout = projectLayout;
    } catch (_) {
      error = '项目布局读取失败';
    }
    if (!_closed) notifyListeners();
  }

  Future<void> updateProjectLayout(ProjectLayout value) async {
    projectLayout = value;
    notifyListeners();
    final save = _layoutSaveQueue.then((_) async {
      await vault.saveProjectLayout(value.toJson());
      _savedProjectLayout = value;
    });
    // A failed write must not prevent subsequent writes from running.
    _layoutSaveQueue = save.catchError((Object _) {});
    try {
      await save;
    } catch (_) {
      if (identical(projectLayout, value) && !_closed) {
        projectLayout = _savedProjectLayout;
        notifyListeners();
      }
      throw const FileSystemException('项目布局保存失败，请重试');
    }
  }

  HttpClient _transport(String trust, String pin) =>
      createTransport(trust, pin, roots: trustedRoots);

  Future<Map<String, dynamic>> _post(
    HttpClient h,
    String origin,
    String path,
    Object data, {
    Map<String, dynamic>? relay,
  }) async {
    final r = await h
        .postUrl(Uri.parse('$origin$path'))
        .timeout(const Duration(seconds: 12));
    r.followRedirects = false;
    r.headers.contentType = ContentType.json;
    final raw = utf8.encode(jsonEncode(data));
    final headers = await relayHeaders(
      relay,
      'POST',
      path,
      raw,
      unixSeconds: clock.unix,
    );
    headers.forEach((k, v) => r.headers.set(k, v));
    r.add(raw);
    final response = await r.close().timeout(const Duration(seconds: 12));
    if (response.statusCode == 202) {
      await response.drain<void>();
      throw ApprovalPending();
    }
    if (response.statusCode != 200) {
      await response.drain<void>();
      throw HttpException('HTTP ${response.statusCode}');
    }
    final text = await response
        .transform(utf8.decoder)
        .join()
        .timeout(const Duration(seconds: 12));
    return Map<String, dynamic>.from(jsonDecode(text) as Map);
  }

  Future<void> pair(String text) async => pairBundle(PairBundle.parse(text));

  Future<void> pairBundle(PairBundle b, {SimpleKeyPair? keyPair}) async {
    if (connected) {
      _lost();
      _retry?.cancel();
    }
    // The authenticated server checks expiry; the phone wall clock is untrusted.
    final h = _transport(b.tlsTrust, b.pin);
    try {
      if (b.relay != null) await _syncGateway(h, b.url, b.relay!);
      final algorithm = X25519();
      final keys = keyPair ?? await algorithm.newKeyPair();
      final pub = await keys.extractPublicKey();
      final shared = await algorithm.sharedSecretKey(
        keyPair: keys,
        remotePublicKey: SimplePublicKey(
          base64Decode(b.publicKey),
          type: KeyPairType.x25519,
        ),
      );
      final root = await WireCrypto.derive(
        await shared.extractBytes(),
        base64Decode(b.token),
        'agentdeck/pair/v1',
      );
      final invite = bodyHash(base64Decode(b.token));
      final request = await WireCrypto.seal(
        root,
        'pair-request/$invite',
        0,
        0,
        {'token': b.token, 'name': 'AgentDeck ${Platform.operatingSystem}'},
      );
      final response = await _post(h, b.url, relayPath(b.relay, 'pair'), {
        'invite': invite,
        'public': base64Encode(pub.bytes),
        'box': request,
      }, relay: b.relay);
      final result = await WireCrypto.open(root, 'pair', response);
      if ((result['relay_grant'] as String? ?? '') !=
          (b.relay?['grant'] as String? ?? '')) {
        throw const FormatException('工作站配对身份不一致');
      }
      ++_generation;
      _retry?.cancel();
      _heartbeat?.cancel();
      _cacheTimer?.cancel();
      _cacheTimer = null;
      await _socket?.close();
      _socket = null;
      _http?.close(force: true);
      connected = false;
      await _saveQueue;
      await _layoutSaveQueue;
      await vault.clear(keepGateway: true);
      projectLayout = ProjectLayout();
      _savedProjectLayout = projectLayout;
      device = {
        'url': b.url,
        'pin': b.pin,
        'tls_trust': b.tlsTrust,
        if (b.relay != null) 'relay': b.relay,
        'device': result['device'],
        'root': base64Encode(root),
      };
      await vault.saveDevice(device!);
      demo = false;
      sessions = [];
      transcripts.clear();
      queues.clear();
      queueErrors.clear();
      images.clear();
    } finally {
      h.close(force: true);
    }
    await connect();
  }

  Future<void> connect() async {
    if (device == null || _closed || demo || _connecting || _suspended) return;
    _connecting = true;
    _retry?.cancel();
    final generation = ++_generation;
    connected = false;
    _heartbeat?.cancel();
    state = '正在连接';
    error = '';
    notifyListeners();
    await _socket?.close();
    _socket = null;
    _http?.close(force: true);
    final h = _transport(
      device!['tls_trust'] as String? ?? 'pin',
      device!['pin'] as String,
    );
    _http = h;
    try {
      final challenge = base64Encode(WireCrypto.random(24));
      final id = device!['device'] as String;
      final binding = device!['relay'] as Map<String, dynamic>?;
      if (binding != null) {
        await _syncGateway(h, device!['url'] as String, binding);
      } else {
        await synchronizeClock(
          clock,
          _root,
          'device',
          id,
          (request) =>
              _post(h, device!['url'] as String, '/v1/time-sync', request),
        );
      }
      if (_suspended || generation != _generation) return;
      final box = await WireCrypto.seal(_root, 'hello/$id', 0, 0, {
        'time': clock.unix,
        'challenge': challenge,
      });
      final reply = await _post(
        h,
        device!['url'] as String,
        relayPath(binding, 'session'),
        {'device': id, 'box': box},
        relay: binding,
      );
      final welcome = await WireCrypto.open(_root, 'welcome/$id', reply);
      if (welcome['challenge'] != challenge || welcome['clock_version'] != 1) {
        throw const FormatException('Connection challenge mismatch');
      }
      _salt = base64Decode(welcome['salt'] as String);
      _rotation = (welcome['rotation_ms'] as num).toInt();
      if (_rotation < 1000 || _salt.length != 32) {
        throw const FormatException('Invalid rotation interval');
      }
      final path = relayPath(binding, 'stream');
      final u = Uri.parse('${device!['url']}$path').replace(scheme: 'wss');
      final headers = await relayHeaders(
        binding,
        'GET',
        path,
        [],
        authorization: 'Bearer ${welcome['ticket']}',
        unixSeconds: clock.unix,
      );
      final ws = await WebSocket.connect(
        u.toString(),
        headers: headers,
        customClient: h,
      ).timeout(const Duration(seconds: 15));
      if (_closed || generation != _generation) {
        await ws.close();
        return;
      }
      _socket = ws;
      _sent = 0;
      _received = 0;
      _attempt = 0;
      _receiveQueue = Future.value();
      _sendQueue = Future.value();
      connected = true;
      state = '实时连接';
      notifyListeners();
      ws.listen(
        (data) {
          _receiveQueue = _receiveQueue
              .then((_) => _receive(data, generation))
              .catchError((Object _) {
                if (generation == _generation) _lost();
              });
        },
        onError: (Object _) {
          if (generation == _generation) _lost();
        },
        onDone: () {
          if (generation == _generation) _lost();
        },
      );
      _heartbeat?.cancel();
      _heartbeat = Timer.periodic(const Duration(seconds: 20), (_) {
        send({'method': 'ping'}).catchError((Object _) {});
      });
      if (selected.isNotEmpty) await watch(selected);
    } catch (e) {
      if (generation == _generation) {
        error = e is HandshakeException
            ? '证书校验失败，请检查后端证书或重新配对'
            : '连接失败，请检查域名、网络或设备授权';
        _lost();
      }
    } finally {
      _connecting = false;
    }
  }

  Future<void> _syncGateway(
    HttpClient h,
    String origin,
    Map<String, dynamic> binding,
  ) => synchronizeClock(
    clock,
    base64Decode(binding['key'] as String),
    'grant',
    binding['grant'] as String,
    (request) => _post(h, origin, '/v2/time-sync', request),
  );

  void suspend() {
    if (_suspended || _closed) return;
    _suspended = true;
    _lost();
    _http?.close(force: true);
    state = device == null ? '尚未配对' : '已暂停连接';
    if (!_closed) notifyListeners();
  }

  Future<void> resume() async {
    if (!_suspended || _closed) return;
    _suspended = false;
    // A cancelled handshake must finish before a fresh one can start.
    while (_connecting && !_closed && !_suspended) {
      await Future<void>.delayed(const Duration(milliseconds: 50));
    }
    if (!_closed && !_suspended) await connect();
  }

  void _lost() {
    if (_closed || demo) return;
    ++_generation;
    connected = false;
    state = '连接中断 · 自动重连';
    _heartbeat?.cancel();
    _socket?.close();
    _socket = null;
    _retry?.cancel();
    final delay = min(30, 1 << min(_attempt++, 5));
    if (!_suspended) _retry = Timer(Duration(seconds: delay), connect);
    notifyListeners();
    for (final c in _media.values) {
      if (!c.isCompleted) c.completeError(const HttpException('连接中断'));
    }
    _media.clear();
    for (final r in _requests.values) {
      if (!r.isCompleted) {
        r.completeError(const HttpException('连接中断；检查会话是否已执行'));
      }
    }
    _requests.clear();
  }

  Future<void> send(Map<String, dynamic> data) {
    final generation = _generation;
    final task = _sendQueue.then((_) async {
      if (!connected || _socket == null || generation != _generation) {
        throw const HttpException('未连接');
      }
      final ep = epoch;
      final key = await WireCrypto.traffic(_root, _salt, 'c2s', ep);
      final b = await WireCrypto.seal(key, 'c2s', ep, ++_sent, data);
      if (generation != _generation || !connected) {
        throw const HttpException('连接已更新');
      }
      _socket!.add(jsonEncode(b));
    });
    _sendQueue = task.catchError((Object _) {});
    return task;
  }

  Future<void> _receive(dynamic raw, int generation) async {
    if (generation != _generation) return;
    if (raw is! String || raw.length > 24 << 20) {
      throw const FormatException('Invalid frame');
    }
    final b = Map<String, dynamic>.from(jsonDecode(raw) as Map);
    final ep = b['epoch'] as int;
    if (b['seq'] != _received + 1 || (ep - epoch).abs() > 1) {
      throw const FormatException('Replay or expired frame');
    }
    final m = await WireCrypto.open(
      await WireCrypto.traffic(_root, _salt, 's2c', ep),
      's2c',
      b,
    );
    if (generation != _generation) return;
    _received = b['seq'] as int;
    switch (m['type']) {
      case 'sessions':
        codexDesktop = m['codex_desktop'] == true;
        _updateAgents(m);
        projects = (m['projects'] as List? ?? [])
            .map((p) => AgentProject.fromJson(Map<String, dynamic>.from(p)))
            .toList();
        sessions = (m['sessions'] as List)
            .map(
              (e) => AgentSession.fromJson(Map<String, dynamic>.from(e as Map)),
            )
            .toList();
      case 'agents':
        _updateAgents(m);
      case 'queue':
        final id = m['session'] as String;
        if (id == selected) {
          if (m['message'] is String) {
            queueErrors[id] = m['message'] as String;
          } else {
            queues[id] = (m['messages'] as List? ?? [])
                .map(
                  (v) => QueuedMessage.fromJson(
                    Map<String, dynamic>.from(v as Map),
                  ),
                )
                .toList();
            queueErrors.remove(id);
          }
        }
      case 'history':
        final id = m['session'] as String;
        if (id == selected) {
          transcripts
              .putIfAbsent(id, Transcript.new)
              .history(
                Map<String, dynamic>.from(m['page'] as Map),
                replace: true,
              );
        }
      case 'events':
        final id = m['session'] as String;
        if (id != selected) break;
        final t = transcripts.putIfAbsent(id, Transcript.new);
        for (final ev in m['events'] as List) {
          final event = Map<String, dynamic>.from(ev as Map);
          if (!t.detached || t.containsEvent(event)) t.add(event);
        }
      case 'media':
        final bytes = Uint8List.fromList(base64Decode(m['bytes'] as String));
        final mediaID = m['media'] as String;
        images.remove(mediaID);
        while (images.isNotEmpty &&
            images.values.fold<int>(0, (n, v) => n + v.length) + bytes.length >
                (32 << 20)) {
          images.remove(images.keys.first);
        }
        images[mediaID] = bytes;
        _media.remove(m['id'])?.complete(bytes);
      case 'result':
        _requests.remove(m['id'])?.complete(m['result']);
      case 'error':
        _requests
            .remove(m['id'])
            ?.completeError(HttpException(m['message'] as String));
        error = m['message'] as String;
        _media
            .remove(m['id'])
            ?.completeError(HttpException(m['message'] as String));
    }
    if (['sessions', 'events', 'history'].contains(m['type'])) {
      _scheduleCache();
      _notifyTimer ??= Timer(const Duration(milliseconds: 80), () {
        _notifyTimer = null;
        if (!_closed) notifyListeners();
      });
    } else if (m['type'] == 'error') {
      notifyListeners();
    }
  }

  Future<dynamic> request(Map<String, dynamic> data) async {
    final id = List.generate(
      16,
      (_) => Random.secure().nextInt(256).toRadixString(16).padLeft(2, '0'),
    ).join();
    final result = Completer<dynamic>();
    _requests[id] = result;
    try {
      await send({...data, 'id': id});
      return await result.future.timeout(const Duration(seconds: 70));
    } finally {
      _requests.remove(id);
    }
  }

  Future<AgentSession> createSession(
    String agent,
    String project, {
    String parent = '',
  }) async {
    final result = await request({
      'method': 'create',
      'agent': agent,
      'project': project,
      'session': parent,
    });
    final session = AgentSession.fromJson(Map<String, dynamic>.from(result));
    if (!sessions.any((s) => s.id == session.id)) sessions.insert(0, session);
    await watch(session.id);
    return session;
  }

  Future<void> sendPrompt(String session, String text) async {
    await request({'method': 'send', 'session': session, 'text': text});
  }

  Future<void> sendInput(
    String session,
    String text,
    TurnOptions options,
    List<String> files,
  ) async {
    if (options.inherited && files.isEmpty) {
      return sendPrompt(session, text);
    }
    await request({
      'method': 'send',
      'session': session,
      'text': text,
      ...options.toJson(),
      'attachments': files,
    });
  }

  Future<void> sendDelivery(
    String session,
    String text,
    TurnOptions options,
    List<String> files,
    String delivery,
  ) async {
    if (delivery == 'send') return sendInput(session, text, options, files);
    await request({
      'method': 'send',
      'session': session,
      'text': text,
      ...options.toJson(),
      'attachments': files,
      'delivery': delivery,
    });
  }

  Future<List<QueuedMessage>> queue(String session) async {
    final value = await request({'method': 'queue_list', 'session': session});
    return (value as List)
        .map((v) => QueuedMessage.fromJson(Map<String, dynamic>.from(v as Map)))
        .toList();
  }

  Future<void> changeQueue(
    String session,
    QueuedMessage message,
    String action, {
    String text = '',
  }) async {
    await request({
      'method': 'queue_$action',
      'session': session,
      'queue_id': message.id,
      'queue_version': message.version,
      'text': text,
    });
  }

  Future<void> cancelDelivery(String session, String queue) async {
    await request({
      'method': 'queue_cancel',
      'session': session,
      'queue_id': queue,
    });
  }

  Future<InputCatalog> inputCatalog(String agent, String session) async =>
      InputCatalog.fromJson(
        Map<String, dynamic>.from(
          await request({
                'method': 'models',
                'agent': agent,
                'session': session,
              })
              as Map,
        ),
      );
  Future<InputCapabilities> inputCapabilities(
    String agent,
    String session, {
    bool media = true,
  }) async => InputCapabilities.fromJson(
    Map<String, dynamic>.from(
      await request({
            'method': 'input_capabilities',
            'agent': agent,
            'session': session,
            'kind': media ? 'media' : 'voice',
          })
          as Map,
    ),
  );

  Future<void> validateAttachments(
    String agent,
    String session,
    List<DraftFile> files,
  ) async {
    if (!files.any((file) => file.image)) return;
    final capabilities = await inputCapabilities(agent, session);
    if (!capabilities.images) {
      throw const FormatException('当前 Agent 接口未提供图片输入，请移除图片或选择普通文件');
    }
  }

  String _turnKey(String session) =>
      '${device?['device'] ?? 'unpaired'}/$session';
  Future<TurnOptions> loadTurnOptions(String session) async {
    final v = await vault.loadGeneration();
    return TurnOptions.fromJson(
      Map<String, dynamic>.from(v[_turnKey(session)] as Map? ?? {}),
    );
  }

  Future<void> _generationSaveQueue = Future.value();
  Future<void> saveTurnOptions(String session, TurnOptions options) {
    final save = _generationSaveQueue.then((_) async {
      final v = await vault.loadGeneration();
      v[_turnKey(session)] = options.toJson();
      await vault.saveGeneration(v);
    });
    _generationSaveQueue = save.catchError((Object _) {});
    return save;
  }

  Future<String> uploadFile(
    String session,
    DraftFile file, {
    String kind = 'file',
    void Function(int, int)? progress,
  }) async {
    if (file.bytes.isEmpty || file.bytes.length > 8 << 20) {
      throw const FormatException('附件为空或超过 8 MiB');
    }
    if (file.uploadedSession != session) {
      file.upload = '';
      file.offset = 0;
    }
    if (file.upload.isEmpty) {
      final v = Map<String, dynamic>.from(
        await request({
              'method': 'upload_begin',
              'session': session,
              'name': file.name,
              'mime': file.mime,
              'kind': kind,
              'size': file.bytes.length,
            })
            as Map,
      );
      file.upload = v['upload'] as String;
      file.uploadedSession = session;
      file.offset = 0;
    }
    while (file.offset < file.bytes.length) {
      final end = (file.offset + (64 << 10)).clamp(0, file.bytes.length);
      final v = Map<String, dynamic>.from(
        await request({
              'method': 'upload_chunk',
              'session': session,
              'upload': file.upload,
              'offset': file.offset,
              'bytes': base64Encode(
                Uint8List.sublistView(file.bytes, file.offset, end),
              ),
            })
            as Map,
      );
      final next = v['offset'] as int;
      if (next <= file.offset || next > file.bytes.length) {
        throw const FormatException('附件分块响应无效');
      }
      file.offset = next;
      progress?.call(next, file.bytes.length);
    }
    return file.upload;
  }

  Future<void> removeDraftFile(DraftFile file) async {
    if (file.upload.isNotEmpty && connected) {
      await request({
        'method': 'upload_drop',
        'session': file.uploadedSession,
        'upload': file.upload,
      });
    }
  }

  Future<String> transcribeRecording(
    String session,
    Uint8List wav, {
    bool preview = false,
  }) async {
    final file = DraftFile('voice.wav', 'audio/wav', wav);
    try {
      final id = await uploadFile(session, file, kind: 'voice');
      final result = Map<String, dynamic>.from(
        await request({
              'method': preview ? 'transcribe_preview' : 'transcribe',
              'session': session,
              'upload': id,
            })
            as Map,
      );
      return result['text'] as String;
    } finally {
      await removeDraftFile(file).catchError((Object _) {});
    }
  }

  Future<void> interrupt(String session) async {
    await request({'method': 'interrupt', 'session': session});
  }

  Future<void> approve(
    String session,
    String approval,
    String decision, {
    String answer = '',
  }) async {
    await request({
      'method': 'approve',
      'session': session,
      'approval': approval,
      'decision': decision,
      'text': answer,
    });
  }

  Future<void> watch(String id) async {
    selected = id;
    final current = transcripts.remove(id) ?? Transcript();
    transcripts[id] = current;
    queues.removeWhere((key, _) => key != id);
    queueErrors.removeWhere((key, _) => key != id);
    while (transcripts.length > 3) {
      transcripts.remove(transcripts.keys.first);
    }
    notifyListeners();
    if (connected) {
      await send({'method': 'watch', 'session': id, 'recent': true});
    }
  }

  Future<Map<String, dynamic>> outlinePage(String id, {int before = 0}) async =>
      Map<String, dynamic>.from(
        await request({'method': 'outline', 'session': id, 'before': before})
            as Map,
      );
  Future<void> jumpToRecord(String id, int first) async {
    final page = await request({
      'method': 'history_at',
      'session': id,
      'first': first,
    });
    if (selected != id) return;
    transcripts
        .putIfAbsent(id, Transcript.new)
        .history(Map<String, dynamic>.from(page as Map), replace: true);
    _scheduleCache();
    notifyListeners();
  }

  Future<void> loadOlder(String id) async {
    final t = transcripts[id];
    if (t == null || !t.hasMore || t.loadingOlder || !connected) return;
    t.loadingOlder = true;
    notifyListeners();
    try {
      final page = await request({
        'method': 'history',
        'session': id,
        'before': t.before,
      });
      if (identical(transcripts[id], t)) {
        t.history(Map<String, dynamic>.from(page as Map));
        _scheduleCache();
      }
    } finally {
      t.loadingOlder = false;
      if (!_closed) notifyListeners();
    }
  }

  Future<void> unwatch(String id) async {
    if (selected != id) return;
    selected = '';
    if (connected) await send({'method': 'watch', 'session': ''});
  }

  Future<void> reload(String id) async {
    transcripts[id] = Transcript();
    await watch(id);
  }

  Future<SessionAttachment> attachment(String session, String href) async {
    final uri = Uri.parse(href);
    if (uri.scheme == 'agentdeck-file') {
      if (!connected) {
        throw const HttpException('当前未连接工作站');
      }
      final buffer = BytesBuilder(copy: false);
      String name = '', mime = '';
      int size = 0;
      do {
        final v = Map<String, dynamic>.from(
          await request({
                'method': 'upload_read',
                'session': session,
                'upload': uri.path,
                'offset': buffer.length,
              })
              as Map,
        );
        size = v['size'] as int;
        name = v['name'] as String;
        mime = v['mime'] as String;
        final chunk = base64Decode(v['bytes'] as String);
        if (size > 8 << 20 ||
            chunk.isEmpty ||
            chunk.length > 64 << 10 ||
            buffer.length + chunk.length > size ||
            v['offset'] != buffer.length + chunk.length) {
          throw const FormatException('无效附件响应');
        }
        if (buffer.isEmpty &&
            RegExp(
              r'\.(doc|xls|txt|log|csv|tsv|md|json|xml|yaml|yml|ini|conf|cfg|sql|mp3|wav|m4a|m4b|aac|flac|ogg|oga|opus|aif|aiff|amr)$',
              caseSensitive: false,
            ).hasMatch(name)) {
          final preview = Map<String, dynamic>.from(
            await request({
                  'method': 'upload_preview',
                  'session': session,
                  'upload': uri.path,
                })
                as Map,
          );
          final bytes = base64Decode(preview['bytes'] as String);
          if (bytes.length > 8 << 20) {
            throw const FormatException('Invalid attachment response');
          }
          return SessionAttachment(
            name: preview['name'] as String,
            mime: preview['mime'] as String,
            bytes: bytes,
            truncated: preview['truncated'] == true,
          );
        }
        buffer.add(chunk);
      } while (buffer.length < size);
      return SessionAttachment(
        name: name,
        mime: mime,
        bytes: buffer.takeBytes(),
      );
    }
    if (uri.scheme == 'agentdeck-media') {
      return SessionAttachment(
        name: '会话图片',
        generatedName: true,
        mime: 'image/attachment',
        bytes: await image(uri.path),
      );
    }
    if (uri.scheme == 'data') {
      if (href.length > 12 << 20) throw const FormatException('图片超过 8 MiB');
      final data = uri.data!;
      final bytes = data.contentAsBytes();
      if (!data.mimeType.startsWith('image/') || bytes.length > 8 << 20) {
        throw const FormatException('图片超过 8 MiB');
      }
      return SessionAttachment(
        name: '会话图片',
        generatedName: true,
        mime: data.mimeType,
        bytes: bytes,
      );
    }
    if (uri.scheme == 'https' || uri.scheme == 'http') {
      final http = HttpClient()
        ..connectionTimeout = const Duration(seconds: 10);
      try {
        var target = uri;
        for (var hop = 0; hop < 6; hop++) {
          if (target.scheme != 'https' ||
              target.host.isEmpty ||
              target.userInfo.isNotEmpty) {
            throw const FormatException('图片链接必须使用 HTTPS');
          }
          final req = await http
              .getUrl(target)
              .timeout(const Duration(seconds: 12));
          req.followRedirects = false;
          final res = await req.close().timeout(const Duration(seconds: 12));
          if ([301, 302, 303, 307, 308].contains(res.statusCode)) {
            final location = res.headers.value('location');
            if (location == null) {
              throw const HttpException('Invalid image redirect');
            }
            target = target.resolve(location);
            continue;
          }
          if (res.statusCode != 200 || res.contentLength > 8 << 20) {
            throw const HttpException('图片加载失败');
          }
          final bytes = BytesBuilder(copy: false);
          await for (final chunk in res.timeout(const Duration(seconds: 12))) {
            if (bytes.length + chunk.length > 8 << 20) {
              throw const FormatException('图片超过 8 MiB');
            }
            bytes.add(chunk);
          }
          final mime = res.headers.contentType?.mimeType ?? '';
          if (!mime.startsWith('image/') &&
              !(mime.startsWith('audio/') && mediaExtension(uri.path)) &&
              !(mime.startsWith('video/') && mediaExtension(uri.path))) {
            throw const FormatException('Invalid image format');
          }
          return SessionAttachment(
            name: mime.startsWith('image/') ? '网页图片' : uri.pathSegments.last,
            generatedName: mime.startsWith('image/'),
            mime: mime,
            bytes: bytes.takeBytes(),
          );
        }
        throw const HttpException('Too many image redirects');
      } finally {
        http.close(force: true);
      }
    }
    if (!connected) throw const HttpException('当前未连接工作站');
    final result = Map<String, dynamic>.from(
      await request({'method': 'attachment', 'session': session, 'href': href})
          as Map,
    );
    return SessionAttachment(
      name: result['name'] as String,
      mime: result['mime'] as String,
      bytes: Uint8List.fromList(base64Decode(result['bytes'] as String)),
      line: (result['line'] as num?)?.toInt() ?? 0,
      truncated: result['truncated'] == true,
    );
  }

  Future<Uint8List> image(String id) async {
    final cached = images.remove(id);
    if (cached != null) {
      images[id] = cached;
      return cached;
    }
    if (_media[id] != null) return _media[id]!.future;
    final c = Completer<Uint8List>();
    _media[id] = c;
    try {
      await send({'method': 'media', 'id': id, 'media': id});
      return await c.future.timeout(const Duration(seconds: 15));
    } finally {
      _media.remove(id);
    }
  }

  void _scheduleCache() {
    // Coalesce periodic writes: one pending save, one small recent transcript.
    _cacheTimer ??= Timer(const Duration(seconds: 5), () {
      _cacheTimer = null;
      if (_cacheWriting) {
        _scheduleCache();
        return;
      }
      _cacheWriting = true;
      _saveQueue = _saveQueue
          .then((_) => _persist())
          .catchError((Object _) {
            error = '加密缓存写入失败';
            notifyListeners();
          })
          .whenComplete(() => _cacheWriting = false);
    });
  }

  Future<void> _persist() async {
    if (demo || device == null) return;
    await vault.writeCache({
      'version': 3,
      'sessions': sessions
          .map(
            (s) => {
              'id': s.id,
              'agent': s.agent,
              'title': s.title,
              'source': s.source,
              'status': s.status,
              'updated': s.updated,
              'project': s.project,
              'managed': s.managed,
            },
          )
          .toList(),
      'transcripts':
          Map<String, Transcript>.fromEntries(
            transcripts.entries.toList().reversed.take(1),
          ).map(
            (k, t) => MapEntry(
              k,
              (t.blocks.length > 40
                      ? t.blocks.sublist(t.blocks.length - 40)
                      : t.blocks)
                  .map(
                    (b) => {
                      'id': 'cache:${b.key}',
                      'seq': t.cursor,
                      'kind': b.kind,
                      'role': b.role,
                      'item': b.key.substring(
                        0,
                        b.key.length - b.kind.length - 1,
                      ),
                      'text': b.text,
                      'media': b.media,
                      'mode': 'snapshot',
                      'status': b.status,
                    },
                  )
                  .toList(),
            ),
          ),
    });
  }

  Future<void> forget() async {
    ++_generation;
    _retry?.cancel();
    _heartbeat?.cancel();
    _cacheTimer?.cancel();
    _cacheTimer = null;
    await _socket?.close();
    _socket = null;
    _http?.close(force: true);
    await _saveQueue;
    await _layoutSaveQueue;
    await vault.clear();
    projectLayout = ProjectLayout();
    _savedProjectLayout = projectLayout;
    device = null;
    demo = false;
    connected = false;
    selected = '';
    sessions = [];
    transcripts.clear();
    queues.clear();
    queueErrors.clear();
    images.clear();
    state = '尚未配对';
    error = '';
    notifyListeners();
  }

  void showDemo() {
    demo = true;
    state = '界面示例';
    sessions = [
      AgentSession(
        id: 'sample-codex',
        agent: 'codex',
        title: '构建一个更好的移动会话体验',
        source: 'stream',
        status: 'running',
      ),
      AgentSession(
        id: 'sample-claude',
        agent: 'claude',
        title: '审查认证模块与重连逻辑',
        source: 'log',
        status: 'completed',
      ),
    ];
    var i = 0;
    final t = Transcript();
    void add(String kind, String text, {String role = '', String media = ''}) {
      i++;
      t.add({
        'id': 'demo-$i',
        'seq': i,
        'kind': kind,
        'item': 'demo-$i',
        'text': text,
        'role': role,
        'media': media,
        'mode': 'snapshot',
      });
    }

    add('message', '帮我实现实时会话，并验证断线后的恢复。', role: 'user');
    add('thinking', '先核对事件顺序，再验证重连补齐。思考摘要与工具输出分别展示。');
    add('tool_call', 'go test -race ./...');
    add(
      'tool_result',
      'ok  agentdeck/internal/server  1.204s\nok  agentdeck/internal/secure  0.021s',
    );
    add(
      'message',
      '会话现在支持实时增量更新。\n\n```go\nstream.Subscribe(sessionID, cursor)\n```\n\n断线后会从最后一条已接收事件继续。',
      role: 'assistant',
    );
    transcripts['sample-codex'] = t;
    notifyListeners();
  }

  @override
  void dispose() {
    _closed = true;
    _notifyTimer?.cancel();
    ++_generation;
    _retry?.cancel();
    _heartbeat?.cancel();
    _cacheTimer?.cancel();
    _socket?.close();
    _http?.close(force: true);
    super.dispose();
  }
}

// HTTPS media is fetched by the phone with the same bounded, strict-TLS path.
bool mediaExtension(String name) => RegExp(
  r'\.(mp4|webm|mp3|wav|m4a|m4b|aac|flac|ogg|oga|opus|aif|aiff|amr)$',
  caseSensitive: false,
).hasMatch(name);
