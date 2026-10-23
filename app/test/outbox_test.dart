import 'dart:async';
import 'dart:io';
import 'dart:convert';
import 'dart:typed_data';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/input.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'outbox_fixtures.dart';

Future<void> until(bool Function() done) async {
  for (var i = 0; i < 300; i++) {
    if (done()) return;
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  fail('outbox did not reach expected state');
}

class SendDiagnosticClient extends OutboxTestClient {
  final diagnostics = <Map<String, dynamic>>[];
  SendDiagnosticClient() : super(MemoryOutgoingVault());
  @override
  Future<void> send(Map<String, dynamic> data) async {
    diagnostics.add(data);
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'running timing requires both this input and native running status',
    () async {
      final c = SendDiagnosticClient();
      addTearDown(c.dispose);
      final t = await c.outbox.enqueue(
        c.sessions.first,
        'Synthetic diagnostic',
        const TurnOptions(),
        [],
        'send',
      );
      await until(() => t.state == 'accepted');
      expect(c.diagnostics.length, 1);
      expect(c.diagnostics.single['send_timing']['running_ms'], isNull);
      c.sessions = [
        AgentSession(
          id: 'a',
          agent: 'opencode',
          title: 'Fixture',
          source: 'opencode-api',
          status: 'running',
          project: '/fixture',
          managed: true,
        ),
      ];
      c.notifyListeners();
      expect(
        c.diagnostics.length,
        1,
      ); // an old turn is not proof of new execution
      c.transcripts['a']!.add({
        'id': 'input',
        'seq': 1,
        'first': 1,
        'item': '${t.id}:0',
        'kind': 'message',
        'role': 'user',
        'mode': 'snapshot',
        'text': t.text,
      });
      c.notifyListeners();
      await until(() => c.diagnostics.length == 2);
      final v = c.diagnostics.last['send_timing'] as Map;
      expect(v['running_ms'], greaterThanOrEqualTo(v['total_ms']));
      expect(c.diagnostics.last.containsKey('text'), false);
      c.notifyListeners();
      expect(c.diagnostics.length, 2);
      expect(c.calls.length, 1); // diagnostics never resend the native input
    },
  );
  test(
    'atomic desktop send skips association round trip and retains original ID',
    () async {
      for (final supported in [false, true]) {
        final c = OutboxTestClient(MemoryOutgoingVault())
          ..atomicCodexSend = supported
          ..codexDesktop = true;
        addTearDown(c.dispose);
        c.sessions = [
          AgentSession(
            id: 'a',
            agent: 'codex',
            title: 'Original',
            source: 'log',
            status: 'completed',
            project: '/fixture',
            managed: false,
          ),
        ];
        final t = await c.outbox.enqueue(
          c.sessions.first,
          'Synthetic input',
          const TurnOptions(),
          [],
          'send',
        );
        await until(() => t.state == 'accepted');
        expect(c.preparations, supported ? 0 : 1);
        expect(c.calls.length, 1);
        expect(jsonDecode(c.calls.single['payload'])['session'], 'a');
        expect(c.executed.length, 1);
      }
    },
  );
  test(
    'ciphertext persists message and attachment across a new vault instance',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final dir = await Directory.systemTemp.createTemp('ad-outbox-');
      addTearDown(() => dir.delete(recursive: true));
      final v = DeviceVault(directory: dir);
      final id = 'a' * 32;
      await Future.wait([
        v.writeCache({'cache': 'cache secret'}),
        v.saveOutgoing(id, {'text': 'outbox secret', 'id': id}),
        v.saveOutgoingFiles(id, [
          {
            'name': 'secret.png',
            'bytes': base64Encode([1, 2, 3]),
          },
        ]),
      ]);
      await for (final f in dir.list(recursive: true)) {
        if (f is File) {
          final text = await f.readAsString();
          expect(text, isNot(contains('outbox secret')));
          expect(text, isNot(contains('secret.png')));
          expect(text, isNot(contains('cache secret')));
        }
      }
      final restored = DeviceVault(directory: dir);
      expect((await restored.loadOutgoing()).single['text'], 'outbox secret');
      expect((await restored.loadOutgoingFiles(id)).single['bytes'], 'AQID');
      expect((await restored.readCache())['cache'], 'cache secret');
      await restored.clear();
      expect(await Directory('${dir.path}/outbox').exists(), false);
    },
  );
  test('storage failure does not start any upload or agent request', () async {
    final v = MemoryOutgoingVault()..failWrite = true;
    final c = OutboxTestClient(v);
    addTearDown(c.dispose);
    await expectLater(
      c.outbox.enqueue(
        c.sessions.first,
        'keep',
        const TurnOptions(),
        [],
        'send',
      ),
      throwsA(isA<FileSystemException>()),
    );
    expect(c.calls, isEmpty);
    expect(c.outbox.tasks, isEmpty);
  });
  test(
    'network recovery automatically replays one frozen command, executes once',
    () async {
      final v = MemoryOutgoingVault();
      final c = OutboxTestClient(v)..loseAckOnce = true;
      addTearDown(c.dispose);
      final t = await c.outbox.enqueue(
        c.sessions.first,
        'once',
        const TurnOptions(model: 'p/m', effort: 'high'),
        [],
        'send',
      );
      await until(() => t.state == 'retrying');
      final id = t.commandID;
      c.online(false);
      t.retryAfter = 0;
      c.online(true);
      await until(() => t.state == 'accepted');
      expect(c.calls.length, 2);
      expect(c.calls.map((v) => v['id']).toSet(), {id});
      expect(c.executed.length, 1);
      expect(c.calls[0]['payload'], c.calls[1]['payload']);
    },
  );
  test(
    'process recreation recovers a sent command with lost ACK without duplication',
    () async {
      final v = MemoryOutgoingVault(), executed = <String, String>{};
      final first = OutboxTestClient(v, executed: executed)
        ..ack = Completer<void>();
      final t = await first.outbox.enqueue(
        first.sessions.first,
        'recover',
        const TurnOptions(),
        [],
        'send',
      );
      await until(() => first.calls.isNotEmpty);
      final id = t.commandID;
      first.dispose();
      final second = OutboxTestClient(v, executed: executed);
      addTearDown(second.dispose);
      await second.outbox.restore();
      await until(() => second.outbox.tasks.single.state == 'accepted');
      expect(second.calls.single['id'], id);
      expect(executed.length, 1);
      first.ack!.complete();
    },
  );
  test(
    'attachments and options survive process recreation before upload',
    () async {
      final v = MemoryOutgoingVault();
      final first = OutboxTestClient(v)..online(false);
      final t = await first.outbox.enqueue(
        first.sessions.first,
        'with image',
        const TurnOptions(effort: 'low'),
        [
          DraftFile('x.png', 'image/png', Uint8List.fromList([1, 2, 3])),
        ],
        'send',
      );
      expect(v.blobs.containsKey(t.id), true);
      first.dispose();
      final second = OutboxTestClient(v);
      addTearDown(second.dispose);
      await second.outbox.restore();
      await until(() => second.outbox.tasks.single.state == 'accepted');
      expect(second.uploads, 1);
      expect(second.calls.single['payload'], contains('uploaded-x.png'));
      expect(second.calls.single['payload'], contains('low'));
    },
  );
  test(
    'definite rejection is retained and explicit retry uses a new safe ID',
    () async {
      final v = MemoryOutgoingVault();
      final c = OutboxTestClient(v)
        ..failure = AgentRequestException(
          'fixture rejection',
          notSent: true,
          remote: true,
        );
      addTearDown(c.dispose);
      final t = await c.outbox.enqueue(
        c.sessions.first,
        'retained',
        const TurnOptions(),
        [],
        'send',
      );
      await until(() => t.state == 'failed');
      expect(v.outgoing[t.id]!['text'], 'retained');
      final old = t.commandID;
      c.failure = null;
      await c.outbox.retry(t);
      await until(() => t.state == 'accepted');
      expect(t.commandID, isNot(old));
      expect(c.executed.length, 1);
    },
  );
  test(
    'unknown server outcome blocks a following message and never changes ID',
    () async {
      final v = MemoryOutgoingVault();
      final c = OutboxTestClient(v)
        ..failure = AgentRequestException('unknown', remote: true);
      addTearDown(c.dispose);
      final t = await c.outbox.enqueue(
        c.sessions.first,
        'first',
        const TurnOptions(),
        [],
        'send',
      );
      await until(() => t.state == 'uncertain');
      await c.outbox.enqueue(
        c.sessions.first,
        'second',
        const TurnOptions(),
        [],
        'send',
      );
      expect(c.calls.length, 1);
      final old = t.commandID;
      c.failure = null;
      await c.outbox.retry(t);
      await until(() => c.calls.length == 3);
      expect(t.commandID, old);
      expect(c.executed.length, 2);
    },
  );
  test('pairing change cannot submit an old workstation message', () async {
    final v = MemoryOutgoingVault();
    final c = OutboxTestClient(v)..online(false);
    await c.outbox.enqueue(
      c.sessions.first,
      'old host',
      const TurnOptions(),
      [],
      'send',
    );
    c.dispose();
    final other = OutboxTestClient(v);
    addTearDown(other.dispose);
    other.device!['device'] = 'different-pair';
    await other.outbox.restore();
    await Future<void>.delayed(Duration.zero);
    expect(other.outbox.tasks, isEmpty);
    expect(other.calls, isEmpty);
  });
  test(
    'backgrounding retains connection only until workstation acknowledgement',
    () async {
      final c = OutboxTestClient(MemoryOutgoingVault())
        ..ack = Completer<void>();
      addTearDown(c.dispose);
      final task = await c.outbox.enqueue(
        c.sessions.first,
        'background',
        const TurnOptions(),
        [],
        'send',
      );
      await until(() => c.calls.isNotEmpty);
      c.suspend();
      expect(c.connected, true);
      c.ack!.complete();
      await until(() => task.state == 'accepted' && !c.connected);
      expect(c.health.phase, ConnectionPhase.suspended);
    },
  );
  test(
    'system timeout preserves pending data and next foreground resumes automatically',
    () async {
      final v = MemoryOutgoingVault();
      final c = OutboxTestClient(v)..online(false);
      addTearDown(c.dispose);
      final task = await c.outbox.enqueue(
        c.sessions.first,
        'recover after timeout',
        const TurnOptions(),
        [],
        'send',
      );
      await c.outbox.pauseForSystemTimeout();
      c.online(true);
      await Future<void>.delayed(Duration.zero);
      expect(c.calls, isEmpty);
      expect(v.outgoing[task.id]!['text'], 'recover after timeout');
      c.outbox.foreground();
      await until(() => task.state == 'accepted');
      expect(c.executed.length, 1);
    },
  );
  test(
    'pair reset during an encrypted commit never starts the old task',
    () async {
      final v = MemoryOutgoingVault()..commitGate = Completer<void>();
      final c = OutboxTestClient(v);
      addTearDown(c.dispose);
      final send = c.outbox.enqueue(
        c.sessions.first,
        'old pairing',
        const TurnOptions(),
        [],
        'send',
      );
      final rejected = expectLater(send, throwsA(isA<FileSystemException>()));
      await Future<void>.delayed(const Duration(milliseconds: 20));
      final resetting = c.outbox.reset();
      v.commitGate!.complete();
      await resetting;
      await rejected;
      expect(c.outbox.tasks, isEmpty);
      expect(c.calls, isEmpty);
    },
  );
}
