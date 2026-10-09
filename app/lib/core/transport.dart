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
  final u = Uri.parse(origin.trim());
  if (u.scheme != 'https' ||
      u.host.isEmpty ||
      u.userInfo.isNotEmpty ||
      u.hasQuery ||
      u.hasFragment ||
      u.path.isNotEmpty) {
    throw const FormatException('请输入 HTTPS 网关域名，不含路径');
  }
  return u.toString();
}
