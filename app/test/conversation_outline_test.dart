import 'package:agentdeck/language.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/main.dart';
import 'package:agentdeck/conversation_outline.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class DirectoryClient extends AgentClient {
  int requestedFirst = 0;
  bool fail = false;
  DirectoryClient() {
    connected = true;
    selected = 's';
    sessions = [
      AgentSession(
        id: 's',
        agent: 'codex',
        title: 'Directory fixture',
        source: 'log',
        status: 'idle',
        project: '/fixture',
      ),
    ];
    transcripts['s'] = Transcript();
  }
  Map<String, dynamic> entry(int i) => {
    'key': 'm$i:message',
    'first': i,
    'kind': 'message',
    'role': i.isEven ? 'user' : 'assistant',
    'preview': 'Dialogue record $i',
    'time': '2026-10-08T00:00:00Z',
  };
  Map<String, dynamic> event(int i) => {
    'id': 'm$i',
    'item': 'm$i',
    'seq': i,
    'first': i,
    'kind': 'message',
    'role': 'assistant',
    'text': 'Full message $i',
    'mode': 'snapshot',
  };
  @override
  Future<Map<String, dynamic>> outlinePage(String id, {int before = 0}) async {
    if (fail) throw StateError('fixture unavailable');
    return {
      'entries': before == 0 ? [entry(100), entry(90)] : [entry(10), entry(1)],
      'before': before == 0 ? 90 : 1,
      'has_more': before == 0,
    };
  }

  @override
  Future<void> jumpToRecord(String id, int first) async {
    requestedFirst = first;
    transcripts[id]!.history({
      'events': [for (var i = first; i < first + 10; i++) event(i)],
      'cursor': 100,
      'before': first,
      'has_more': first > 1,
      'has_newer': true,
    }, replace: true);
    notifyListeners();
  }

  @override
  Future<void> reload(String id) async {
    transcripts[id]!.history({
      'events': [event(100)],
      'cursor': 100,
      'before': 100,
      'has_more': false,
    }, replace: true);
    notifyListeners();
  }

  @override
  Future<void> unwatch(String id) async {}
  @override
  Future<void> loadOlder(String id) async {}
}

class ActivityDirectoryClient extends DirectoryClient {
  @override
  Map<String, dynamic> entry(int i) => {
    ...super.entry(i),
    'key': 'm$i:tool_result',
    'kind': 'tool_result',
  };
  @override
  Map<String, dynamic> event(int i) => {
    ...super.event(i),
    'kind': 'tool_result',
    'role': '',
  };
}

void main() {
  testWidgets('outline filters sender across all pages and combines search', (
    tester,
  ) async {
    final c = DirectoryClient();
    addTearDown(c.dispose);
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: UiStrings.delegates,
        supportedLocales: UiStrings.supportedLocales,
        locale: AppLanguage.simplifiedChinese.locale,
        home: Scaffold(
          body: ConversationOutline(client: c, session: 's'),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('outline-filter-sent')));
    await tester.pumpAndSettle();
    expect(find.text('Dialogue record 10'), findsOneWidget);
    expect(find.text('Dialogue record 1'), findsNothing);
    await tester.enterText(find.byType(TextField), 'record 1');
    await tester.pumpAndSettle();
    expect(find.text('Dialogue record 10'), findsOneWidget);
    expect(find.text('Dialogue record 90'), findsNothing);
    await tester.tap(find.byKey(const ValueKey('outline-filter-received')));
    await tester.pumpAndSettle();
    expect(find.text('Dialogue record 1'), findsOneWidget);
    expect(find.text('Dialogue record 10'), findsNothing);
    await tester.tap(find.text('Dialogue record 1'));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
  });

  testWidgets('outline expands collapsed activity and the target detail', (
    tester,
  ) async {
    final c = ActivityDirectoryClient();
    addTearDown(c.dispose);
    c.transcripts['s']!.add(c.event(100));
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: UiStrings.delegates,
        supportedLocales: UiStrings.supportedLocales,
        locale: AppLanguage.simplifiedChinese.locale,
        home: ConversationPage(client: c, session: c.sessions.first),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('Full message 100'), findsNothing);
    await tester.tap(find.byTooltip('对话目录'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Dialogue record 10'));
    await tester.pumpAndSettle();
    expect(c.requestedFirst, 10);
    expect(find.text('Full message 10'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('all-activities-toggle')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('all-activities-toggle')));
    await tester.pumpAndSettle();
    expect(find.byType(ExpansionTile), findsNothing);
  });

  testWidgets(
    'directory includes unloaded records and jumps precisely without all bodies',
    (tester) async {
      final c = DirectoryClient();
      addTearDown(c.dispose);
      c.transcripts['s']!.add(c.event(100));
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: ConversationPage(client: c, session: c.sessions.first),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('对话目录'));
      await tester.pumpAndSettle();
      expect(find.text('Dialogue record 1'), findsOneWidget);
      expect(find.text('4 条对话记录 · 含未加载的历史，点击即可跳转'), findsOneWidget);
      await tester.enterText(find.byType(TextField).last, 'record 10');
      await tester.pumpAndSettle();
      expect(find.text('Dialogue record 90'), findsNothing);
      await tester.tap(find.text('Dialogue record 10'));
      await tester.pumpAndSettle();
      expect(c.requestedFirst, 10);
      expect(c.transcripts['s']!.blocks.length, 10);
      expect(find.text('Full message 10'), findsOneWidget);
      expect(find.text('Full message 100'), findsNothing);
      await tester.tap(find.byTooltip('回到最新消息'));
      await tester.pumpAndSettle();
      expect(find.text('Full message 100'), findsOneWidget);
      expect(c.transcripts['s']!.detached, false);
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets(
    'directory retries and searches on a small screen with large text',
    (tester) async {
      tester.view.physicalSize = const Size(320, 740);
      tester.view.devicePixelRatio = 1;
      tester.platformDispatcher.textScaleFactorTestValue = 1.6;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
      final c = DirectoryClient()..fail = true;
      addTearDown(c.dispose);
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: Scaffold(
            body: ConversationOutline(client: c, session: 's'),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('目录加载失败，请重试'), findsOneWidget);
      c.fail = false;
      await tester.tap(find.text('目录加载失败，请重试'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), 'unmatched');
      await tester.pumpAndSettle();
      expect(find.text('没有匹配的记录'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );
}
