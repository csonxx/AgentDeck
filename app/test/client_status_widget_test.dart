import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/input_composer.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/client_status.dart';
import 'package:agentdeck/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'widget_test.dart' show OperableClient;

class TransportStatusClient extends OperableClient {
  String transport = 'gateway';
  @override
  String get connectionMethod => transport;
}

void main() {
  testWidgets(
    'status displays the confirmed transport and explains relay fallback',
    (t) async {
      final c = TransportStatusClient();
      addTearDown(c.dispose);
      c.directNote = '直连未成功，继续使用网关中继';
      for (final entry in {
        'gateway': 'Gateway relay',
        'webrtc': 'Peer-to-peer',
        'https': 'Direct HTTPS',
      }.entries) {
        c.transport = entry.key;
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(body: ClientStatusSheet(client: c)),
          ),
        );
        await t.pumpAndSettle();
        expect(find.byKey(const ValueKey('connection-method')), findsOneWidget);
        expect(find.text(entry.value), findsOneWidget);
        expect(t.takeException(), isNull);
      }
    },
  );
  testWidgets(
    'status details remain usable on a narrow screen with large Russian text',
    (t) async {
      t.view.physicalSize = const Size(320, 640);
      t.view.devicePixelRatio = 1;
      t.platformDispatcher.textScaleFactorTestValue = 1.8;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.platformDispatcher.clearTextScaleFactorTestValue);
      final c = OperableClient();
      final languages = LanguageController(
        c.vault,
        initial: AppLanguage.russian,
      );
      addTearDown(c.dispose);
      addTearDown(languages.dispose);
      c.health.report(LocalIssueKind.projectLayout, '项目布局读取失败');
      c.device = {'url': 'https://example.com/agentdeck'};
      await t.pumpWidget(
        LanguageScope(
          controller: languages,
          child: MaterialApp(
            locale: languages.language.locale,
            supportedLocales: UiStrings.supportedLocales,
            localizationsDelegates: UiStrings.delegates,
            home: Scaffold(body: ClientStatusSheet(client: c)),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('https://example.com/agentdeck'), findsOneWidget);
      expect(find.byKey(const ValueKey('retry-projectLayout')), findsOneWidget);
      expect(
        find.byKey(const ValueKey('dismiss-projectLayout')),
        findsOneWidget,
      );
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'local warning stays compact, can be dismissed, and recovers independently',
    (t) async {
      final c = OperableClient();
      addTearDown(c.dispose);
      c.health.report(LocalIssueKind.projectLayout, '项目布局读取失败');
      await t.pumpWidget(AgentDeckApp(client: c));
      await t.pumpAndSettle();
      expect(find.text('Live connection'), findsOneWidget);
      expect(
        find.byKey(const ValueKey('client-local-warning')),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('client-blocking-notice')),
        findsNothing,
      );
      await t.tap(find.byKey(const ValueKey('client-status')));
      await t.pumpAndSettle();
      expect(find.byKey(const ValueKey('retry-projectLayout')), findsOneWidget);
      await t.tap(find.byKey(const ValueKey('dismiss-projectLayout')));
      await t.pumpAndSettle();
      c.health.report(LocalIssueKind.projectLayout, '项目布局读取失败');
      c.notifyListeners();
      await t.pumpAndSettle();
      expect(find.byKey(const ValueKey('client-local-warning')), findsNothing);
      expect(c.health.issues, hasLength(1));
      await t.tap(find.byKey(const ValueKey('retry-projectLayout')));
      await t.pumpAndSettle();
      expect(c.health.issues, isEmpty);
      expect(c.live, isTrue);
    },
  );

  testWidgets(
    'revoked pairing has a persistent actionable notice, recovery removes it',
    (t) async {
      final c = OperableClient();
      addTearDown(c.dispose);
      c.health.transition(
        ConnectionPhase.revoked,
        failure: '设备配对已被工作站删除，请重新配对',
      );
      await t.pumpWidget(AgentDeckApp(client: c));
      await t.pumpAndSettle();
      expect(
        find.byKey(const ValueKey('client-blocking-notice')),
        findsOneWidget,
      );
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('client-blocking-notice')),
          matching: find.text('Pair again'),
        ),
        findsOneWidget,
      );
      expect(find.text('Live connection'), findsNothing);
      c.health.transition(ConnectionPhase.synchronizing);
      c.notifyListeners();
      await t.pump();
      expect(
        find.byKey(const ValueKey('client-blocking-notice')),
        findsNothing,
      );
      expect(find.text('Syncing chats'), findsOneWidget);
      expect(c.live, isFalse);
      c.health.synchronized();
      c.notifyListeners();
      await t.pumpAndSettle();
      expect(find.text('Live connection'), findsOneWidget);
    },
  );

  testWidgets(
    'offline cached execution does not spin; drafting survives reconnect and sync',
    (t) async {
      final c = OperableClient();
      addTearDown(c.dispose);
      c.sessions = [
        AgentSession(
          id: 'a',
          agent: 'codex',
          title: 'Cached running chat',
          source: 'stream',
          managed: true,
          status: 'running',
          project: '/project-a',
        ),
      ];
      c.health.transition(ConnectionPhase.reconnecting, failure: '连接中断');
      await t.pumpWidget(AgentDeckApp(client: c));
      await t.pumpAndSettle();
      expect(find.byType(RunningIndicator), findsNothing);
      await t.tap(find.byKey(const ValueKey('session-a')));
      await t.pumpAndSettle();
      final input = find.descendant(
        of: find.byType(InputComposer),
        matching: find.byType(TextField),
      );
      await t.enterText(input, 'Draft retained during reconnect');
      await t.pump();
      expect(find.byType(RunningIndicator), findsNothing);
      final send = find.byKey(const ValueKey('send-message'));
      expect(t.widget<IconButton>(send).onPressed, isNull);
      c.health.transition(ConnectionPhase.synchronizing);
      c.notifyListeners();
      await t.pump();
      expect(t.widget<IconButton>(send).onPressed, isNull);
      c.health.synchronized();
      c.notifyListeners();
      await t.pump(const Duration(milliseconds: 100));
      expect(find.text('Draft retained during reconnect'), findsOneWidget);
      expect(t.widget<IconButton>(send).onPressed, isNotNull);
      await t.pumpWidget(const SizedBox());
    },
  );
}
