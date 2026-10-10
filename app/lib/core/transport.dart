import 'dart:io';
import 'package:crypto/crypto.dart' as hashing;
import 'crypto.dart';

HttpClient createTransport(String trust, String pin, {SecurityContext? roots}) {
  if (trust == 'system' && pin.isEmpty) {
    return HttpClient(context: roots)
      ..connectionTimeout = const Duration(seconds: 10);
  }
  if (trust != 'pin' || !RegExp(r'^[a-f0-9]{64}$').hasMatch(pin)) {
    throw const FormatException('Invalid TLS trust configuration');
  }
  final h = HttpClient(context: SecurityContext(withTrustedRoots: false));
  h.connectionTimeout = const Duration(seconds: 10);
  h.badCertificateCallback = (cert, host, port) {
    final got = hashing.sha256
        .convert(certificatePublicKeyInfo(cert.der))
        .toString();
    final now = DateTime.now();
    return got == pin &&
        !now.isBefore(cert.startValidity) &&
        !now.isAfter(cert.endValidity);
  };
  return h;
}

String validateGatewayOrigin(String origin) {
  final raw = origin.trim();
  final u = Uri.parse(raw);
  if (u.scheme != 'https' ||
      u.host.isEmpty ||
      u.userInfo.isNotEmpty ||
      u.hasQuery ||
      u.hasFragment ||
      u.toString() != raw ||
      !validGatewayPrefix(u.path) ||
      u.toString().contains('%')) {
    throw const FormatException('请输入 HTTPS 网关地址，可含 /agentdeck 前缀，不含末尾斜杠');
  }
  return u.toString();
}
