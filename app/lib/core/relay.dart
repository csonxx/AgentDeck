import 'dart:convert';
import 'package:crypto/crypto.dart' as hashing;
import 'crypto.dart';

String bodyHash(List<int> bytes) => hashing.sha256.convert(bytes).toString();
String relayPath(Map<String, dynamic>? relay, String operation) =>
    relay == null ? '/v1/$operation' : '/v2/relay/${relay['local']}/$operation';
Future<Map<String, String>> relayHeaders(
  Map<String, dynamic>? relay,
  String method,
  String path,
  List<int> body, {
  String authorization = '',
  required int unixSeconds,
}) async {
  if (relay == null) {
    return authorization.isEmpty ? {} : {'Authorization': authorization};
  }
  final proof = await authorizationProof(
    base64Decode(relay['key'] as String),
    'grant/${relay['grant']}',
    method,
    path,
    body,
    authorization,
    unixSeconds: unixSeconds,
  );
  return {
    'Authorization': 'Relay ${relay['grant']}',
    'X-Relay-Auth': proof,
    if (authorization.isNotEmpty) 'X-Local-Authorization': authorization,
  };
}

Future<String> authorizationProof(
  List<int> key,
  String scope,
  String method,
  String path,
  List<int> body,
  String authorization, {
  required int unixSeconds,
}) async {
  final box = await WireCrypto.seal(key, scope, 0, 0, {
    'time': unixSeconds,
    'method': method,
    'path': path,
    'body': bodyHash(body),
    'authorization': authorization,
  });
  return base64Encode(utf8.encode(jsonEncode(box)));
}

Future<String> pairComparisonCode(List<int> root, String invite) async {
  final key = await WireCrypto.derive(
    root,
    utf8.encode(invite),
    'agentdeck/pair-sas/v1',
  );
  return key.take(6).map((b) => b.toRadixString(16).padLeft(2, '0')).join();
}

class ApprovalPending implements Exception {}
