import 'package:flutter_test/flutter_test.dart';
import 'package:agentdeck/core/clock.dart';
import 'package:agentdeck/core/crypto.dart';

void main() {
  test('Clock ignores wall jumps and crosses a real 15-minute boundary', () {
    var wall = 1000000000, elapsed = 0;
    final c = ProtocolClock(
      wallMillis: () => wall,
      monotonicMillis: () => elapsed,
    );
    c.calibrate(900000 * 2000 + 899999, 0, 0);
    final before = c.millis ~/ 900000;
    wall += 86400000;
    expect(c.millis ~/ 900000, before);
    elapsed++;
    expect(c.millis ~/ 900000, before + 1);
    wall -= 3 * 86400000;
    elapsed += 15000;
    expect(c.millis, 900000 * 2001 + 15000);
    expect(() => c.calibrate(0, 1, elapsed), throwsFormatException);
    expect(() => c.calibrate(100, -1, elapsed), throwsFormatException);
    expect(() => c.calibrate(100, 10001, elapsed), throwsFormatException);
    expect(c.millis, 900000 * 2001 + 15000);
  });
  test('Authenticated sample binds key, scope, challenge and RTT', () async {
    final key = WireCrypto.random(32);
    for (final mode in ['valid', 'challenge', 'key', 'scope', 'seq', 'slow']) {
      var elapsed = 0;
      final c = ProtocolClock(
        wallMillis: () => 1,
        monotonicMillis: () => elapsed,
      );
      Future<void> sync() =>
          synchronizeClock(c, key, 'device', 'test', (request) async {
            final v = await WireCrypto.open(
              key,
              'clock-request/device/test',
              request['box'],
            );
            elapsed = mode == 'slow' ? 10001 : 200;
            return WireCrypto.seal(
              mode == 'key' ? WireCrypto.random(32) : key,
              mode == 'scope'
                  ? 'clock-reply/grant/test'
                  : 'clock-reply/device/test',
              0,
              mode == 'seq' ? 1 : 0,
              {
                'challenge': mode == 'challenge'
                    ? 'stale-response'
                    : v['challenge'],
                'server_time_ms': 1770000000000,
                'version': 1,
              },
            );
          });
      if (mode == 'valid') {
        await sync();
        expect(c.calibrated, isTrue);
        expect(c.millis, 1770000000100);
        elapsed += 10;
        expect(c.millis, 1770000000110);
      } else {
        await expectLater(sync(), throwsA(anything));
        expect(c.calibrated, isFalse);
      }
    }
  });
}
