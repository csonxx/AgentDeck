import 'package:agentdeck/core/goals.dart';
import 'package:agentdeck/goal_panel.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'widget_test.dart' show OperableClient;

class GoalsClient extends OperableClient {
  final changes = <String>[];
  @override
  Future<void> changeGoal(AgentGoal goal, String action) async {
    changes.add(action);
    if (action == 'delete') {
      goals = [];
    } else {
      goals = [
        AgentGoal({
          'session': goal.session,
          'agent': goal.agent,
          'objective': goal.objective,
          'status': action == 'pause' ? 'paused' : 'active',
          'version': 'v2',
          'actions': ['pause', 'resume', 'delete'],
        }),
      ];
    }
    notifyListeners();
  }
}

void main() {
  test(
    'current goals retain actionable states and exclude finished records',
    () {
      for (final status in [
        'active',
        'paused',
        'blocked',
        'usageLimited',
        'budgetLimited',
        'complete',
        'failed',
        '',
        'unknown',
      ]) {
        final goal = AgentGoal({
          'session': 'a',
          'agent': 'codex',
          'status': status,
        });
        expect(
          goal.isCurrent,
          [
            'active',
            'paused',
            'blocked',
            'usageLimited',
            'budgetLimited',
          ].contains(status),
          reason: status,
        );
      }
    },
  );

  testWidgets('finished goals disappear when native state updates', (
    tester,
  ) async {
    final c = GoalsClient();
    addTearDown(c.dispose);
    AgentGoal goal(String status) => AgentGoal({
      'session': 'a',
      'agent': 'codex',
      'objective': 'Synthetic current goal',
      'status': status,
      'actions': ['pause', 'resume', 'delete'],
    });
    c.goals = [goal('complete'), goal('failed')];
    await tester.pumpWidget(
      AgentDeckApp(
        client: c,
        languages: LanguageController(
          c.vault,
          initial: AppLanguage.simplifiedChinese,
        ),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));
    expect(find.byKey(const ValueKey('agent-goals')), findsNothing);
    expect(find.text('目标已完成'), findsNothing);

    c.goals = [goal('active')];
    c.notifyListeners();
    await tester.pump();
    expect(find.text('目标 · 1'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('agent-goals')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.byType(GoalPanel), findsOneWidget);

    c.goals = [goal('complete')];
    c.notifyListeners();
    await tester.pump();
    expect(find.byType(GoalPanel), findsNothing);
    expect(find.text('Synthetic current goal'), findsNothing);
    expect(find.byKey(const ValueKey('agent-goals')), findsNothing);
    expect(
      c.goals,
      hasLength(1),
    ); // Presentation does not erase native history.
  });

  testWidgets(
    'native goal state, pause and confirmed deletion stay on original session',
    (tester) async {
      final c = GoalsClient();
      c.goals = [
        AgentGoal({
          'session': 'a',
          'agent': 'codex',
          'objective': 'Synthetic goal for test',
          'status': 'active',
          'version': 'v1',
          'actions': ['pause', 'resume', 'delete'],
        }),
      ];
      addTearDown(c.dispose);
      await tester.pumpWidget(
        AgentDeckApp(
          client: c,
          languages: LanguageController(
            c.vault,
            initial: AppLanguage.simplifiedChinese,
          ),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));
      expect(find.text('目标执行中'), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('agent-goals')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.byType(GoalPanel), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('goal-pause-a')));
      await tester.pump();
      expect(c.changes, ['pause']);
      expect(find.byKey(const ValueKey('goal-resume-a')), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('goal-delete-a')));
      await tester.pumpAndSettle();
      expect(c.changes, ['pause']);
      await tester.tap(find.text('取消'));
      await tester.pumpAndSettle();
      expect(c.goals, hasLength(1));
      await tester.tap(find.byKey(const ValueKey('goal-delete-a')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('删除'));
      await tester.pumpAndSettle();
      expect(c.changes, ['pause', 'delete']);
      expect(find.byType(GoalPanel), findsNothing);
      expect(c.sessions.where((s) => s.id == 'a'), hasLength(1));
    },
  );
}
