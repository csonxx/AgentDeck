import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/core/input.dart';
import 'package:agentdeck/input_composer.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'outbox_fixtures.dart';
import 'outbox_page_test.dart' show mount;
import 'outbox_test.dart' show until;

const safetyMessage = '平台已因安全检查结束此会话，请在电脑端查看；原会话不能继续发送';
AgentSession withIssue(
  AgentSession s, {
  String status = 'failed',
  SessionIssue? issue,
}) => AgentSession(
  id: s.id,
  agent: s.agent,
  title: s.title,
  project: s.project,
  source: s.source,
  managed: s.managed,
  status: status,
  issue: issue,
);
void main() {
  testWidgets(
    'native safety stop is visible and cannot submit while retaining editable text',
    (tester) async {
      final c = OutboxTestClient(MemoryOutgoingVault());
      addTearDown(c.dispose);
      c.sessions = [
        withIssue(
          c.sessions.first,
          issue: const SessionIssue(
            code: 'safety_ended',
            message: safetyMessage,
            blocksInput: true,
          ),
        ),
      ];
      await mount(tester, c);
      expect(find.text(safetyMessage), findsOneWidget);
      expect(find.text('需要处理'), findsOneWidget);
      expect(find.text('就绪'), findsNothing);
      await tester.enterText(find.byType(TextField), 'retain this draft');
      await tester.pump();
      final composer = tester.widget<InputComposer>(find.byType(InputComposer));
      expect(composer.canSend, false);
      expect(c.calls, isEmpty);
      await expectLater(
        c.outbox.enqueue(
          c.sessions.first,
          'blocked',
          const TurnOptions(),
          [],
          'send',
        ),
        throwsA(isA<Exception>()),
      );
      expect(c.outbox.tasks, isEmpty);
      c.sessions = [withIssue(c.sessions.first, status: 'completed')];
      c.notifyListeners();
      await tester.pump();
      expect(find.text(safetyMessage), findsNothing);
      expect(
        tester.widget<InputComposer>(find.byType(InputComposer)).canSend,
        true,
      );
      expect(find.text('retain this draft'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets('failed and unknown native states never display ready', (
    tester,
  ) async {
    final c = OutboxTestClient(MemoryOutgoingVault());
    addTearDown(c.dispose);
    c.sessions = [withIssue(c.sessions.first)];
    await mount(tester, c);
    expect(find.text('执行失败'), findsOneWidget);
    expect(find.text('就绪'), findsNothing);
    c.sessions = [withIssue(c.sessions.first, status: 'unknown')];
    c.notifyListeners();
    await tester.pump();
    expect(find.text('状态未知'), findsOneWidget);
    expect(find.text('就绪'), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });
  test('old protocol remains readable, new structured issues round trip', () {
    final base = {
      'id': 's',
      'agent': 'codex',
      'title': 'Chat',
      'source': 'desktop',
      'status': 'failed',
    };
    expect(AgentSession.fromJson(base).issue, isNull);
    final issue = SessionIssue.fromJson({
      'code': 'safety_ended',
      'message': safetyMessage,
      'blocks_input': true,
    });
    expect(
      AgentSession.fromJson({
        ...base,
        'issue': issue.toJson(),
      }).issue!.blocksInput,
      true,
    );
  });
  test(
    'offline pending task stops before submission when a terminal state arrives',
    () async {
      final c = OutboxTestClient(MemoryOutgoingVault());
      addTearDown(c.dispose);
      c.online(false);
      final task = await c.outbox.enqueue(
        c.sessions.first,
        'pending',
        const TurnOptions(),
        [],
        'send',
      );
      c.sessions = [
        withIssue(
          c.sessions.first,
          issue: const SessionIssue(
            code: 'safety_ended',
            message: safetyMessage,
            blocksInput: true,
          ),
        ),
      ];
      c.online(true);
      await until(() => task.state == 'failed');
      expect(c.calls, isEmpty);
      expect(task.definitelyNotSent, true);
      expect(task.error, safetyMessage);
    },
  );
  test(
    'lost ACK check keeps its original ID even after that turn ends the chat',
    () async {
      final c = OutboxTestClient(MemoryOutgoingVault())..loseAckOnce = true;
      addTearDown(c.dispose);
      final task = await c.outbox.enqueue(
        c.sessions.first,
        'once',
        const TurnOptions(),
        [],
        'send',
      );
      await until(() => task.state == 'retrying');
      final originalID = task.commandID;
      c.sessions = [
        withIssue(
          c.sessions.first,
          issue: const SessionIssue(
            code: 'safety_ended',
            message: safetyMessage,
            blocksInput: true,
          ),
        ),
      ];
      await c.outbox.retry(task);
      await until(() => task.state == 'accepted');
      expect(c.calls.map((m) => m['id']).toSet(), {originalID});
      expect(c.executed.length, 1);
    },
  );
}
