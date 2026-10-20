import 'dart:io';
import 'dart:async';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:flutter_test/flutter_test.dart';

class HealthVault extends DeviceVault {
  bool keysFail = false, cacheReadFail = false, cacheWriteFail = false;
  bool preferencesFail = false;
  Map<String, dynamic>? savedDevice = {'device': 'fixture-device'};
  @override
  Future<Map<String, dynamic>?> loadDevice() async {
    if (keysFail) throw const FileSystemException('fixture');
    return savedDevice;
  }

  @override
  Future<Map<String, dynamic>?> loadDeviceName() async {
    if (preferencesFail) throw const FileSystemException('fixture');
    return {'name': 'Fixture phone'};
  }

  @override
  Future<Map<String, dynamic>> readCache() async {
    if (cacheReadFail) throw const FileSystemException('fixture');
    return {};
  }

  @override
  Future<void> writeCache(Map<String, dynamic> value) async {
    if (cacheWriteFail) throw const FileSystemException('fixture');
  }
}

class HealthClient extends AgentClient {
  int connects = 0;
  HealthClient(HealthVault vault) : super(vault: vault);
  @override
  Future<void> connect() async {
    connects++;
    health.transition(ConnectionPhase.synchronizing);
    notifyListeners();
  }
}

class FailedRefreshClient extends AgentClient {
  @override
  Future<dynamic> request(
    Map<String, dynamic> data, {
    Duration timeout = const Duration(seconds: 70),
  }) async => throw const HttpException('fixture refresh failure');
}

class DelayedHistoryClient extends AgentClient {
  final calls = <Completer<dynamic>>[];
  @override
  Future<dynamic> request(
    Map<String, dynamic> data, {
    Duration timeout = const Duration(seconds: 70),
  }) {
    final call = Completer<dynamic>();
    calls.add(call);
    return call.future;
  }
}

void main() {
  Map<String, dynamic> message(int seq, String text) => {
    'id': 'message-$seq',
    'item': 'answer',
    'seq': seq,
    'first': 1,
    'kind': 'message',
    'role': 'assistant',
    'text': text,
    'mode': 'snapshot',
  };
  Map<String, dynamic> page(int cursor, String text) => {
    'cursor': cursor,
    'before': 1,
    'events': [message(cursor, text)],
    'has_more': false,
    'has_newer': false,
  };

  test('live response received during refresh is never rolled back', () async {
    final c = DelayedHistoryClient()..selected = 'fixture';
    addTearDown(c.dispose);
    final t = Transcript()..add(message(1, 'Before'));
    c.transcripts['fixture'] = t;
    final refresh = c.reload('fixture');
    t.add(message(3, 'Newest live response'));
    c.calls.single.complete(page(2, 'Stale snapshot'));
    await refresh;
    expect(t.blocks.single.text, 'Newest live response');
    expect(t.cursor, 3);
    expect(t.detached, isFalse);
  });

  test('old paging reply cannot mutate a refreshed history window', () async {
    final c = DelayedHistoryClient()..selected = 'fixture';
    addTearDown(c.dispose);
    c.health.transition(ConnectionPhase.live);
    final t = Transcript()..add(message(1, 'Before'));
    t.hasMore = true;
    c.transcripts['fixture'] = t;
    final older = c.loadOlder('fixture');
    final refresh = c.reload('fixture');
    c.calls[1].complete(page(4, 'Refreshed history'));
    await refresh;
    c.calls[0].complete(page(2, 'Obsolete paging result'));
    await older;
    expect(t.blocks.single.text, 'Refreshed history');
    expect(t.loadingOlder, isFalse);
  });
  for (final older in [true, false]) {
    test(
      'direction reversal discards delayed ${older ? "older" : "newer"} page',
      () async {
        final c = DelayedHistoryClient()..selected = 'fixture';
        addTearDown(c.dispose);
        c.health.transition(ConnectionPhase.live);
        final t = Transcript()..add(message(1, 'Visible message'));
        t.hasMore = true;
        t.detached = true;
        c.transcripts['fixture'] = t;
        final pending = older ? c.loadOlder('fixture') : c.loadNewer('fixture');
        t.cancelPaging();
        c.calls.single.complete(page(2, 'Obsolete opposite page'));
        await pending;
        expect(t.blocks.single.text, 'Visible message');
        expect(t.hasMore, isTrue);
        expect(t.detached, isTrue);
        expect(t.loadingOlder || t.loadingNewer, isFalse);
      },
    );
  }
  test(
    'transport, sync, connection failure and local storage recover independently',
    () {
      final h = ClientHealth();
      h.report(LocalIssueKind.cacheWrite, 'cache failure');
      h.transition(ConnectionPhase.connecting);
      expect(h.connected, isFalse);
      h.transition(ConnectionPhase.synchronizing);
      expect(h.connected, isTrue);
      expect(h.live, isFalse);
      h.synchronized();
      expect(h.live, isTrue);
      h.transition(ConnectionPhase.reconnecting, failure: 'network failure');
      h.transition(ConnectionPhase.connecting);
      expect(h.connectionFailure, 'network failure');
      h.transition(ConnectionPhase.synchronizing);
      expect(h.connectionFailure, isEmpty);
      expect(h.issues.single.kind, LocalIssueKind.cacheWrite);
      h.synchronized();
      expect(h.live, isTrue);
      h.resolve(LocalIssueKind.cacheWrite);
      expect(h.issues, isEmpty);
    },
  );

  test(
    'dismissal survives repeated failures but resets after real recovery',
    () {
      final h = ClientHealth();
      h.report(LocalIssueKind.projectLayout, 'layout failure');
      h.dismiss(LocalIssueKind.projectLayout);
      h.report(LocalIssueKind.projectLayout, 'layout failure');
      expect(h.visibleIssues, isEmpty);
      expect(h.issues, hasLength(1));
      h.resolve(LocalIssueKind.projectLayout);
      h.report(LocalIssueKind.projectLayout, 'layout failure');
      expect(h.visibleIssues, hasLength(1));
      h.report(LocalIssueKind.deviceStorage, 'key failure');
      h.dismiss(LocalIssueKind.deviceStorage);
      expect(h.blockingIssue!.dismissed, isFalse);
      h.transition(ConnectionPhase.revoked, failure: 'revoked');
      h.synchronized();
      expect(h.needsPairing, isTrue);
      expect(h.connected, isFalse);
    },
  );

  test(
    'unreadable cache and preferences do not block valid device connection',
    () async {
      final v = HealthVault()
        ..cacheReadFail = true
        ..preferencesFail = true;
      final c = HealthClient(v);
      addTearDown(c.dispose);
      await c.initialize();
      expect(c.connects, 1);
      expect(c.health.phase, ConnectionPhase.synchronizing);
      expect(c.error, isEmpty);
      expect(c.health.blockingIssue, isNull);
      expect(
        c.health.issues.map((i) => i.kind),
        containsAll([LocalIssueKind.cacheRead, LocalIssueKind.deviceName]),
      );
      c.health.synchronized();
      await c.retryIssue(LocalIssueKind.cacheRead);
      expect(c.live, isTrue);
      expect(
        c.health.issues.any((i) => i.kind == LocalIssueKind.cacheRead),
        isFalse,
      );
    },
  );

  test(
    'device key read failure is recoverable without deleting pairing',
    () async {
      final v = HealthVault()..keysFail = true;
      final c = HealthClient(v);
      addTearDown(c.dispose);
      await c.initialize();
      expect(c.connects, 0);
      expect(c.health.blockingIssue?.kind, LocalIssueKind.deviceStorage);
      v.keysFail = false;
      await c.retryIssue(LocalIssueKind.deviceStorage);
      expect(c.connects, 1);
      expect(c.device?['device'], 'fixture-device');
      expect(c.health.blockingIssue, isNull);
      expect(v.savedDevice, isNotNull);
    },
  );

  test(
    'cache write failure leaves live connection usable and clears on successful write',
    () async {
      final v = HealthVault()..cacheWriteFail = true;
      final c = HealthClient(v)..device = v.savedDevice;
      addTearDown(c.dispose);
      c.health.transition(ConnectionPhase.live);
      await c.retryIssue(LocalIssueKind.cacheWrite);
      expect(c.live, isTrue);
      expect(c.error, isEmpty);
      expect(c.health.issues.single.kind, LocalIssueKind.cacheWrite);
      c.dismissIssue(LocalIssueKind.cacheWrite);
      await c.retryIssue(LocalIssueKind.cacheWrite);
      expect(c.health.visibleIssues, isEmpty);
      v.cacheWriteFail = false;
      await c.retryIssue(LocalIssueKind.cacheWrite);
      expect(c.health.issues, isEmpty);
    },
  );

  test(
    'offline send is definitively not sent, without global error or resend',
    () async {
      final c = AgentClient();
      addTearDown(c.dispose);
      await expectLater(
        c.sendPrompt('fixture', 'test'),
        throwsA(
          isA<AgentRequestException>().having(
            (e) => e.notSent,
            'not sent',
            isTrue,
          ),
        ),
      );
      expect(c.error, isEmpty);
      expect(c.health.issues, isEmpty);
    },
  );

  test(
    'failed refresh preserves transcript identity and older reading position',
    () async {
      final c = FailedRefreshClient()..selected = 'fixture';
      addTearDown(c.dispose);
      final t = Transcript()
        ..add({
          'id': 'old',
          'seq': 1,
          'kind': 'message',
          'role': 'assistant',
          'text': 'Retained record',
        });
      t.readingHistory = true;
      t.detached = true;
      c.transcripts['fixture'] = t;
      await expectLater(c.reload('fixture'), throwsA(isA<HttpException>()));
      expect(c.transcripts['fixture'], same(t));
      expect(t.blocks.single.text, 'Retained record');
      expect(t.readingHistory, isTrue);
      expect(t.detached, isTrue);
      expect(c.error, isEmpty);
    },
  );
}
