import 'dart:async';
import 'dart:io';
import 'package:agentdeck/agent_settings.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:agentdeck/input_composer.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/main.dart';
import 'package:agentdeck/ui_catalog.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'widget_test.dart' show OperableClient;

class PreferenceVault extends DeviceVault {
  String? saved;
  bool fail = false;
  Completer<String?>? read;
  Completer<void>? write;
  @override
  Future<String?> loadLanguage() async => read == null ? saved : read!.future;
  @override
  Future<void> saveLanguage(String code) async {
    if (write != null) await write!.future;
    if (fail) throw const FileSystemException('fixture');
    saved = code;
  }
}

Future<void> choose(WidgetTester t, AppLanguage language) async {
  await t.tap(find.byKey(const ValueKey('language-setting')));
  await t.pumpAndSettle();
  for (final option in AppLanguage.values) {
    expect(
      find.descendant(
        of: find.byKey(ValueKey('language-${option.code}')),
        matching: find.text(option.label),
      ),
      findsOneWidget,
    );
  }
  await t.tap(find.byKey(ValueKey('language-${language.code}')));
  await t.pumpAndSettle();
}

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  test(
    'English is the default; unsupported preferences fall back to English',
    () async {
      final v = PreferenceVault();
      final c = LanguageController(v);
      addTearDown(c.dispose);
      expect(c.language, AppLanguage.english);
      await c.load();
      expect(c.language, AppLanguage.english);
      v.saved = 'invalid';
      await c.load();
      expect(c.language, AppLanguage.english);
    },
  );
  test(
    'all four language preferences survive a new vault/controller',
    () async {
      for (final language in AppLanguage.values) {
        final c = LanguageController(DeviceVault());
        await c.select(language);
        final restored = LanguageController(DeviceVault());
        await restored.load();
        expect(restored.language, language);
        expect(restored.language.locale, language.locale);
        c.dispose();
        restored.dispose();
      }
    },
  );
  test(
    'failed writes retain the saved language and a later save recovers',
    () async {
      final v = PreferenceVault()..fail = true;
      final c = LanguageController(v);
      addTearDown(c.dispose);
      await expectLater(
        c.select(AppLanguage.russian),
        throwsA(isA<FileSystemException>()),
      );
      expect(c.language, AppLanguage.english);
      v.fail = false;
      await c.select(AppLanguage.traditionalChinese);
      expect(c.language, AppLanguage.traditionalChinese);
      expect(v.saved, 'zh-Hant');
    },
  );
  test('a delayed startup read cannot override a user selection', () async {
    final v = PreferenceVault()..read = Completer<String?>();
    final c = LanguageController(v);
    addTearDown(c.dispose);
    final loading = c.load();
    await c.select(AppLanguage.russian);
    v.read!.complete('zh-Hans');
    await loading;
    expect(c.language, AppLanguage.russian);
  });
  test(
    'rapid saves are ordered; disposing during IO does not notify',
    () async {
      final v = PreferenceVault()..write = Completer<void>();
      final c = LanguageController(v);
      final first = c.select(AppLanguage.simplifiedChinese);
      final last = c.select(AppLanguage.russian);
      c.dispose();
      v.write!.complete();
      await Future.wait([first, last]);
      expect(v.saved, 'ru');
    },
  );
  test('catalogs have complete translations and identical placeholders', () {
    Set<String> slots(String s) =>
        RegExp(r'\{\d+\}').allMatches(s).map((m) => m[0]!).toSet();
    for (final entry in uiCatalog.entries) {
      expect(entry.value.length, 3, reason: entry.key);
      for (final translated in entry.value) {
        expect(translated.trim(), isNotEmpty, reason: entry.key);
        expect(slots(translated), slots(entry.key), reason: entry.key);
      }
    }
    for (final language in AppLanguage.values) {
      final strings = UiStrings(language);
      // Never reinterpret placeholders or translate conversation/project text.
      expect(strings.text('发消息给 {0}', ['项目 {0}']), contains('项目 {0}'));
      expect(strings.text('My project / 中文会话'), 'My project / 中文会话');
      expect(
        strings.error(const FormatException('图片超过 8 MiB')),
        strings.text('图片超过 8 MiB'),
      );
    }
  });
  test('forgetting a workstation preserves the language preference', () async {
    final support = Directory.systemTemp.createTempSync('language-vault');
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          const MethodChannel('plugins.flutter.io/path_provider'),
          (_) async => support.path,
        );
    addTearDown(() {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            const MethodChannel('plugins.flutter.io/path_provider'),
            null,
          );
      support.deleteSync(recursive: true);
    });
    final v = DeviceVault();
    await v.saveLanguage('ru');
    await v.clear();
    expect(await DeviceVault().loadLanguage(), 'ru');
  });
  test('English and Russian counts use correct plural forms', () {
    const en = UiStrings(AppLanguage.english);
    const ru = UiStrings(AppLanguage.russian);
    expect(en.count(1, chats: true), '1 chat');
    expect(en.count(2, chats: false), '2 projects');
    expect(ru.count(1, chats: true), '1 беседа');
    expect(ru.count(2, chats: true), '2 беседы');
    expect(ru.count(11, chats: true), '11 бесед');
    expect(ru.count(21, chats: false), '21 проект');
    expect(ru.count(24, chats: false), '24 проекта');
    expect(ru.count(114, chats: false), '114 проектов');
  });
  testWidgets('default is English even on a Chinese phone', (t) async {
    t.platformDispatcher.localeTestValue = const Locale('zh', 'CN');
    addTearDown(t.platformDispatcher.clearLocaleTestValue);
    final c = OperableClient();
    addTearDown(c.dispose);
    await t.pumpWidget(AgentDeckApp(client: c));
    await t.pumpAndSettle();
    expect(find.text('Projects'), findsOneWidget);
    expect(find.text('Recent chats'), findsOneWidget);
    expect(find.byTooltip('Settings'), findsOneWidget);
    expect(find.text('项目 A 会话'), findsOneWidget);
  });
  testWidgets(
    'settings switches all four languages immediately and restores on restart',
    (t) async {
      final client = OperableClient();
      addTearDown(client.dispose);
      final languages = LanguageController(DeviceVault());
      addTearDown(languages.dispose);
      await t.pumpWidget(AgentDeckApp(client: client, languages: languages));
      await t.pumpAndSettle();
      await t.tap(find.byTooltip('Settings'));
      await t.pumpAndSettle();
      final titles = ['Settings', '设置', '設定', 'Настройки'];
      for (var i = 0; i < AppLanguage.values.length; i++) {
        final language = AppLanguage.values[i];
        await choose(t, language);
        expect(find.text(titles[i]), findsOneWidget);
        expect(find.text(UiStrings(language).text('语言')), findsOneWidget);
        expect(languages.language, language);
      }
      await t.tap(find.byType(BackButton));
      await t.pumpAndSettle();
      expect(find.text('Проекты'), findsOneWidget);
      expect(find.text('Недавние беседы'), findsOneWidget);
      expect(find.text('项目 A 会话'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      final restored = LanguageController(DeviceVault());
      addTearDown(restored.dispose);
      await restored.load();
      await t.pumpWidget(AgentDeckApp(client: client, languages: restored));
      await t.pumpAndSettle();
      expect(find.byTooltip('Настройки'), findsOneWidget);
      expect(client.agentVisibility.hidden, isEmpty);
      expect(client.projectLayout.hidden, isEmpty);
    },
  );
  testWidgets(
    'language picker supports Russian with large text on a narrow phone',
    (t) async {
      t.view.physicalSize = const Size(320, 780);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final client = AgentClient();
      final languages = LanguageController(
        PreferenceVault(),
        initial: AppLanguage.russian,
      );
      addTearDown(client.dispose);
      addTearDown(languages.dispose);
      await t.pumpWidget(
        LanguageScope(
          controller: languages,
          child: MaterialApp(
            locale: AppLanguage.russian.locale,
            supportedLocales: UiStrings.supportedLocales,
            localizationsDelegates: UiStrings.delegates,
            builder: (context, child) => MediaQuery(
              data: MediaQuery.of(
                context,
              ).copyWith(textScaler: const TextScaler.linear(2)),
              child: child!,
            ),
            home: AgentSettingsPage(client: client),
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.scrollUntilVisible(
        find.byKey(const ValueKey('language-setting')),
        200,
        scrollable: find.byType(Scrollable).first,
      );
      await t.tap(find.byKey(const ValueKey('language-setting')));
      await t.pumpAndSettle();
      expect(find.text('繁體中文'), findsOneWidget);
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    'open conversation UI updates without replacing the draft or content',
    (t) async {
      final c = OperableClient();
      addTearDown(c.dispose);
      final languages = LanguageController(PreferenceVault());
      addTearDown(languages.dispose);
      await t.pumpWidget(AgentDeckApp(client: c, languages: languages));
      await t.pumpAndSettle();
      await t.tap(find.text('项目 A 会话'));
      await t.pumpAndSettle();
      final composer = t.widget<InputComposer>(find.byType(InputComposer));
      composer.controller.text = '中文原文 / original {0}';
      for (final language in AppLanguage.values) {
        await languages.select(language);
        await t.pumpAndSettle();
        expect(
          find.byTooltip(UiStrings(language).text('添加图片或文件')),
          findsOneWidget,
        );
        expect(find.text('中文原文 / original {0}'), findsOneWidget);
        expect(find.text('项目 A 会话'), findsOneWidget);
        await t.tap(find.byTooltip(UiStrings(language).text('添加图片或文件')));
        await t.pumpAndSettle();
        expect(find.text(UiStrings(language).text('添加附件')), findsOneWidget);
        expect(find.text(UiStrings(language).text('文件')), findsOneWidget);
        Navigator.of(
          t.element(find.text(UiStrings(language).text('添加附件'))),
        ).pop();
        await t.pumpAndSettle();
      }
      expect(c.sent, isEmpty);
    },
  );
}
