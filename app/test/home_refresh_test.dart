import 'dart:async';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/main.dart';
import 'package:agentdeck/language.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'widget_test.dart' show OperableClient;

class RefreshClient extends OperableClient {
  int requests = 0;
  Completer<dynamic> response = Completer<dynamic>();
  @override
  Future<dynamic> request(
    Map<String, dynamic> data, {
    Duration timeout = const Duration(seconds: 70),
  }) {
    expect(data['method'], 'sessions');
    expect(timeout, const Duration(seconds: 12));
    requests++;
    return response.future;
  }

  @override
  Future<void> refreshGoals() async {}
}

Widget home(RefreshClient c) => MaterialApp(
  localizationsDelegates: UiStrings.delegates,
  supportedLocales: UiStrings.supportedLocales,
  locale: AppLanguage.simplifiedChinese.locale,
  home: DeckHome(client: c),
);

void main() {
  test(
    'refresh coalesces concurrent requests and waits for server acknowledgement',
    () async {
      final c = RefreshClient();
      addTearDown(c.dispose);
      bool complete = false;
      final a = c.refreshSessions().then((_) => complete = true);
      final b = c.refreshSessions();
      expect(c.requests, 1);
      expect(complete, isFalse);
      c.response.complete({});
      await Future.wait([a, b]);
      expect(complete, isTrue);
      c.response = Completer<dynamic>();
      final next = c.refreshSessions();
      expect(c.requests, 2);
      c.response.complete({});
      await next;
    },
  );

  for (final empty in [false, true]) {
    testWidgets(
      'pull-to-refresh works for ${empty ? "empty" : "short populated"} home and retains layout',
      (tester) async {
        final c = RefreshClient();
        addTearDown(c.dispose);
        if (empty) c.sessions.clear();
        c.projectLayout = ProjectLayout().withScoped(
          'codex',
          ProjectLayout(
            order: ['/project-b', '/project-a'],
            collapsed: ['/project-a'],
            hidden: ['/hidden'],
          ),
        );
        final before = c.projectLayout.toJson();
        await tester.pumpWidget(home(c));
        await tester.pumpAndSettle();
        await tester.drag(find.byType(Scrollable).last, const Offset(0, 350));
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 500));
        expect(c.requests, 1);
        expect(find.byType(RefreshProgressIndicator), findsOneWidget);
        c.response.complete({});
        await tester.pumpAndSettle();
        expect(find.byType(RefreshProgressIndicator), findsNothing);
        expect(c.projectLayout.toJson(), before);
        if (!empty) expect(find.text('项目 A 会话'), findsNothing);
        expect(tester.takeException(), isNull);
      },
    );
  }

  testWidgets(
    'refresh failure is visible and does not erase the existing list',
    (tester) async {
      final c = RefreshClient();
      addTearDown(c.dispose);
      await tester.pumpWidget(home(c));
      await tester.pumpAndSettle();
      await tester.drag(find.byType(Scrollable).last, const Offset(0, 350));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      c.response.completeError(TimeoutException('fixture timeout'));
      await tester.pumpAndSettle();
      expect(find.text('刷新超时，请检查连接后重试'), findsOneWidget);
      expect(find.text('项目 A 会话'), findsOneWidget);
      expect(c.sessions.length, 2);
    },
  );
}
