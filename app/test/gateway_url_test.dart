import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:agentdeck/core/crypto.dart';
import 'package:agentdeck/core/transport.dart';

void main() {
  test('gateway base preserves prefix and rejects ambiguous routes', () {
    for (final url in [
      'https://example.com',
      'https://example.com:8443/agentdeck',
      'https://example.com/team/agent-deck_1',
    ]) {
      expect(validateGatewayOrigin(url), url);
    }
    for (final url in [
      'http://example.com/agentdeck',
      'https://user@example.com',
      'https://example.com/',
      'https://example.com/a/',
      'https://example.com/a//b',
      'https://example.com/a/../b',
      'https://example.com/%61',
      'https://example.com/a%2fb',
      'https://example.com/a?',
      'https://example.com/a#',
    ]) {
      expect(
        () => validateGatewayOrigin(url),
        throwsFormatException,
        reason: url,
      );
    }
  });
  test('only relay pairing allows a gateway prefix', () {
    final bundle = <String, dynamic>{
      'version': 1,
      'url': 'https://example.com/agentdeck',
      'tls_trust': 'system',
      'pin': '',
      'token': base64Encode(List.filled(32, 0)),
      'public': base64Encode(List.filled(32, 1)),
      'expires': 1,
    };
    expect(() => PairBundle.parse(jsonEncode(bundle)), throwsFormatException);
    bundle['relay'] = {
      'local': 'local',
      'tenant': 'tenant',
      'grant': 'grant',
      'key': base64Encode(List.filled(32, 2)),
    };
    expect(
      PairBundle.parse(jsonEncode(bundle)).url,
      'https://example.com/agentdeck',
    );
  });
}
