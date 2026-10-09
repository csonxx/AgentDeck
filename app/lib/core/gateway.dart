import 'dart:convert';
import 'dart:io';
import 'package:cryptography/cryptography.dart';
import 'client.dart';
import 'crypto.dart';
import 'clock.dart';
import 'relay.dart';
import 'transport.dart';

// Feishu establishes account/routing ownership. It never supplies an agent
// decryption root. The first peer root is confirmed independently via SAS.
class GatewayLogin {
  final String origin;
  final HttpClient http;
  String flow = '', loginUrl = '';
  int expires = 0;
  List<int> _flowKey = [];
  Map<String, dynamic>? account;
  final ProtocolClock clock;
  GatewayLogin(
    String url, {
    SecurityContext? trustedRoots,
    ProtocolClock? clock,
  }) : origin = validateGatewayOrigin(url),
       http = createTransport('system', '', roots: trustedRoots),
       clock = clock ?? ProtocolClock();
  void close() => http.close(force: true);
  Future<Map<String, dynamic>> _post(
    String path,
    Object? body, {
    Map<String, String> headers = const {},
  }) async {
    final req = await http
        .postUrl(Uri.parse('$origin$path'))
        .timeout(const Duration(seconds: 12));
    req.followRedirects = false;
    headers.forEach((k, v) => req.headers.set(k, v));
    req.headers.contentType = ContentType.json;
    if (body != null) req.write(jsonEncode(body));
    final res = await req.close().timeout(const Duration(seconds: 15));
    if (res.statusCode != 200) {
      await res.drain<void>();
      throw HttpException('Gateway HTTP ${res.statusCode}');
    }
    return Map<String, dynamic>.from(
      jsonDecode(
            await res
                .transform(utf8.decoder)
                .join()
                .timeout(const Duration(seconds: 15)),
          )
          as Map,
    );
  }

  Future<void> _sync(String kind, String id, List<int> key) => synchronizeClock(
    clock,
    key,
    kind,
    id,
    (request) => _post('/v2/time-sync', request),
  );

  Future<void> begin() async {
    final algorithm = X25519();
    final keys = await algorithm.newKeyPair();
    final public = await keys.extractPublicKey();
    final v = await _post('/v2/auth/flows', {
      'kind': 'app',
      'name': 'C-APP',
      'public': base64Encode(public.bytes),
    });
    flow = v['flow'] as String;
    loginUrl = v['url'] as String;
    expires = v['expires'] as int;
    if (!RegExp(r'^[a-zA-Z0-9_-]{1,80}$').hasMatch(flow) ||
        loginUrl != '$origin/v2/auth/login?flow=$flow') {
      throw const FormatException('网关登录地址不一致');
    }
    final shared = await algorithm.sharedSecretKey(
      keyPair: keys,
      remotePublicKey: SimplePublicKey(
        base64Decode(v['public'] as String),
        type: KeyPairType.x25519,
      ),
    );
    _flowKey = await WireCrypto.derive(
      await shared.extractBytes(),
      base64Decode(v['salt'] as String),
      'agentdeck/auth-flow/v1/$flow',
    );
    await _sync('flow', flow, _flowKey);
  }

  Future<bool> poll() async {
    await _sync('flow', flow, _flowKey);
    if (clock.unix > expires) {
      throw const FormatException('飞书登录已过期，请重新开始');
    }
    const path = '/v2/auth/poll';
    final proof = await authorizationProof(
      _flowKey,
      'flow/$flow',
      'POST',
      path,
      [],
      '',
      unixSeconds: clock.unix,
    );
    final v = await _post(
      path,
      null,
      headers: {'Authorization': 'Flow $flow', 'X-Relay-Auth': proof},
    );
    final result = await WireCrypto.open(_flowKey, 'flow-result/$flow', v);
    if (result['challenge'] != bodyHash(utf8.encode(proof))) {
      throw const FormatException('网关登录校验失败');
    }
    if (result['status'] == 'failed') {
      throw const FormatException('飞书授权失败，请重新开始');
    }
    if (result['status'] != 'ready') return false;
    account = Map<String, dynamic>.from(
      (result['result'] as Map)['account'] as Map,
    );
    return true;
  }

  Future<dynamic> _account(String method, Map<String, dynamic> data) async {
    final a = account!;
    await _sync('account', a['id'] as String, base64Decode(a['key'] as String));
    if ((a['expires'] as int) < clock.unix) {
      throw const FormatException('请重新用飞书登录');
    }
    final key = base64Decode(a['key'] as String);
    final path = '/v2/account/$method';
    final body = await WireCrypto.seal(
      key,
      'account-request/${a['id']}',
      0,
      0,
      data,
    );
    final proof = await authorizationProof(
      key,
      'account/${a['id']}',
      'POST',
      path,
      utf8.encode(jsonEncode(body)),
      '',
      unixSeconds: clock.unix,
    );
    final box = await _post(
      path,
      body,
      headers: {'Authorization': 'Account ${a['id']}', 'X-Relay-Auth': proof},
    );
    final v = await WireCrypto.open(key, 'account-response/${a['id']}', box);
    if (v['challenge'] != bodyHash(utf8.encode(proof))) {
      throw const FormatException('网关请求校验失败');
    }
    return v['result'];
  }

  Future<List<Map<String, dynamic>>> locals() async =>
      (await _account('locals', {}) as List)
          .map((v) => Map<String, dynamic>.from(v as Map))
          .toList();
  Future<GatewayPair> offer(String local) async {
    final grant = Map<String, dynamic>.from(
      await _account('bind', {'local': local}) as Map,
    );
    if (grant['local'] != local) throw const FormatException('工作站关联不一致');
    final binding = <String, dynamic>{
      'tenant': grant['tenant'],
      'local': local,
      'grant': grant['id'],
      'key': grant['key'],
    };
    final keys = await X25519().newKeyPair();
    final pub = await keys.extractPublicKey();
    final path = relayPath(binding, 'offer');
    final body = {'public': base64Encode(pub.bytes)};
    final headers = await relayHeaders(
      binding,
      'POST',
      path,
      utf8.encode(jsonEncode(body)),
      unixSeconds: clock.unix,
    );
    final v = await _post(path, body, headers: headers);
    final b = PairBundle.parse(
      jsonEncode({
        'version': 1,
        'url': origin,
        'tls_trust': 'system',
        'pin': '',
        'relay': binding,
        'public': v['public'],
        'token': v['token'],
        'expires': v['expires'],
      }),
    );
    final shared = await X25519().sharedSecretKey(
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
    if (bodyHash(base64Decode(b.token)) != v['invite']) {
      throw const FormatException('配对邀请校验失败');
    }
    final code = await pairComparisonCode(root, v['invite'] as String);
    return GatewayPair(b, keys, code);
  }
}

class GatewayPair {
  final PairBundle bundle;
  final SimpleKeyPair keys;
  final String code;
  GatewayPair(this.bundle, this.keys, this.code);
  String get displayCode =>
      '${code.substring(0, 4)}-${code.substring(4, 8)}-${code.substring(8)}';
  Future<void> complete(AgentClient client) =>
      client.pairBundle(bundle, keyPair: keys);
}
