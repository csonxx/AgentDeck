import 'dart:convert';
import 'dart:math';
import 'package:cryptography/cryptography.dart';

bool validGatewayPrefix(String path) =>
    path.isEmpty || RegExp(r'^(/[a-zA-Z0-9_-]{1,80})+$').hasMatch(path);

class WireCrypto {
  // cryptography_flutter registers the platform factory before main. Native
  // AES handles catalogs/caches without lengthy pure-Dart work on the UI isolate;
  // standalone Dart tools retain the compatible fallback and wire format.
  static final aes = AesGcm.with256bits();
  static final hkdf = Hkdf(hmac: Hmac.sha256(), outputLength: 32);
  static List<int> random(int n) =>
      List.generate(n, (_) => Random.secure().nextInt(256));
  static Future<List<int>> derive(
    List<int> key,
    List<int> salt,
    String info,
  ) async => (await hkdf.deriveKey(
    secretKey: SecretKey(key),
    nonce: salt,
    info: utf8.encode(info),
  )).extractBytes();
  static Future<List<int>> traffic(
    List<int> root,
    List<int> salt,
    String direction,
    int epoch,
  ) => derive(root, salt, 'agentdeck/traffic/v1/$direction/$epoch');
  static Future<Map<String, dynamic>> seal(
    List<int> key,
    String scope,
    int epoch,
    int seq,
    Object value,
  ) async {
    final box = await aes.encrypt(
      utf8.encode(jsonEncode(value)),
      secretKey: SecretKey(key),
      nonce: random(12),
      aad: utf8.encode('agentdeck/v1/$scope/$epoch/$seq'),
    );
    return {
      'epoch': epoch,
      'seq': seq,
      'nonce': base64Encode(box.nonce),
      'data': base64Encode([...box.cipherText, ...box.mac.bytes]),
    };
  }

  static Future<Map<String, dynamic>> open(
    List<int> key,
    String scope,
    Map<String, dynamic> box,
  ) async {
    final data = base64Decode(box['data'] as String);
    if (data.length < 16) {
      throw const FormatException('Invalid encrypted frame');
    }
    final plain = await aes.decrypt(
      SecretBox(
        data.sublist(0, data.length - 16),
        nonce: base64Decode(box['nonce'] as String),
        mac: Mac(data.sublist(data.length - 16)),
      ),
      secretKey: SecretKey(key),
      aad: utf8.encode('agentdeck/v1/$scope/${box['epoch']}/${box['seq']}'),
    );
    return Map<String, dynamic>.from(jsonDecode(utf8.decode(plain)) as Map);
  }
}

class PairBundle {
  final String url, pin, token, publicKey, tlsTrust;
  final int expires;
  final Map<String, dynamic>? relay;
  PairBundle(
    this.url,
    this.pin,
    this.token,
    this.publicKey,
    this.expires, {
    this.tlsTrust = 'pin',
    this.relay,
  });
  factory PairBundle.parse(String text) {
    if (text.trim().startsWith('agentdeck://pair/')) {
      text = utf8.decode(
        base64Decode(text.trim().substring('agentdeck://pair/'.length)),
      );
    }
    final m = Map<String, dynamic>.from(jsonDecode(text) as Map);
    final u = Uri.parse(m['url'] as String);
    final trust = m['tls_trust'] as String? ?? 'pin';
    final pin = m['pin'] as String? ?? '';
    final relay = m['relay'] == null
        ? null
        : Map<String, dynamic>.from(m['relay'] as Map);
    if (relay != null) {
      final id = RegExp(r'^[a-zA-Z0-9_-]{1,80}$');
      if (!id.hasMatch(relay['local'] as String? ?? '') ||
          !id.hasMatch(relay['tenant'] as String? ?? '') ||
          !id.hasMatch(relay['grant'] as String? ?? '') ||
          base64Decode(relay['key'] as String).length != 32) {
        throw const FormatException('网关关联信息无效');
      }
    }
    final validTrust =
        (trust == 'system' && pin.isEmpty) ||
        (trust == 'pin' && RegExp(r'^[a-f0-9]{64}$').hasMatch(pin));
    if (m['version'] != 1 ||
        u.scheme != 'https' ||
        u.host.isEmpty ||
        u.userInfo.isNotEmpty ||
        u.hasQuery ||
        u.hasFragment ||
        (relay != null && u.toString() != m['url']) ||
        (relay == null ? u.path.isNotEmpty : !validGatewayPrefix(u.path)) ||
        u.toString().contains('%') ||
        !validTrust ||
        base64Decode(m['token'] as String).length != 32 ||
        base64Decode(m['public'] as String).length != 32) {
      throw const FormatException('配对信息无效');
    }
    return PairBundle(
      m['url'] as String,
      pin,
      m['token'] as String,
      m['public'] as String,
      m['expires'] as int,
      tlsTrust: trust,
      relay: relay,
    );
  }
}

// X.509 Certificate ::= SEQUENCE { tbsCertificate, signatureAlgorithm, signature }.
// Return the DER SubjectPublicKeyInfo unchanged. Pinning this key survives a
// normal certificate renewal with the same key. Reject malformed DER strictly.
List<int> certificatePublicKeyInfo(List<int> der) {
  ({int tag, int content, int end}) read(int start, int bound) {
    if (start < 0 || start + 2 > bound) {
      throw const FormatException('Invalid DER');
    }
    final tag = der[start];
    var size = der[start + 1];
    var offset = start + 2;
    if ((size & 0x80) != 0) {
      final n = size & 0x7f;
      if (n == 0 || n > 4 || offset + n > bound) {
        throw const FormatException('Invalid DER length');
      }
      size = 0;
      for (var i = 0; i < n; i++) {
        size = size * 256 + der[offset++];
      }
    }
    if (offset + size > bound) throw const FormatException('Truncated DER');
    return (tag: tag, content: offset, end: offset + size);
  }

  final cert = read(0, der.length);
  if (cert.tag != 0x30 || cert.end != der.length) {
    throw const FormatException('Invalid certificate');
  }
  final tbs = read(cert.content, cert.end);
  if (tbs.tag != 0x30) throw const FormatException('Invalid TBS certificate');
  var at = tbs.content;
  final first = read(at, tbs.end);
  if (first.tag == 0xa0) at = first.end;
  // serialNumber, signature, issuer, validity, subject precede SPKI.
  for (var i = 0; i < 5; i++) {
    at = read(at, tbs.end).end;
  }
  final spki = read(at, tbs.end);
  if (spki.tag != 0x30) throw const FormatException('Invalid SPKI');
  return der.sublist(at, spki.end);
}
