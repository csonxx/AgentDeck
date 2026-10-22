import 'package:agentdeck/core/session_open_timing.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'open timing waits for actual history revision to paint and reports once',
    () {
      final t = SessionOpenTiming('session', 'request', 100);
      expect(t.presented(120, 0), isNull);
      t.sent = 150;
      t.received(
        arrival: 300,
        decodeStart: 350,
        decodeEnd: 450,
        applyEnd: 470,
        bytes: 26000,
        revision: 12,
      );
      expect(t.presented(500, 11), isNull);
      final values = t.presented(550, 12)!;
      expect(values, {
        'send_ms': 50,
        'round_trip_ms': 150,
        'receive_queue_ms': 50,
        'decode_ms': 100,
        'apply_ms': 20,
        'paint_ms': 80,
        'total_ms': 450,
        'wire_bytes': 26000,
      });
      expect(t.presented(600, 13), isNull);
    },
  );
}
