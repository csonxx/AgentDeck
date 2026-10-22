import 'dart:async';
import 'package:agentdeck/core/client.dart';
import 'package:flutter_test/flutter_test.dart';

class OrderedAccountClient extends AgentClient {
  final calls = <String>[];
  final responses = <Completer<dynamic>>[];
  @override
  Future<dynamic> request(
    Map<String, dynamic> data, {
    Duration timeout = const Duration(seconds: 70),
  }) {
    calls.add(data['method'] as String);
    final result = Completer<dynamic>();
    responses.add(result);
    return result.future;
  }
}

void main() {
  test('usage, cards and history occupy one account request slot', () async {
    final c = OrderedAccountClient();
    addTearDown(c.dispose);
    final quota = c.accountUsage('codex');
    final cards = c.codexResetState();
    final history = c.codexResetHistory();
    await Future<void>.delayed(Duration.zero);
    expect(c.calls, ['usage']);
    c.responses[0].complete({'windows': []});
    await quota;
    await Future<void>.delayed(Duration.zero);
    expect(c.calls, ['usage', 'usage_resets']);
    c.responses[1].complete({'available_count': 3});
    await cards;
    await Future<void>.delayed(Duration.zero);
    expect(c.calls, ['usage', 'usage_resets', 'usage_reset_history']);
    c.responses[2].complete({'events': []});
    await history;
    expect(c.calls.any((m) => m == 'usage_reset_consume'), isFalse);
  });
  test('a failed read releases the lane without retry or reset', () async {
    final c = OrderedAccountClient();
    addTearDown(c.dispose);
    final cards = c.codexResetState();
    final failure = expectLater(cards, throwsStateError);
    final history = c.codexResetHistory();
    await Future<void>.delayed(Duration.zero);
    c.responses[0].completeError(StateError('fixture unavailable'));
    await failure;
    await Future<void>.delayed(Duration.zero);
    expect(c.calls, ['usage_resets', 'usage_reset_history']);
    c.responses[1].complete({'events': []});
    await history;
    expect(c.calls.length, 2);
  });
}
