import 'package:agentdeck/language.dart';
import 'dart:io';
import 'package:flutter/services.dart';
import 'package:agentdeck/core/agents.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:agentdeck/agent_settings.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

class FailingAgentVault extends DeviceVault {
  @override
  Future<void> saveAgentVisibility(Map<String, dynamic> value) async =>
      throw StateError('fixture write failure');
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final support = Directory.systemTemp.createTempSync('agent-visibility-test');
  setUpAll(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          const MethodChannel('plugins.flutter.io/path_provider'),
          (_) async => support.path,
        );
  });
  tearDownAll(() {
    support.deleteSync(recursive: true);
  });
  test(
    'six agents persist independently of pairing and project visibility',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final c = AgentClient(vault: DeviceVault());
      addTearDown(c.dispose);
      expect(c.visibleAgents.map((a) => a.id), agentNames.keys);
      await c.updateAgentVisibility(
        AgentVisibility(hidden: ['codex', 'qoder']),
      );
      final restored = AgentClient(vault: DeviceVault());
      addTearDown(restored.dispose);
      await restored.loadAgentVisibility();
      expect(restored.visibleAgents.map((a) => a.id), [
        'claude',
        'opencode',
        'trae',
        'workbuddy',
      ]);
      await restored.vault.clear();
      await restored.loadAgentVisibility();
      expect(restored.agentVisibility.hidden, {'codex', 'qoder'});
      expect(restored.projectLayout.hidden, isEmpty);
    },
  );
  test('failed secure save rolls back visibility', () async {
    final c = AgentClient(vault: FailingAgentVault());
    addTearDown(c.dispose);
    await expectLater(
      c.updateAgentVisibility(AgentVisibility(hidden: ['codex'])),
      throwsA(isA<FileSystemException>()),
    );
    expect(c.agentVisibility.hidden, isEmpty);
  });
  test(
    'future agent IDs survive saved preferences and all hidden can recover',
    () {
      var v = AgentVisibility(hidden: [...agentNames.keys, 'future']);
      v = AgentVisibility.fromJson(v.toJson()).setVisible('trae', true);
      expect(v.hidden.contains('future'), isTrue);
      expect(v.hidden.contains('trae'), isFalse);
    },
  );
  testWidgets('settings handles six providers and large text on narrow phone', (
    tester,
  ) async {
    FlutterSecureStorage.setMockInitialValues({});
    tester.view.physicalSize = const Size(320, 740);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final c = AgentClient();
    addTearDown(c.dispose);
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: UiStrings.delegates,
        supportedLocales: UiStrings.supportedLocales,
        locale: AppLanguage.simplifiedChinese.locale,
        home: MediaQuery(
          data: const MediaQueryData(textScaler: TextScaler.linear(2)),
          child: AgentSettingsPage(client: c),
        ),
      ),
    );
    expect(find.text('Codex'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('授权 WorkBuddy 本地助理'),
      250,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    expect(find.text('授权 WorkBuddy 本地助理'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
