import 'package:agentdeck/core/client.dart' show ConnectionPhase;
import 'dart:async';
import 'package:agentdeck/core/usage.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'widget_test.dart' show OperableClient;

class UsageClient extends OperableClient {
  final calls = <String>[];
  Completer<AccountUsage> pending = Completer<AccountUsage>();
  @override
  Future<AccountUsage> accountUsage(String agent) {
    calls.add(agent);
    return pending.future;
  }
}

void main() {
  testWidgets(
    'usage loads on tap and exposes cache freshness and unknown resets',
    (tester) async {
      final client = UsageClient();
      addTearDown(client.dispose);
      await tester.pumpWidget(
        AgentDeckApp(
          client: client,
          languages: LanguageController(
            client.vault,
            initial: AppLanguage.simplifiedChinese,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(client.calls, isEmpty);
      expect(
        (tester.getCenter(find.byKey(const ValueKey('account-usage'))).dy -
                tester
                    .getCenter(find.byKey(const ValueKey('session-count')))
                    .dy)
            .abs(),
        lessThan(2),
      );
      expect(
        tester.getSize(find.byKey(const ValueKey('session-meta-row'))).height,
        lessThanOrEqualTo(40),
      );
      await tester.tap(find.byKey(const ValueKey('account-usage')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));
      expect(client.calls, ['codex']);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
      client.pending.complete(
        AccountUsage({
          'source': 'claude-desktop-cache',
          'stale': true,
          'as_of': '2026-10-09T21:20:29Z',
          'windows': [
            {'label': '每周额度', 'used_percent': 96, 'window_minutes': 10080},
          ],
        }),
      );
      await tester.pumpAndSettle();
      expect(find.text('已用 96% · 剩余 4%'), findsOneWidget);
      expect(find.textContaining('非实时'), findsOneWidget);
      expect(find.text('重置时间：暂不可用'), findsOneWidget);
      expect(tester.takeException(), isNull);
      Navigator.of(tester.element(find.text('Codex 用量'))).pop();
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(ChoiceChip, 'Claude'));
      await tester.pumpAndSettle();
      client.pending = Completer<AccountUsage>();
      await tester.tap(find.byKey(const ValueKey('account-usage')));
      await tester.pump();
      client.pending.completeError(Exception('Usage unavailable'));
      await tester.pumpAndSettle();
      expect(client.calls, ['codex', 'claude']);
      expect(find.textContaining('Usage unavailable'), findsOneWidget);
      client.pending = Completer<AccountUsage>();
      await tester.tap(find.byTooltip('刷新用量'));
      await tester.pump();
      client.pending.complete(
        AccountUsage({
          'source': 'claude-cli',
          'stale': false,
          'as_of': '2026-10-10T12:00:00Z',
          'windows': [
            {
              'label': '5 小时额度',
              'used_percent': 0,
              'window_minutes': 300,
              'resets_at': '2026-10-10T17:00:00Z',
            },
          ],
        }),
      );
      await tester.pumpAndSettle();
      expect(find.text('已用 0% · 剩余 100%'), findsOneWidget);
      expect(find.textContaining('非实时'), findsNothing);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets('disconnected workstation cannot request usage', (tester) async {
    final client = UsageClient()
      ..health.transition(ConnectionPhase.reconnecting);
    addTearDown(client.dispose);
    await tester.pumpWidget(
      AgentDeckApp(
        client: client,
        languages: LanguageController(
          client.vault,
          initial: AppLanguage.simplifiedChinese,
        ),
      ),
    );
    await tester.pumpAndSettle();
    final button = tester.widget<TextButton>(
      find.byKey(const ValueKey('account-usage')),
    );
    expect(button.onPressed, isNull);
    expect(client.calls, isEmpty);
  });
}
