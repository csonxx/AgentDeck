import 'dart:convert';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/usage_resets.dart';
import 'package:agentdeck/codex_reset_panel.dart';
import 'package:agentdeck/language.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

class ResetClient extends AgentClient {
  int prepares = 0, consumes = 0;
  bool eligible = true;
  String outcome = 'reset';
  final List<String> keys = [];
  @override
  Future<Map<String, dynamic>> codexResetState() async => {
    'available_count': 3,
    'details_known': true,
    'eligible': eligible,
    'eligibility_known': true,
    'account_version': 'account-fixture',
    'credits': [
      {
        'id': 'a',
        'title': 'Example reset',
        'description': 'Native detail',
        'status': 'available',
        'reset_type': 'codexRateLimits',
        'granted_at': '2026-10-20T08:00:00Z',
        'expires_at': '2030-01-01T08:00:00Z',
      },
    ],
  };
  @override
  Future<Map<String, dynamic>> codexResetHistory([
    String cursor = '',
  ]) async => {
    'events': [
      {'id': 'x', 'kind': 'granted', 'occurred_at': '2026-10-20T08:00:00Z'},
      {'id': 'y', 'kind': 'redeemed', 'occurred_at': '2026-10-21T08:00:00Z'},
    ],
    'next': '',
  };
  @override
  Future<Map<String, dynamic>> prepareCodexReset(
    String credit,
    String account,
  ) async {
    prepares++;
    return {'intent': 'intent-test', 'expires_at': '2030-01-01T00:00:00Z'};
  }

  @override
  Future<Map<String, dynamic>> consumeCodexReset(
    String intent,
    String id,
  ) async {
    consumes++;
    keys.add(id);
    return {'outcome': outcome};
  }
}

Future<void> mount(WidgetTester t, ResetClient c, {double scale = 1}) async {
  await t.pumpWidget(
    LanguageScope(
      controller: LanguageController(
        c.vault,
        initial: AppLanguage.simplifiedChinese,
      ),
      child: MaterialApp(
        locale: AppLanguage.simplifiedChinese.locale,
        localizationsDelegates: UiStrings.delegates,
        supportedLocales: UiStrings.supportedLocales,
        home: MediaQuery(
          data: MediaQueryData(textScaler: TextScaler.linear(scale)),
          child: Scaffold(
            body: SingleChildScrollView(
              child: CodexResetPanel(client: c, onChanged: () {}),
            ),
          ),
        ),
      ),
    ),
  );
  await t.pumpAndSettle();
}

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  test('missing count and missing detail remain unknown', () {
    final s = CodexResetState({});
    expect(s.available, isNull);
    expect(s.detailsKnown, isFalse);
    expect(s.eligible, isFalse);
  });
  testWidgets('view and cancellation never consume a reset', (t) async {
    final c = ResetClient();
    addTearDown(c.dispose);
    await mount(t, c);
    expect(find.text('可用 3 张'), findsOneWidget);
    expect(find.text('Native detail'), findsOneWidget);
    expect(find.text('获得重置卡'), findsOneWidget);
    expect(find.text('已使用重置卡'), findsOneWidget);
    expect(c.consumes, 0);
    await t.ensureVisible(find.byKey(const ValueKey('reset-use-a')));
    await t.tap(find.byKey(const ValueKey('reset-use-a')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 350));
    expect(c.prepares, 0);
    expect(find.text('使用 Codex 重置卡？'), findsOneWidget);
    expect(c.consumes, 0);
    await t.tap(find.byKey(const ValueKey('reset-cancel')));
    await t.pumpAndSettle();
    expect(c.consumes, 0);
    expect(c.prepares, 0);
  });
  testWidgets('back dismisses confirmation without preparing or using a card', (
    t,
  ) async {
    final c = ResetClient();
    addTearDown(c.dispose);
    await mount(t, c);
    await t.tap(find.byKey(const ValueKey('reset-use-a')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 350));
    await t.binding.handlePopRoute();
    await t.pumpAndSettle();
    expect(find.text('使用 Codex 重置卡？'), findsNothing);
    expect(c.prepares, 0);
    expect(c.consumes, 0);
  });
  testWidgets(
    'changing workstation while confirming never submits on the new binding',
    (t) async {
      final c = ResetClient();
      addTearDown(c.dispose);
      await mount(t, c);
      await t.tap(find.byKey(const ValueKey('reset-use-a')));
      await t.pump();
      await t.pump(const Duration(milliseconds: 350));
      c.device = {'url': 'https://other-workstation', 'device': 'other-device'};
      await t.tap(find.byKey(const ValueKey('reset-confirm')));
      await t.pumpAndSettle();
      expect(c.prepares, 0);
      expect(c.consumes, 0);
      expect(
        await c.vault.storage.read(key: 'agentdeck.codex.reset.pending'),
        isNull,
      );
    },
  );
  testWidgets('explicit confirmation persists one attempt before consume', (
    t,
  ) async {
    final c = ResetClient();
    addTearDown(c.dispose);
    await mount(t, c);
    await t.tap(find.byKey(const ValueKey('reset-use-a')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 350));
    expect(c.consumes, 0);
    expect(c.prepares, 0);
    await t.tap(find.byKey(const ValueKey('reset-confirm')));
    await t.pumpAndSettle();
    expect(c.consumes, 1);
    expect(c.prepares, 1);
    expect(find.text('已使用一张重置卡，额度已重置'), findsOneWidget);
    expect(
      await c.vault.storage.read(key: 'agentdeck.codex.reset.pending'),
      isNull,
    );
  });
  testWidgets(
    'unknown outcome restores without automatic retry and reuses key',
    (t) async {
      final c = ResetClient()..outcome = 'unknown';
      addTearDown(c.dispose);
      await mount(t, c);
      await t.tap(find.byKey(const ValueKey('reset-use-a')));
      await t.pump();
      await t.pump(const Duration(milliseconds: 350));
      await t.tap(find.byKey(const ValueKey('reset-confirm')));
      await t.pumpAndSettle();
      expect(c.consumes, 1);
      final saved = jsonDecode(
        (await c.vault.storage.read(key: 'agentdeck.codex.reset.pending'))!,
      );
      expect(saved['id'], c.keys.single);
      await t.pumpWidget(const SizedBox());
      await mount(t, c);
      expect(c.consumes, 1);
      await t.ensureVisible(find.byKey(const ValueKey('reset-retry')));
      await t.tap(find.byKey(const ValueKey('reset-retry')));
      await t.pumpAndSettle();
      expect(c.consumes, 1);
      c.outcome = 'alreadyRedeemed';
      await t.tap(find.byKey(const ValueKey('reset-confirm')));
      await t.pumpAndSettle();
      expect(c.keys[0], c.keys[1]);
      expect(c.consumes, 2);
    },
  );
  testWidgets('no eligible quota disables use and large text fits', (t) async {
    final c = ResetClient()..eligible = false;
    addTearDown(c.dispose);
    await t.binding.setSurfaceSize(const Size(320, 700));
    addTearDown(() => t.binding.setSurfaceSize(null));
    await mount(t, c, scale: 2);
    final b = t.widget<OutlinedButton>(
      find.byKey(const ValueKey('reset-use-a')),
    );
    expect(b.onPressed, isNull);
    expect(c.consumes, 0);
    expect(t.takeException(), isNull);
  });
}
