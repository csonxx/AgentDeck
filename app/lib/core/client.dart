import 'permissions.dart';
import 'session_reads.dart';
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'usage.dart';
import 'goals.dart';
import 'app_lock.dart';
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
import 'download_manager.dart';
import 'app_update.dart';
import 'outbox.dart';
import 'client_health.dart';
import 'session_open_timing.dart';
import 'hybrid_socket.dart';
import 'peer_channel.dart';
import 'diagnostics.dart';
import 'feedback.dart';
export 'client_health.dart';

class AgentRequestException extends HttpException {
  final bool notSent;
  final bool remote;
  AgentRequestException(
    super.message, {
    this.notSent = false,
    this.remote = false,
  });
}

class AgentClient extends ChangeNotifier {
  final DeviceVault vault;
  String deviceName = 'AgentDeck ${Platform.operatingSystem}';
  bool deviceNamePending = false;
  Future<void> _acceptDeviceName(String? name) async {
    if (name == null || name.isEmpty || deviceNamePending) return;
    deviceName = name;
    try {
      await vault.saveDeviceName({'name': name, 'pending': false});
      health.resolve(LocalIssueKind.deviceName);
    } catch (_) {
      health.report(LocalIssueKind.deviceName, '设备名称保存失败，请重试');
    }
    notifyListeners();
  }

  Future<void> renameDevice(String name) async {
    name = name.trim();
    if (name.isEmpty ||
        name.runes.length > 80 ||
        RegExp(r'[\x00-\x1f\x7f]').hasMatch(name)) {
      throw const FormatException('设备名称无效，请输入 1–80 个字符');
    }
    await vault.saveDeviceName({'name': name, 'pending': true});
    health.resolve(LocalIssueKind.deviceName);
    deviceName = name;
    deviceNamePending = true;
    notifyListeners();
    if (!connected) return;
    final result = await request({'method': 'rename_device', 'text': name});
    deviceNamePending = false;
    await _acceptDeviceName((result as Map)['name'] as String);
  }

  AppUpdater? _updates;
  AppUpdater get updates => _updates ??= AppUpdater(
    vault,
    gateway: () async {
      if (device?['relay'] != null) return device!['url'] as String;
      final account = await vault.loadGatewayAccount();
      return account?['url'] as String? ?? defaultUpdateGateway;
    },
    createHttp: () => device?['relay'] != null
        ? _transport(
            device!['tls_trust'] as String? ?? 'pin',
            device!['pin'] as String,
          )
        : HttpClient(context: trustedRoots),
  );
  DownloadManager? _downloads;
  DownloadManager get downloads => _downloads ??= DownloadManager(this);
  OutboxManager? _outbox;
  OutboxManager get outbox => _outbox ??= OutboxManager(this);
  void notifyOutboxChanged() {
    if (!_closed) notifyListeners();
  }

  int _downloadLeases = 0;
  bool _backgrounded = false;
  void retainDownloadConnection() {
    _downloadLeases++;
    if (_suspended) {
      final background = _backgrounded;
      unawaited(
        resume().then((_) {
          _backgrounded = background;
        }),
      );
    }
  }

  void releaseDownloadConnection() {
    if (_downloadLeases > 0) _downloadLeases--;
    if (_backgrounded && _downloadLeases == 0) suspend();
  }

  late final AppLockController appLock = AppLockController(vault);
  // Tests inject a local CA for the TLS-terminating proxy. Production relies on
  // platform roots and never accepts invalid certificates in system mode.
  @visibleForTesting
  final SecurityContext? trustedRoots;
  final ProtocolClock clock;
  late final DiagnosticLog diagnosticLog = DiagnosticLog(vault, clock);
  FeedbackManager? _feedback;
  FeedbackManager get feedback => _feedback ??= FeedbackManager(
    vault: vault,
    log: diagnosticLog,
    exchange: (data) => request(data),
    binding: () => '${device?['url'] ?? ''}|${device?['device'] ?? 'unpaired'}',
    connected: () => connected,
    supported: () => localFeedback,
    version: () async {
      try {
        final info = await AppUpdater.channel
            .invokeMapMethod<String, dynamic>('info')
            .timeout(const Duration(seconds: 2));
        final v = '${info?['version'] ?? ''}+${info?['version_code'] ?? ''}';
        if (RegExp(r'^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$').hasMatch(v)) {
          return v;
        }
      } catch (_) {
        /* Host builds lack Android package metadata. */
      }
      return 'unknown';
    },
  );
  bool localFeedback = false;
  SessionOpenTiming? _sessionOpen;

  // Called after a frame containing the received history has actually painted.
  // The authenticated diagnostic packet is bounded metadata, never chat content.
  void historyPresented(String session, int renderedRevision) {
    final timing = _sessionOpen;
    if (timing == null || timing.session != session || selected != session) {
      return;
    }
    final metrics = timing.presented(clock.stamp, renderedRevision);
    if (metrics == null || !connected) return;
    diagnosticLog.record(
      'history_presented',
      request: timing.request,
      duration: metrics['total_ms'] ?? 0,
    );
    unawaited(
      send({
        'method': 'client_timing',
        'id': timing.request,
        'session': session,
        'timing': metrics,
      }).catchError((Object _) {}),
    );
  }

  AgentClient({
    DeviceVault? vault,
    this.trustedRoots,
    ProtocolClock? clock,
    PeerFactory? peerFactory,
  }) : vault = vault ?? DeviceVault(),
       clock = clock ?? ProtocolClock(),
       peerFactory = peerFactory ?? NativePeerChannel.create;
  Map<String, dynamic>? device;
  List<AgentSession> sessions = [];
  SessionReads sessionReads = SessionReads();
  bool isUnread(AgentSession session) => sessionReads.isUnread(session);
  void setReadingSession(String id) {
    sessionReads.visible = id;
    if (sessionReads.foreground && sessionReads.markRead(id)) {
      _scheduleCache(immediately: true);
      notifyListeners();
    }
  }

  void setReadingForeground(bool foreground) {
    sessionReads.foreground = foreground;
    if (foreground) setReadingSession(sessionReads.visible);
  }

  void updateSessions(List<AgentSession> value) {
    final before = jsonEncode(sessionReads.toJson());
    sessions = value;
    sessionReads.observe(value);
    if (jsonEncode(sessionReads.toJson()) != before) {
      _scheduleCache(immediately: true);
    }
  }

  void updateNativeReadState(Map<String, dynamic> state) {
    final before = jsonEncode(sessionReads.toJson());
    sessionReads.applyNative(state);
    sessionReads.observe(sessions);
    if (jsonEncode(sessionReads.toJson()) != before) {
      _scheduleCache(immediately: true);
    }
  }

  List<AgentGoal> goals = [];
  String goalError = '';
  bool _goalsLoading = false;
  Timer? _goalTimer;
  Future<void> refreshGoals() async {
    if (!connected || _goalsLoading) return;
    _goalsLoading = true;
    final generation = _generation;
    try {
      final value = await request({'method': 'goal_list'});
      if (generation != _generation || _closed) return;
      goals = (value as List)
          .map((v) => AgentGoal(Map<String, dynamic>.from(v as Map)))
          .where((g) => g.isCurrent)
          .toList();
      goalError = '';
    } catch (e) {
      if (generation == _generation && !_closed) goalError = e.toString();
    } finally {
      _goalsLoading = false;
      if (!_closed) notifyListeners();
    }
  }

  Future<void> changeGoal(AgentGoal goal, String action) async {
    try {
      await request({
        'method': 'goal_change',
        'session': goal.session,
        'goal_version': goal.version,
        'goal_action': action,
      });
    } finally {
      await refreshGoals();
    }
  }

  final Set<String> submittedInputs = {};
  final Map<String, String> submittedMessageIDs = {};
  String? consumeSubmittedMessageID(String id) =>
      submittedMessageIDs.remove(id);
  final Set<String> queuedInputs = {};
  bool consumeQueuedInput(String id) => queuedInputs.remove(id);
  bool consumeSubmittedInput(String id) => submittedInputs.remove(id);
  void _inputResult(String? id, dynamic result) {
    if (id != null && result is Map && result['delivery'] == 'queued') {
      queuedInputs.add(id);
      if (queuedInputs.length > 100) queuedInputs.remove(queuedInputs.first);
      return;
    }
    if (id == null || result is! Map || result['delivery'] != 'submitted') {
      return;
    }
    submittedInputs.add(id);
    if (result['message_id'] case final String nativeID
        when nativeID.isNotEmpty) {
      submittedMessageIDs[id] = nativeID;
      if (submittedMessageIDs.length > 100) {
        submittedMessageIDs.remove(submittedMessageIDs.keys.first);
      }
    }
    if (submittedInputs.length > 100) {
      submittedInputs.remove(submittedInputs.first);
    }
  }

  final Map<String, List<QueuedMessage>> queues = {};
  final Map<String, String> queueErrors = {};
  bool codexDesktop = false;
  bool atomicCodexSend = false;
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
      health.resolve(LocalIssueKind.agentVisibility);
    } catch (_) {
      health.report(LocalIssueKind.agentVisibility, 'Agent 显示设置读取失败');
    }
    if (!_closed) notifyListeners();
  }

  Future<void> updateAgentVisibility(AgentVisibility value) async {
    agentVisibility = value;
    notifyListeners();
    final save = _agentSaveQueue.then((_) async {
      await vault.saveAgentVisibility(value.toJson());
      _savedAgentVisibility = value;
      health.resolve(LocalIssueKind.agentVisibility);
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
  HybridSocket? _socket;
  final PeerFactory peerFactory;
  Timer? _directRetry;
  int _directAttemptGeneration = -1, _directCooldownUntil = 0;
  Map<String, dynamic>? _directPolicy;
  String directNote = '';
  String get connectionMethod => !connected || _socket == null
      ? ''
      : _socket?.direct == true
      ? 'webrtc'
      : device?['relay'] != null
      ? 'gateway'
      : 'https';
  String get connectionMethodLabel => switch (connectionMethod) {
    'webrtc' => '点对点直连',
    'gateway' => '网关中继',
    'https' => 'HTTPS 直连',
    _ => '',
  };

  HttpClient? _http;
  Timer? _retry, _heartbeat, _cacheTimer, _notifyTimer, _syncTimer;
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
  final ClientHealth health = ClientHealth();
  String get state => health.label;
  String get error => health.connectionFailure;
  bool get connected => health.connected;
  bool get live => health.live;
  String selected = '';
  bool demo = false, _closed = false, _connecting = false, _suspended = false;
  int get epoch => clock.millis ~/ _rotation;
  List<int> get _root => base64Decode(device!['root'] as String);

  Future<void> initialize() async {
    await loadProjectLayout();
    await loadAgentVisibility();
    try {
      device = await vault.loadDevice();
      health.resolve(LocalIssueKind.deviceStorage);
    } catch (_) {
      health.report(LocalIssueKind.deviceStorage, '无法读取本机安全存储，请重试或重新配对');
    }
    try {
      final preference = await vault.loadDeviceName();
      deviceName = preference?['name'] as String? ?? deviceName;
      deviceNamePending = preference?['pending'] == true;
      health.resolve(LocalIssueKind.deviceName);
    } catch (_) {
      health.report(LocalIssueKind.deviceName, '设备名称读取失败，请重试');
    }
    unawaited(downloads.restore());
    if (device != null) {
      try {
        final cache = await vault.readCache();
        sessionReads.restore(
          Map<String, dynamic>.from(cache['session_reads'] as Map? ?? {}),
        );
        sessions = (cache['sessions'] as List? ?? [])
            .map(
              (e) => AgentSession.fromJson(Map<String, dynamic>.from(e as Map)),
            )
            .toList();
        sessionReads.observe(sessions);
        for (final e in Map<String, dynamic>.from(
          cache['version'] == 3 ? cache['transcripts'] as Map? ?? {} : {},
        ).entries.take(3)) {
          final t = Transcript();
          final cached = e.value as List;
          for (final v in cached.skip(max(0, cached.length - 40))) {
            t.add(Map<String, dynamic>.from(v as Map));
          }
          final savedCursor = (cache['transcript_cursors'] as Map?)?[e.key];
          if (savedCursor is num && savedCursor.toInt() > t.cursor) {
            t.cursor = savedCursor.toInt();
          }
          transcripts[e.key] = t;
        }
        health.resolve(LocalIssueKind.cacheRead);
      } catch (_) {
        health.report(LocalIssueKind.cacheRead, '本机会话缓存读取失败，将从工作站同步');
      }
      // Corrupt/unavailable history is recoverable from Local, and must never
      // prevent reading valid pairing keys or establishing a fresh connection.
      await outbox.restore();
      await connect();
    }
    unawaited(updates.initialize());
    notifyListeners();
  }

  Future<void> loadProjectLayout() async {
    try {
      projectLayout = ProjectLayout.fromJson(
        await vault.loadProjectLayout() ?? {},
      );
      _savedProjectLayout = projectLayout;
      health.resolve(LocalIssueKind.projectLayout);
    } catch (_) {
      health.report(LocalIssueKind.projectLayout, '项目布局读取失败');
    }
    if (!_closed) notifyListeners();
  }

  Future<void> updateProjectLayout(ProjectLayout value) async {
    projectLayout = value;
    notifyListeners();
    final save = _layoutSaveQueue.then((_) async {
      await vault.saveProjectLayout(value.toJson());
      _savedProjectLayout = value;
      health.resolve(LocalIssueKind.projectLayout);
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

  final Set<LocalIssueKind> retryingIssues = {};
  void dismissIssue(LocalIssueKind kind) {
    health.dismiss(kind);
    notifyListeners();
  }

  Future<void> retryIssue(LocalIssueKind kind) async {
    if (_closed || !retryingIssues.add(kind)) return;
    final generation = _generation;
    notifyListeners();
    try {
      switch (kind) {
        case LocalIssueKind.deviceStorage:
          final saved = await vault.loadDevice();
          if (generation != _generation || _closed) return;
          device ??= saved;
          health.resolve(kind);
          if (device != null) await connect();
        case LocalIssueKind.projectLayout:
          await loadProjectLayout();
        case LocalIssueKind.agentVisibility:
          await loadAgentVisibility();
        case LocalIssueKind.deviceName:
          final issue = health.issues.where((i) => i.kind == kind).firstOrNull;
          if (issue?.message == '设备名称读取失败，请重试') {
            final saved = await vault.loadDeviceName();
            if (saved?['pending'] == true || !connected) {
              deviceName = saved?['name'] as String? ?? deviceName;
              deviceNamePending = saved?['pending'] == true;
            }
          }
          await vault.saveDeviceName({
            'name': deviceName,
            'pending': deviceNamePending,
          });
          health.resolve(kind);
          if (connected && deviceNamePending) await renameDevice(deviceName);
        case LocalIssueKind.cacheRead:
        case LocalIssueKind.cacheWrite:
          if (kind == LocalIssueKind.cacheRead && !live) {
            await refreshSessions();
          }
          await _queueCacheWrite();
      }
    } catch (_) {
      // Keep the existing scoped issue until its own recovery succeeds.
    } finally {
      retryingIssues.remove(kind);
      if (!_closed) notifyListeners();
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
        {'token': b.token, 'name': deviceName},
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
      _failPending();
      _retry?.cancel();
      _goalTimer?.cancel();
      _heartbeat?.cancel();
      _cacheTimer?.cancel();
      _cacheTimer = null;
      await _socket?.close();
      _socket = null;
      _http?.close(force: true);
      health.transition(ConnectionPhase.unpaired);
      await _saveQueue;
      await _layoutSaveQueue;
      await _downloads?.reset();
      await _outbox?.reset();
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
      health.resolve(LocalIssueKind.deviceStorage);
      demo = false;
      sessions = [];
      sessionReads = SessionReads();
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
    if (_closed) return;
    if (device?['revoked'] == true) {
      health.transition(ConnectionPhase.revoked, failure: '设备配对已被工作站删除，请重新配对');
      notifyListeners();
      return;
    }
    if (device == null || _closed || demo || _connecting || _suspended) return;
    _connecting = true;
    _directRetry?.cancel();
    _directAttemptGeneration = -1;
    _directPolicy = null;
    _retry?.cancel();
    final generation = ++_generation;
    _failPending();
    health.transition(ConnectionPhase.connecting);
    _syncTimer?.cancel();
    _goalTimer?.cancel();
    _heartbeat?.cancel();
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
      await _acceptDeviceName(welcome['device_name'] as String?);
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
      final nativeSocket = await WebSocket.connect(
        u.toString(),
        headers: headers,
        customClient: h,
      ).timeout(const Duration(seconds: 15));
      if (_closed || generation != _generation) {
        await nativeSocket.close();
        return;
      }
      final ws = HybridSocket(nativeSocket);
      _socket = ws;
      _sent = 0;
      _received = 0;
      _attempt = 0;
      _receiveQueue = Future.value();
      _sendQueue = Future.value();
      health.transition(ConnectionPhase.synchronizing);
      // Opening a socket does not prove that the displayed catalog is current.
      _syncTimer = Timer(const Duration(seconds: 12), () {
        if (generation == _generation && !live) {
          _lost(failure: '会话同步超时，正在重新连接');
        }
      });
      notifyListeners();
      ws.listen(
        (data) {
          // Validate expiry against arrival, not time spent decrypting earlier
          // frames in our serial queue (large attachments can take seconds).
          final arrivalEpoch = epoch;
          final arrivalStamp = clock.stamp;
          _receiveQueue = _receiveQueue
              .then(
                (_) => _receive(data, generation, arrivalEpoch, arrivalStamp),
              )
              .catchError((Object failure) {
                assert(() {
                  debugPrint(
                    'AgentDeck receive failed: ${failure.runtimeType}',
                  );
                  return true;
                }());
                if (generation == _generation) _lost();
              });
        },
        onError: (Object _) {
          if (generation == _generation) _lost();
        },
        onDone: () {
          if (generation == _generation) {
            if (ws.closeCode == 4003) {
              device?['revoked'] = true;
              if (device != null) {
                unawaited(vault.saveDevice(device!).catchError((Object _) {}));
              }
            }
            _lost();
          }
        },
      );
      _goalTimer?.cancel();
      _heartbeat?.cancel();
      var heartbeatPending = false;
      _heartbeat = Timer.periodic(const Duration(seconds: 20), (_) {
        if (heartbeatPending) return;
        heartbeatPending = true;
        unawaited(
          request({'method': 'ping'}, timeout: const Duration(seconds: 45))
              .catchError((Object _) {
                if (generation == _generation) _lost();
                return null;
              })
              .whenComplete(() => heartbeatPending = false),
        );
      });
      unawaited(refreshGoals());
      _goalTimer?.cancel();
      _goalTimer = Timer.periodic(
        const Duration(seconds: 5),
        (_) => unawaited(refreshGoals()),
      );
      if (selected.isNotEmpty) await watch(selected);
      if (deviceNamePending) {
        unawaited(
          renameDevice(deviceName).catchError((Object _) {
            notifyListeners();
          }),
        );
      }
    } catch (e) {
      if (generation == _generation) {
        _lost(
          failure: e is HandshakeException
              ? '证书校验失败，请检查后端证书或重新配对'
              : '连接失败，请检查域名、网络或设备授权',
        );
      }
    } finally {
      _connecting = false;
    }
  }

  Future<void> _tryDirect() async {
    final generation = _generation;
    final socket = _socket;
    final policy = _directPolicy;
    if (!live ||
        _suspended ||
        socket == null ||
        socket.direct ||
        device?['relay'] == null ||
        policy?['version'] != 1 ||
        _directAttemptGeneration == generation) {
      return;
    }
    _directAttemptGeneration = generation;
    if (policy?['enabled'] != true) {
      directNote = '网关已关闭直连';
      notifyListeners();
      return;
    }
    final servers = (policy?['stun_servers'] as List? ?? [])
        .whereType<String>()
        .toList();
    if (servers.length > 4 ||
        servers.any((s) => !s.startsWith('stun:') || s.length > 512)) {
      return;
    }
    if (clock.stamp < _directCooldownUntil) {
      _scheduleDirectRetry(generation);
      return;
    }
    PeerChannel? peer;
    var commitStarted = false;
    bool current() =>
        !_closed && generation == _generation && identical(_socket, socket);
    directNote = '正在尝试直连，当前使用网关中继';
    notifyListeners();
    try {
      var abandoned = false;
      final creating = peerFactory(servers);
      unawaited(
        creating.then<void>((value) {
          if (abandoned || !current()) unawaited(value.close());
        }, onError: (Object _, StackTrace _) {}),
      );
      try {
        peer = await creating.timeout(const Duration(seconds: 5));
      } catch (_) {
        abandoned = true;
        rethrow;
      }
      if (!current()) {
        await peer.close();
        return;
      }
      socket.attach(peer);
      final offer = await peer.offer();
      if (!current()) return;
      final answer = Map<String, dynamic>.from(
        await request({
              'method': 'peer_offer',
              'sdp': offer,
            }, timeout: const Duration(seconds: 12))
            as Map,
      );
      if (!current()) return;
      final id = answer['peer_id'];
      if (id is! String ||
          id.isEmpty ||
          id.length > 80 ||
          answer['sdp'] is! String) {
        throw const FormatException('Invalid peer negotiation');
      }
      await peer.answer(answer['sdp'] as String);
      await peer.ready;
      if (!current()) return;
      commitStarted = true;
      final result = await request({
        'method': 'peer_commit',
        'peer_id': id,
      }, timeout: const Duration(seconds: 5));
      if (!current()) return;
      if (result is! Map || result['transport'] != 'webrtc') {
        throw const FormatException('Invalid peer commit');
      }
      socket.activate();
      directNote = '';
      notifyListeners();
    } catch (_) {
      if (!current()) return;
      directNote = servers.isEmpty
          ? '未配置 STUN，局域网或 IPv6 直连未成功，继续使用网关中继'
          : '直连未成功，继续使用网关中继';
      if (commitStarted) {
        _lost(failure: '直连切换未完成，正在恢复网关中继');
      } else {
        await socket.detach();
        _scheduleDirectRetry(generation);
        if (current()) notifyListeners();
      }
    } finally {
      if (!current()) await peer?.close();
    }
  }

  void _scheduleDirectRetry(int generation) {
    _directRetry?.cancel();
    _directRetry = Timer(const Duration(minutes: 2), () {
      if (_closed || generation != _generation || !live || _suspended) return;
      _directAttemptGeneration = -1;
      unawaited(_tryDirect());
    });
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
    unawaited(diagnosticLog.flush());
    setReadingForeground(false);
    _backgrounded = true;
    if (_downloadLeases > 0) return;
    if (_suspended || _closed) return;
    _suspended = true;
    _lost();
    _http?.close(force: true);
    if (!_closed) notifyListeners();
  }

  Future<void> resume() async {
    _backgrounded = false;
    _outbox?.foreground();
    if (!_suspended || _closed) return;
    _suspended = false;
    // A cancelled handshake must finish before a fresh one can start.
    while (_connecting && !_closed && !_suspended) {
      await Future<void>.delayed(const Duration(milliseconds: 50));
    }
    if (!_closed && !_suspended) await connect();
  }

  void _lost({String? failure}) {
    if (_closed || demo) return;
    diagnosticLog.record('disconnected');
    if (_socket?.direct == true) {
      _directCooldownUntil = clock.stamp + 60000;
      directNote = '直连已中断，已回退网关中继';
    }
    _directRetry?.cancel();
    ++_generation;
    final revoked = device?['revoked'] == true;
    health.transition(
      revoked
          ? ConnectionPhase.revoked
          : device == null
          ? ConnectionPhase.unpaired
          : _suspended
          ? ConnectionPhase.suspended
          : ConnectionPhase.reconnecting,
      failure: revoked
          ? '设备配对已被工作站删除，请重新配对'
          : _suspended || device == null
          ? ''
          : failure ?? '连接中断',
    );
    _syncTimer?.cancel();
    _goalTimer?.cancel();
    _heartbeat?.cancel();
    _socket?.close();
    _socket = null;
    _retry?.cancel();
    final delay = min(30, 1 << min(_attempt++, 5));
    if (!_suspended && device != null && !revoked) {
      _retry = Timer(Duration(seconds: delay), connect);
    }
    notifyListeners();
    _failPending();
  }

  void _failPending() {
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
        throw AgentRequestException('未连接', notSent: true);
      }
      final ep = epoch;
      final key = await WireCrypto.traffic(_root, _salt, 'c2s', ep);
      final b = await WireCrypto.seal(key, 'c2s', ep, ++_sent, data);
      if (generation != _generation || !connected) {
        throw AgentRequestException('连接已更新', notSent: true);
      }
      if (data['method'] == 'watch' && _sessionOpen?.request == data['id']) {
        _sessionOpen?.sent = clock.stamp;
      }
      await _socket!.add(jsonEncode(b));
    });
    _sendQueue = task.catchError((Object _) {});
    return task;
  }

  Future<void> _receive(
    dynamic raw,
    int generation,
    int arrivalEpoch,
    int arrivalStamp,
  ) async {
    if (generation != _generation) return;
    final decodingStamp = clock.stamp;
    if (raw is! String || raw.length > 24 << 20) {
      throw const FormatException('Invalid frame');
    }
    final b = Map<String, dynamic>.from(jsonDecode(raw) as Map);
    final ep = b['epoch'] as int;
    if (b['seq'] != _received + 1 || (ep - arrivalEpoch).abs() > 1) {
      assert(() {
        debugPrint(
          'AgentDeck frame rejected: sequence=${b['seq']}, '
          'expected=${_received + 1}, epoch_delta=${ep - arrivalEpoch}',
        );
        return true;
      }());
      throw const FormatException('Replay or expired frame');
    }
    final m = await WireCrypto.open(
      await WireCrypto.traffic(_root, _salt, 's2c', ep),
      's2c',
      b,
    );
    if (generation != _generation) return;
    final decodedStamp = clock.stamp;
    _received = b['seq'] as int;
    switch (m['type']) {
      case 'device':
        await _acceptDeviceName(m['name'] as String?);
      case 'sessions':
        localFeedback = m['feedback_local'] == true;
        final direct = m['direct'];
        _directPolicy = direct is Map
            ? Map<String, dynamic>.from(direct)
            : null;
        codexDesktop = m['codex_desktop'] == true;
        atomicCodexSend = m['codex_send_direct'] == true;
        _updateAgents(m);
        projects = (m['projects'] as List? ?? [])
            .map((p) => AgentProject.fromJson(Map<String, dynamic>.from(p)))
            .toList();
        updateSessions(
          (m['sessions'] as List)
              .map(
                (e) =>
                    AgentSession.fromJson(Map<String, dynamic>.from(e as Map)),
              )
              .toList(),
        );
        updateNativeReadState(
          Map<String, dynamic>.from(m['native_reads'] as Map? ?? {}),
        );
        health.synchronized();
        _syncTimer?.cancel();
        unawaited(_tryDirect());
        _requests.remove(m['id'])?.complete(m);
      case 'agents':
        _updateAgents(m);
      case 'native_reads':
        if (m['agent'] == 'codex') {
          updateNativeReadState(
            Map<String, dynamic>.from(m['state'] as Map? ?? {}),
          );
        }
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
              .liveSnapshot(Map<String, dynamic>.from(m['page'] as Map));
          final timing = _sessionOpen;
          if (timing?.session == id && timing?.request == m['id']) {
            timing!.received(
              arrival: arrivalStamp,
              decodeStart: decodingStamp,
              decodeEnd: decodedStamp,
              applyEnd: clock.stamp,
              bytes: raw.length,
              revision: transcripts[id]!.revision,
            );
          }
        }
      case 'events':
        final id = m['session'] as String;
        if (id != selected) break;
        final t = transcripts.putIfAbsent(id, Transcript.new);
        for (final ev in m['events'] as List) {
          final event = Map<String, dynamic>.from(ev as Map);
          final seq = (event['seq'] as num).toInt();
          if (seq > t.latestLiveCursor) t.latestLiveCursor = seq;
          final retained = t.containsEvent(event);
          if (t.readingHistory &&
              !retained &&
              event['kind'] != 'status' &&
              ((event['first'] ?? seq) as num).toInt() >
                  (t.blocks.lastOrNull?.first ?? 0)) {
            t.detached = true;
          }
          if ((!t.detached && !t.readingHistory) || retained) t.add(event);
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
      case 'pong':
        _requests.remove(m['id'])?.complete();
      case 'result':
        _requests.remove(m['id'])?.complete(m['result']);
      case 'error':
        _requests
            .remove(m['id'])
            ?.completeError(
              AgentRequestException(
                m['message'] as String,
                notSent: m['not_sent'] == true,
                remote: true,
              ),
            );
        // Only the matching caller owns this failure (send, file, usage,
        // permissions, etc). A late error must not become a global outage.
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

  // Account reads and explicit reset requests share one lane. The server has
  // two async slots; keep the other available for home goals/status queries.
  // Queueing never retries a request, especially an explicit card redemption.
  Future<void> _codexAccountQueue = Future.value();
  Future<Map<String, dynamic>> _codexAccountRequest(
    Map<String, dynamic> data, {
    Duration timeout = const Duration(seconds: 70),
  }) {
    final task = _codexAccountQueue.then(
      (_) async => Map<String, dynamic>.from(
        await request({...data, 'agent': 'codex'}, timeout: timeout),
      ),
    );
    _codexAccountQueue = task.then<void>(
      (_) {},
      onError: (Object _, StackTrace _) {},
    );
    return task;
  }

  Future<AccountUsage> accountUsage(String agent) async => AccountUsage(
    agent == 'codex'
        ? await _codexAccountRequest({'method': 'usage'})
        : Map<String, dynamic>.from(
            await request({'method': 'usage', 'agent': agent}),
          ),
  );

  Future<Map<String, dynamic>> codexResetState() =>
      _codexAccountRequest({'method': 'usage_resets'});
  Future<Map<String, dynamic>> codexResetHistory([String cursor = '']) =>
      _codexAccountRequest({
        'method': 'usage_reset_history',
        'reset_cursor': cursor,
      });
  Future<Map<String, dynamic>> prepareCodexReset(
    String credit,
    String account,
  ) => _codexAccountRequest({
    'method': 'usage_reset_prepare',
    'reset_credit': credit,
    'reset_account': account,
  });
  Future<Map<String, dynamic>> consumeCodexReset(String intent, String id) =>
      _codexAccountRequest({
        'method': 'usage_reset_consume',
        'reset_intent': intent,
        'id': id,
        'confirmed': true,
      }, timeout: const Duration(seconds: 60));

  static String requestID() => List.generate(
    16,
    (_) => Random.secure().nextInt(256).toRadixString(16).padLeft(2, '0'),
  ).join();

  Future<void>? _sessionsRefresh;
  Future<void> refreshSessions() {
    return _sessionsRefresh ??= _refreshSessions().whenComplete(() {
      _sessionsRefresh = null;
    });
  }

  Future<void> _refreshSessions() async {
    if (!connected) {
      if (device == null) throw const HttpException('请先连接工作站');
      await (() async {
        while (_connecting && !connected && !_closed && !_suspended) {
          await Future<void>.delayed(const Duration(milliseconds: 50));
        }
        if (!connected) await connect();
      })().timeout(const Duration(seconds: 20));
    }
    if (!connected) throw const HttpException('连接失败，请检查域名、网络或设备授权');
    try {
      await request({
        'method': 'sessions',
      }, timeout: const Duration(seconds: 12));
    } on TimeoutException {
      _lost(failure: '会话同步超时，正在重新连接');
      rethrow;
    }
    unawaited(refreshGoals());
  }

  Future<dynamic> request(
    Map<String, dynamic> data, {
    Duration timeout = const Duration(seconds: 70),
  }) async {
    final id = data['id'] as String? ?? requestID();
    final started = clock.stamp;
    final sending = data['method'] == 'send';
    if (sending) diagnosticLog.record('send_started', request: id);
    final result = Completer<dynamic>();
    _requests[id] = result;
    final response = result.future.timeout(timeout);
    // A disconnect can complete the request while crypto/send is still
    // awaited. Attach an error listener immediately; still propagate it below.
    unawaited(
      response.then<void>((_) {}, onError: (Object _, StackTrace _) {}),
    );
    try {
      await send({...data, 'id': id});
      final value = await response;
      if (sending) {
        diagnosticLog.record(
          'send_ack',
          request: id,
          duration: clock.stamp - started,
        );
      }
      return value;
    } catch (e) {
      diagnosticLog.record(
        sending
            ? 'send_error'
            : e is TimeoutException
            ? 'request_timeout'
            : 'request_error',
        request: id,
        duration: clock.stamp - started,
      );
      rethrow;
    } finally {
      _requests.remove(id);
      if (!result.isCompleted) result.complete();
    }
  }

  Future<AgentSession> createSession(
    String agent,
    String project, {
    String parent = '',
    String permissionMode = '',
  }) async {
    final result = await request({
      'method': 'create',
      if (permissionMode.isNotEmpty) 'permission_mode': permissionMode,
      'agent': agent,
      'project': project,
      'session': parent,
    });
    final session = AgentSession.fromJson(Map<String, dynamic>.from(result));
    if (!sessions.any((s) => s.id == session.id)) sessions.insert(0, session);
    await watch(session.id);
    return session;
  }

  // An outbox job must never switch the visible/watch session in another route.
  Future<AgentSession> prepareOutgoingSession(
    String agent,
    String project,
    String parent,
    String id,
  ) async {
    final result = await request({
      'method': 'create',
      'agent': agent,
      'project': project,
      'session': parent,
      'id': id,
    });
    final value = AgentSession.fromJson(Map<String, dynamic>.from(result));
    final index = sessions.indexWhere((s) => s.id == value.id);
    if (index < 0) {
      sessions.insert(0, value);
    } else {
      sessions[index] = value;
    }
    return value;
  }

  Future<void> sendPrompt(
    String session,
    String text, {
    String? requestId,
  }) async {
    final result = await request({
      'method': 'send',
      'session': session,
      'text': text,
      'id': ?requestId,
    });
    _inputResult(requestId, result);
  }

  Future<void> sendInput(
    String session,
    String text,
    TurnOptions options,
    List<String> files, {
    String? requestId,
  }) async {
    if (options.inherited && files.isEmpty) {
      return sendPrompt(session, text, requestId: requestId);
    }
    final result = await request({
      'method': 'send',
      'session': session,
      'text': text,
      ...options.toJson(),
      'attachments': files,
      'id': ?requestId,
    });
    _inputResult(requestId, result);
  }

  Future<void> sendDelivery(
    String session,
    String text,
    TurnOptions options,
    List<String> files,
    String delivery, {
    String? requestId,
  }) async {
    if (delivery == 'send') {
      return sendInput(session, text, options, files, requestId: requestId);
    }
    final result = await request({
      'method': 'send',
      'session': session,
      'text': text,
      ...options.toJson(),
      'attachments': files,
      'delivery': delivery,
      'id': ?requestId,
    });
    _inputResult(requestId, result);
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
  Future<SessionPermissions> sessionPermissions(String session) async =>
      SessionPermissions.fromJson(
        Map<String, dynamic>.from(
          await request({
                'method': 'session_permissions',
                'session': session,
              }, timeout: const Duration(seconds: 12))
              as Map,
        ),
      );
  Future<SessionPermissions> changePermissions(
    String session,
    String version,
    String mode,
  ) async => SessionPermissions.fromJson(
    Map<String, dynamic>.from(
      await request({
            'method': 'session_permissions_change',
            'session': session,
            'permission_version': version,
            'permission_mode': mode,
          }, timeout: const Duration(seconds: 20))
          as Map,
    ),
  );

  Future<SessionGeneration> sessionGeneration(String session) async =>
      SessionGeneration.fromJson(
        Map<String, dynamic>.from(
          await request({'method': 'session_settings', 'session': session})
              as Map,
        ),
      );
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
    final timing = id.isEmpty
        ? null
        : SessionOpenTiming(id, requestID(), clock.stamp);
    _sessionOpen = timing;
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
      await send({
        'method': 'watch',
        if (timing != null) 'id': timing.request,
        'session': id,
        'recent': true,
        'compact': true,
      });
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
      'compact': true,
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
    if (t == null ||
        !t.hasMore ||
        t.loadingOlder ||
        t.loadingNewer ||
        !connected) {
      return;
    }
    t.loadingOlder = true;
    final windowVersion = t.windowVersion;
    notifyListeners();
    try {
      final page = await request({
        'method': 'history',
        'compact': true,
        'session': id,
        'before': t.before,
      });
      if (identical(transcripts[id], t) && t.windowVersion == windowVersion) {
        t.history(Map<String, dynamic>.from(page as Map));
        _scheduleCache();
      }
    } finally {
      t.loadingOlder = false;
      if (!_closed) notifyListeners();
    }
  }

  Future<void> loadNewer(String id) async {
    final t = transcripts[id];
    if (t == null ||
        !t.detached ||
        t.blocks.isEmpty ||
        t.loadingOlder ||
        t.loadingNewer ||
        !connected) {
      return;
    }
    t.loadingNewer = true;
    final windowVersion = t.windowVersion;
    notifyListeners();
    try {
      final page = await request({
        'method': 'history_after',
        'compact': true,
        'session': id,
        'after': t.blocks.last.first,
      });
      if (identical(transcripts[id], t) && t.windowVersion == windowVersion) {
        t.history(Map<String, dynamic>.from(page as Map), newer: true);
        _scheduleCache();
      }
    } finally {
      t.loadingNewer = false;
      if (!_closed) notifyListeners();
    }
  }

  Future<void> unwatch(String id) async {
    if (selected != id) return;
    _sessionOpen = null;
    selected = '';
    if (connected) await send({'method': 'watch', 'session': ''});
  }

  // A document picker may return before the resumed lifecycle and handshake.
  HttpClient attachmentHttpClient() => HttpClient(context: trustedRoots);

  Future<void> prepareAttachmentTransfer() async {
    if (connected) return;
    if (device == null) throw const HttpException('当前未连接工作站');
    await (() async {
      while (_suspended && !_closed) {
        await Future<void>.delayed(const Duration(milliseconds: 50));
      }
      if (_closed) throw const HttpException('当前未连接工作站');
      if (!connected) await refreshSessions();
    })().timeout(const Duration(seconds: 30));
  }

  Future<void> reload(String id) async {
    // Commit a refresh only after a successful reply; failed/late refreshes
    // leave the current transcript and its reading position intact.
    final page = await request({
      'method': 'history',
      'session': id,
      'compact': true,
    });
    if (selected != id) return;
    transcripts
        .putIfAbsent(id, Transcript.new)
        .refreshLatest(Map<String, dynamic>.from(page as Map));
    _scheduleCache();
    notifyListeners();
  }

  Future<Map<String, dynamic>> executionDetail(
    String session,
    TranscriptBlock block,
  ) async => Map<String, dynamic>.from(
    await request({
          'method': 'item_detail',
          'session': session,
          'item': block.key.substring(
            0,
            block.key.length - block.kind.length - 1,
          ),
          'kind': block.kind,
        })
        as Map,
  );

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
      if (href.length > 12 << 20) {
        return SessionAttachment(
          name: 'image',
          mime: uri.data!.mimeType,
          bytes: Uint8List(0),
          size: -1,
          downloadRequired: true,
        );
      }
      final data = uri.data!;
      final bytes = data.contentAsBytes();
      if (!data.mimeType.startsWith('image/')) {
        throw const FormatException('图片超过 8 MiB');
      }
      return SessionAttachment(
        name: '会话图片',
        generatedName: true,
        mime: data.mimeType,
        size: bytes.length,
        downloadRequired: bytes.length > 8 << 20,
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
          if (res.statusCode != 200) {
            throw const HttpException('图片加载失败');
          }
          final remoteMime =
              res.headers.contentType?.mimeType ?? 'application/octet-stream';
          if (res.contentLength > 8 << 20) {
            return SessionAttachment(
              name: target.pathSegments.lastOrNull ?? 'attachment',
              mime: remoteMime,
              bytes: Uint8List(0),
              size: res.contentLength,
              downloadRequired: true,
            );
          }
          final bytes = BytesBuilder(copy: false);
          await for (final chunk in res.timeout(const Duration(seconds: 12))) {
            if (bytes.length + chunk.length > 8 << 20) {
              return SessionAttachment(
                name: target.pathSegments.lastOrNull ?? 'attachment',
                mime: remoteMime,
                bytes: Uint8List(0),
                size: -1,
                downloadRequired: true,
              );
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
      bytes: Uint8List.fromList(base64Decode(result['bytes'] as String? ?? '')),
      line: (result['line'] as num?)?.toInt() ?? 0,
      size: (result['size'] as num?)?.toInt() ?? 0,
      downloadRequired: result['download_required'] == true,
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
    final response = c.future.timeout(const Duration(seconds: 15));
    unawaited(
      response.then<void>((_) {}, onError: (Object _, StackTrace _) {}),
    );
    try {
      await send({'method': 'media', 'id': id, 'media': id});
      return await response;
    } catch (e, stack) {
      if (!c.isCompleted) c.completeError(e, stack);
      rethrow;
    } finally {
      _media.remove(id);
    }
  }

  void _scheduleCache({bool immediately = false}) {
    // Coalesce periodic writes: one pending save, one small recent transcript.
    if (immediately) {
      _cacheTimer?.cancel();
      _cacheTimer = null;
    }
    _cacheTimer ??= Timer(
      immediately ? Duration.zero : const Duration(seconds: 5),
      () {
        _cacheTimer = null;
        if (_cacheWriting) {
          _scheduleCache();
          return;
        }
        _cacheWriting = true;
        _queueCacheWrite().whenComplete(() => _cacheWriting = false);
      },
    );
  }

  Future<void> _queueCacheWrite() {
    _saveQueue = _saveQueue.then((_) => _persist()).catchError((Object _) {
      health.report(LocalIssueKind.cacheWrite, '加密缓存写入失败');
      if (!_closed) notifyListeners();
    });
    return _saveQueue;
  }

  Future<void> _persist() async {
    if (demo || device == null) return;
    await vault.writeCache({
      'version': 3,
      'session_reads': sessionReads.toJson(),
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
              'completion': s.completion,
              'completed_at': s.completedAt,
              if (s.issue != null) 'issue': s.issue!.toJson(),
            },
          )
          .toList(),
      'transcript_cursors': {
        for (final entry in transcripts.entries) entry.key: entry.value.cursor,
      },
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
                      'seq': b.seq,
                      'first': b.first,
                      'position': b.position,
                      'detail': b.detail,
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
    final hadIssue = health.issues.any(
      (i) =>
          i.kind == LocalIssueKind.cacheRead ||
          i.kind == LocalIssueKind.cacheWrite,
    );
    health.resolve(LocalIssueKind.cacheRead);
    health.resolve(LocalIssueKind.cacheWrite);
    if (hadIssue && !_closed) notifyListeners();
  }

  Future<void> forget() async {
    diagnosticLog.enabled = false;
    ++_generation;
    _failPending();
    _retry?.cancel();
    _goalTimer?.cancel();
    _directRetry?.cancel();
    _heartbeat?.cancel();
    _syncTimer?.cancel();
    _cacheTimer?.cancel();
    _cacheTimer = null;
    await _socket?.close();
    _socket = null;
    _http?.close(force: true);
    await _saveQueue;
    await _layoutSaveQueue;
    await _downloads?.reset();
    await _outbox?.reset();
    await _feedback?.reset();
    await diagnosticLog.clear();
    await vault.clear();
    localFeedback = false;
    diagnosticLog.enabled = true;
    projectLayout = ProjectLayout();
    _savedProjectLayout = projectLayout;
    device = null;
    demo = false;
    selected = '';
    sessions = [];
    sessionReads = SessionReads();
    goals = [];
    goalError = '';
    transcripts.clear();
    queues.clear();
    queueErrors.clear();
    images.clear();
    health.reset();
    notifyListeners();
  }

  void showDemo() {
    demo = true;
    health.transition(ConnectionPhase.demo);
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
    _feedback?.dispose();
    diagnosticLog.dispose();
    _updates?.dispose();
    _closed = true;
    _outbox?.dispose();
    _downloads?.dispose();
    appLock.dispose();
    _notifyTimer?.cancel();
    ++_generation;
    _failPending();
    _retry?.cancel();
    _goalTimer?.cancel();
    _directRetry?.cancel();
    _heartbeat?.cancel();
    _syncTimer?.cancel();
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
