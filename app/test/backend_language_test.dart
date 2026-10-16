import 'dart:convert';
import 'dart:io';
import 'package:agentdeck/backend_messages.dart';
import 'package:agentdeck/ui_catalog.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:agentdeck/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'widget_test.dart' show OperableClient;

const authError = '请在工作站配置 WorkBuddy 开放平台应用、密钥环境变量及 HTTPS 回调地址';

class FailingLoginClient extends OperableClient {
  @override
  Future<Map<String, dynamic>> agentLogin(String id) async {
    notifyListeners();
    throw const HttpException(authError);
  }
}

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  test(
    'every app-visible backend message has all four translations',
    () async {
      final result = await Process.run(
        'go',
        ['run', '../scripts/export_ui_messages.go', '../backend/internal'],
        environment: {'GOWORK': 'off'},
      );
      expect(result.exitCode, 0, reason: '${result.stderr}');
      final records = (jsonDecode(result.stdout as String) as List).cast<Map>();
      expect(records.length, greaterThan(160));
      Set<String> slots(String s) =>
          RegExp(r'\{\d+\}').allMatches(s).map((m) => m[0]!).toSet();
      for (final record in records) {
        final key = record['key'] as String;
        for (final language in [
          AppLanguage.english,
          AppLanguage.traditionalChinese,
          AppLanguage.russian,
        ]) {
          final translated = UiStrings(language).text(key);
          expect(
            uiCatalog.containsKey(key) || backendUiMessages.containsKey(key),
            isTrue,
            reason: 'Missing ${language.code}: $key',
          );
          expect(translated, isNotEmpty);
          expect(slots(translated), slots(key), reason: key);
        }
        if (record['kind'] == 'notice') {
          expect(backendNoticeKeys, contains(key));
        }
      }
      for (final entry in backendUiMessages.entries) {
        expect(entry.value.length, 3);
        for (final value in entry.value) {
          expect(value, isNotEmpty);
          expect(slots(value), slots(entry.key), reason: entry.key);
        }
      }
    },
    timeout: const Timeout(Duration(seconds: 90)),
  );
  test('UI literals and native client errors have translations', () {
    final literal = RegExp(
      r"(?:\btr|FormatException|FileSystemException|HttpException)\(\s*'((?:\\.|[^'\\])*)'",
    );
    var checked = 0;
    for (final file in Directory(
      'lib',
    ).listSync(recursive: true).whereType<File>()) {
      if (!file.path.endsWith('.dart') ||
          file.path.endsWith('_catalog.dart') ||
          file.path.endsWith('backend_messages.dart')) {
        continue;
      }
      for (final match in literal.allMatches(file.readAsStringSync())) {
        final key = match[1]!
            .replaceAll(r'\n', '\n')
            .replaceAll(r"\'", "'")
            .replaceAll(r'\\', r'\');
        if (!RegExp(r'[\u4e00-\u9fff]').hasMatch(key)) continue;
        checked++;
        expect(
          uiCatalog.containsKey(key) || backendUiMessages.containsKey(key),
          isTrue,
          reason: '${file.path}: $key',
        );
      }
    }
    expect(checked, greaterThan(200));
  });
  test('formatted backend errors preserve status codes and agent names', () {
    for (final language in AppLanguage.values) {
      final strings = UiStrings(language);
      expect(
        strings.error(const HttpException(authError)),
        strings.text(authError),
      );
      expect(
        strings.error('HttpException: $authError'),
        strings.text(authError),
      );
      expect(
        strings.error('WorkBuddy 请求失败（HTTP 401），请检查授权或电脑在线状态'),
        strings.text('WorkBuddy 请求失败（HTTP {0}），请检查授权或电脑在线状态', [401]),
      );
      expect(
        strings.text('请在电脑完成 Qoder CLI 登录并检查模型配置'),
        strings.text('请在电脑完成 {0} CLI 登录并检查模型配置', ['Qoder']),
      );
      expect(
        strings.notice('图片暂不可用：图片不可用'),
        strings.text('图片暂不可用：{0}', [strings.text('图片不可用')]),
      );
      expect(
        strings.error('HTTP 429: provider_limit'),
        'HTTP 429: provider_limit',
      );
      expect(strings.notice(authError), authError);
      expect(strings.notice('自定义 Agent 提示'), '自定义 Agent 提示');
    }
  });
  testWidgets(
    'authorization failure stays with its operation and follows language',
    (t) async {
      final c = FailingLoginClient();
      final languages = LanguageController(DeviceVault());
      addTearDown(c.dispose);
      addTearDown(languages.dispose);
      await t.pumpWidget(AgentDeckApp(client: c, languages: languages));
      await t.pumpAndSettle();
      await t.tap(find.byTooltip('Settings'));
      await t.pumpAndSettle();
      final button = find.text('Authorize WorkBuddy assistant');
      await t.scrollUntilVisible(button, 250);
      await t.ensureVisible(button);
      await t.pumpAndSettle();
      await t.tap(button);
      await t.pumpAndSettle();
      expect(
        find.text(const UiStrings(AppLanguage.english).text(authError)),
        findsOneWidget,
      );
      expect(find.text(authError), findsNothing);
      await t.tap(find.byType(BackButton));
      await t.pumpAndSettle();
      await t.pump(const Duration(seconds: 5));
      await t.pumpAndSettle();
      expect(
        find.text(const UiStrings(AppLanguage.english).text(authError)),
        findsNothing,
      );
      await languages.select(AppLanguage.russian);
      await t.pumpAndSettle();
      expect(c.error, isEmpty);
      expect(c.live, isTrue);
      expect(find.text(authError), findsNothing);
    },
  );
  testWidgets('only backend notices are translated in a conversation', (
    t,
  ) async {
    final c = OperableClient();
    addTearDown(c.dispose);
    final transcript = Transcript();
    c.transcripts['a'] = transcript;
    for (final entry in [('message', authError), ('notice', '图片链接已隐藏敏感凭据')]) {
      transcript.add({
        'id': entry.$1,
        'seq': entry.$1 == 'message' ? 1 : 2,
        'kind': entry.$1,
        'role': 'assistant',
        'text': entry.$2,
        'mode': 'snapshot',
      });
    }
    await t.pumpWidget(AgentDeckApp(client: c));
    await t.pumpAndSettle();
    await t.tap(find.text('项目 A 会话'));
    await t.pumpAndSettle();
    expect(find.textContaining(authError, findRichText: true), findsOneWidget);
    expect(
      find.text(const UiStrings(AppLanguage.english).notice('图片链接已隐藏敏感凭据')),
      findsOneWidget,
    );
  });
}
