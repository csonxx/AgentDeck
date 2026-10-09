import 'dart:convert';
import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:agentdeck/core/crypto.dart';

void main() {
  test(
    'Go and Dart derive identical epoch keys and decode the same AES-GCM frame',
    () async {
      final v =
          jsonDecode(await File('test/fixtures/crypto.json').readAsString())
              as Map<String, dynamic>;
      final root = base64Decode(v['root'] as String);
      final salt = base64Decode(v['salt'] as String);
      final key = await WireCrypto.traffic(root, salt, 's2c', 17);
      expect(base64Encode(key), v['key']);
      expect(
        await WireCrypto.open(key, 's2c', v['box'] as Map<String, dynamic>),
        v['plain'],
      );
      expect(
        base64Encode(await WireCrypto.traffic(root, salt, 's2c', 18)),
        isNot(v['key']),
      );
      expect(
        () => WireCrypto.open(key, 'c2s', v['box'] as Map<String, dynamic>),
        throwsA(anything),
      );
    },
  );
  test('encrypted cache rejects corruption and altered sequence', () async {
    final key = List<int>.generate(32, (i) => i);
    final b = await WireCrypto.seal(key, 'cache', 0, 0, {'private': '你好'});
    expect(b.toString(), isNot(contains('你好')));
    expect(await WireCrypto.open(key, 'cache', b), {'private': '你好'});
    b['seq'] = 1;
    expect(() => WireCrypto.open(key, 'cache', b), throwsA(anything));
  });
  test('pairing rejects insecure origins and bad pins', () {
    final m = {
      'version': 1,
      'url': 'http://evil.invalid',
      'pin': '0' * 64,
      'token': base64Encode(List.filled(32, 0)),
      'public': base64Encode(List.filled(32, 1)),
      'expires': 1,
    };
    expect(() => PairBundle.parse(jsonEncode(m)), throwsFormatException);
    m['url'] = 'https://server.invalid';
    final b = PairBundle.parse(
      'agentdeck://pair/${base64Encode(utf8.encode(jsonEncode(m)))}',
    );
    expect(b.url, 'https://server.invalid');
    expect(b.tlsTrust, 'pin');
    m['pin'] = '';
    expect(() => PairBundle.parse(jsonEncode(m)), throwsFormatException);
    m['tls_trust'] = 'system';
    expect(PairBundle.parse(jsonEncode(m)).tlsTrust, 'system');
    m['pin'] = '0' * 64;
    expect(() => PairBundle.parse(jsonEncode(m)), throwsFormatException);
    m['pin'] = '';
    m['tls_trust'] = 'insecure';
    expect(() => PairBundle.parse(jsonEncode(m)), throwsFormatException);
    m['tls_trust'] = 'system';
    m['url'] = 'http://server.invalid';
    expect(() => PairBundle.parse(jsonEncode(m)), throwsFormatException);
  });
}
