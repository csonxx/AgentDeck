import 'dart:convert';
import 'crypto.dart';

/// Protocol time is authenticated, then advanced by a monotonic stopwatch.
/// A connection keeps this clock until it closes; reconnect replaces the anchor.
class ProtocolClock {
  final int Function() _wall;
  final int Function() _monotonic;
  int? _base;
  int _anchor = 0;
  ProtocolClock({int Function()? wallMillis, int Function()? monotonicMillis})
    : _wall = wallMillis ?? (() => DateTime.now().millisecondsSinceEpoch),
      _monotonic = monotonicMillis ?? _stopwatch();
  static int Function() _stopwatch() {
    final watch = Stopwatch()..start();
    return () => watch.elapsedMilliseconds;
  }

  int get stamp => _monotonic();
  bool get calibrated => _base != null;
  int get millis => _base == null ? _wall() : _base! + stamp - _anchor;
  int get unix => millis ~/ 1000;
  void calibrate(int serverMillis, int roundTripMillis, int receivedStamp) {
    if (serverMillis <= 0 || roundTripMillis < 0 || roundTripMillis > 10000) {
      throw const FormatException('时间校准失败，请检查网络后重试');
    }
    _base = serverMillis + roundTripMillis ~/ 2;
    _anchor = receivedStamp;
  }
}

Future<void> synchronizeClock(
  ProtocolClock clock,
  List<int> key,
  String kind,
  String id,
  Future<Map<String, dynamic>> Function(Map<String, dynamic>) exchange,
) async {
  final scope = '$kind/$id';
  final challenge = base64Encode(WireCrypto.random(24));
  final box = await WireCrypto.seal(key, 'clock-request/$scope', 0, 0, {
    'challenge': challenge,
  });
  final start = clock.stamp;
  final response = await exchange({'kind': kind, 'id': id, 'box': box});
  final received = clock.stamp;
  if (response['epoch'] != 0 || response['seq'] != 0) {
    throw const FormatException('时间响应格式错误');
  }
  final sample = await WireCrypto.open(key, 'clock-reply/$scope', response);
  if (sample['challenge'] != challenge ||
      sample['version'] != 1 ||
      sample['server_time_ms'] is! int) {
    throw const FormatException('时间响应校验失败');
  }
  clock.calibrate(sample['server_time_ms'] as int, received - start, received);
}
