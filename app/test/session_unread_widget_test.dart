import 'package:agentdeck/core/client.dart' show ConnectionPhase;
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'widget_test.dart' show OperableClient;

AgentSession chat(int completion, {String status = 'completed'}) =>
    AgentSession(
      id: 'a',
      agent: 'codex',
      title: '项目 A 会话',
      source: 'stream',
      status: status,
      project: '/project-a',
      managed: true,
      completion: completion,
      completedAt: DateTime.now()
          .toUtc()
          .add(const Duration(minutes: 1))
          .toIso8601String(),
    );
void main() {
  testWidgets(
    'unread completion dot appears in projects and clears on opening',
    (tester) async {
      final c = OperableClient()
        ..health.transition(ConnectionPhase.reconnecting);
      addTearDown(c.dispose);
      c.updateSessions([chat(1)]);
      await tester.pumpWidget(
        MaterialApp(
          navigatorObservers: [deckRoutes],
          home: DeckHome(client: c),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('unread-a')), findsNothing);
      c.updateSessions([chat(2)]);
      c.notifyListeners();
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('unread-a')), findsOneWidget);
      expect(
        find.byKey(const ValueKey('unread-project-/project-a')),
        findsOneWidget,
      );
      await tester.tap(find.byKey(const ValueKey('session-a')));
      await tester.pumpAndSettle();
      expect(c.isUnread(chat(2)), isFalse);
      c.updateSessions([chat(3)]);
      c.notifyListeners();
      await tester.pumpAndSettle();
      expect(c.isUnread(chat(3)), isFalse);
      await tester.pageBack();
      await tester.pumpAndSettle();
      c.updateSessions([chat(4)]);
      c.notifyListeners();
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('unread-a')), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
  testWidgets('running completion has a spinner rather than unread dot', (
    tester,
  ) async {
    final c = OperableClient();
    addTearDown(c.dispose);
    c.updateSessions([chat(1)]);
    c.updateSessions([chat(2, status: 'running')]);
    await tester.pumpWidget(MaterialApp(home: DeckHome(client: c)));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));
    expect(find.byType(RunningIndicator), findsOneWidget);
    expect(find.byKey(const ValueKey('unread-a')), findsNothing);
    c.health.transition(ConnectionPhase.reconnecting);
    c.notifyListeners();
    await tester.pump(const Duration(milliseconds: 100));
    expect(find.byType(RunningIndicator), findsNothing);
    expect(find.byKey(const ValueKey('unread-a')), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
