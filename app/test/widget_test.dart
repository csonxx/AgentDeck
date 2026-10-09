import 'package:agentdeck/language.dart';
import 'package:agentdeck/core/input.dart';
import 'package:agentdeck/input_composer.dart';
import 'package:agentdeck/core/agents.dart';
import 'dart:io';
import 'dart:async';
import 'dart:ui' as ui;
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/main.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

class EmptyGatewayVault extends DeviceVault {
  @override
  Future<Map<String, dynamic>?> loadGatewayAccount() async => null;
}

class MemoryLayoutVault extends DeviceVault {
  Map<String, dynamic>? layout;
  @override
  Future<Map<String, dynamic>?> loadProjectLayout() async => layout;
  @override
  Future<void> saveProjectLayout(Map<String, dynamic> value) async {
    layout = Map<String, dynamic>.from(value);
  }
}

class OperableClient extends AgentClient {
  String sent = '', forkedFrom = '';
  OperableClient({DeviceVault? vault})
    : super(vault: vault ?? MemoryLayoutVault()) {
    connected = true;
    state = '实时连接';
    projects = [
      AgentProject(id: 'p1', name: 'project-a', path: '/project-a'),
      AgentProject(id: 'p2', name: 'project-b', path: '/project-b'),
    ];
    sessions = [
      AgentSession(
        id: 'a',
        agent: 'codex',
        title: '项目 A 会话',
        source: 'stream',
        status: 'idle',
        updated: '2026-10-08T01:00:00Z',
        project: '/project-a',
        managed: true,
      ),
      AgentSession(
        id: 'b',
        agent: 'codex',
        title: '项目 B 会话',
        source: 'stream',
        status: 'idle',
        updated: '2026-10-08T02:00:00Z',
        project: '/project-b',
        managed: true,
      ),
    ];
  }
  @override
  Future<void> watch(String id) async {
    selected = id;
    transcripts.putIfAbsent(id, Transcript.new);
    notifyListeners();
  }

  @override
  Future<AgentSession> createSession(
    String agent,
    String project, {
    String parent = '',
  }) async {
    forkedFrom = parent;
    final child = AgentSession(
      id: 'child',
      agent: agent,
      title: '手机续聊',
      source: 'stream',
      status: 'idle',
      project: projects.firstWhere((p) => p.id == project).path,
      managed: true,
    );
    sessions.add(child);
    await watch(child.id);
    return child;
  }

  @override
  Future<void> sendPrompt(String session, String text) async {
    sent = '$session:$text';
    transcripts[session]!.add({
      'id': 'reply',
      'seq': 1,
      'kind': 'message',
      'role': 'assistant',
      'text': '真实协议回复位置',
      'mode': 'snapshot',
    });
    notifyListeners();
  }
}

class AttachmentGuardClient extends OperableClient {
  int uploads = 0;
  final capabilities = Completer<Map<String, dynamic>>();
  @override
  Future<dynamic> request(Map<String, dynamic> data) async {
    if (data['method'] == 'input_capabilities') return capabilities.future;
    return null;
  }

  @override
  Future<String> uploadFile(
    String session,
    DraftFile file, {
    String kind = 'file',
    void Function(int, int)? progress,
  }) async {
    uploads++;
    return 'upload';
  }
}

class PagingClient extends OperableClient {
  final page = Completer<Map<String, dynamic>>();
  int requestedBefore = 0;
  @override
  Future<dynamic> request(Map<String, dynamic> data) {
    requestedBefore = data['before'] as int;
    return page.future;
  }
}

void main() {
  testWidgets(
    'unsupported images preserve the draft before adopting a desktop chat or uploading',
    (tester) async {
      final c = AttachmentGuardClient();
      addTearDown(c.dispose);
      final session = AgentSession(
        id: 'a',
        agent: 'opencode',
        title: 'Image guard',
        source: 'desktop',
        status: 'idle',
        updated: '',
        project: '/project-a',
        managed: false,
      );
      c.sessions = [session];
      await c.watch('a');
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: ConversationPage(client: c, session: session),
        ),
      );
      await tester.pumpAndSettle();
      final composer = tester.widget<InputComposer>(find.byType(InputComposer));
      composer.controller.text = 'Keep this draft';
      final pending = composer.onSend(const TurnOptions(), [
        DraftFile('image.png', 'image/png', Uint8List.fromList([1])),
      ]);
      await tester.pump();
      expect(find.text('正在检查图片支持…'), findsOneWidget);
      expect(c.forkedFrom, isEmpty);
      expect(c.uploads, 0);
      c.capabilities.complete({'images': false, 'files': true});
      expect(await pending, isFalse);
      await tester.pumpAndSettle();
      expect(composer.controller.text, 'Keep this draft');
      expect(c.forkedFrom, isEmpty);
      expect(c.uploads, 0);
      expect(c.sent, isEmpty);
      expect(find.textContaining('未提供图片输入'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      await tester.pump();
    },
  );
  testWidgets('history paging keeps the viewport and returns to latest', (
    tester,
  ) async {
    final c = PagingClient();
    addTearDown(c.dispose);
    await c.watch('a');
    Map<String, dynamic> event(int i) => {
      'id': 'history-$i',
      'seq': i,
      'first': i,
      'kind': 'notice',
      'text': 'Historical message $i',
      'mode': 'snapshot',
    };
    final t = c.transcripts['a']!;
    t.history({
      'events': [for (var i = 61; i <= 100; i++) event(i)],
      'cursor': 100,
      'before': 61,
      'has_more': true,
    }, replace: true);
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: UiStrings.delegates,
        supportedLocales: UiStrings.supportedLocales,
        locale: AppLanguage.simplifiedChinese.locale,
        home: ConversationPage(client: c, session: c.sessions.first),
      ),
    );
    await tester.pumpAndSettle();
    final list = find.byType(ListView);
    await tester.drag(list, const Offset(0, 300));
    await tester.pumpAndSettle();
    final state = tester.state<ScrollableState>(
      find.descendant(of: list, matching: find.byType(Scrollable)).first,
    );
    state.position.jumpTo(state.position.maxScrollExtent);
    await tester.pumpAndSettle();
    await tester.tap(find.text('加载更早消息'));
    await tester.pump();
    expect(c.requestedBefore, 61);
    expect(find.text('正在加载…'), findsOneWidget);
    final before = state.position.pixels;
    c.page.complete({
      'events': [for (var i = 21; i <= 60; i++) event(i)],
      'cursor': 100,
      'before': 21,
      'has_more': true,
    });
    await tester.pumpAndSettle();
    expect(t.blocks.length, 80);
    expect(t.blocks.first.text, 'Historical message 21');
    expect(state.position.pixels, closeTo(before, 1));
    await tester.tap(find.byTooltip('回到最新消息'));
    await tester.pumpAndSettle();
    expect(state.position.pixels, 0);
    expect(find.text('Historical message 100'), findsOneWidget);
    // Allow the periodic cache task to flush before widget-test teardown.
    await tester.pump(const Duration(seconds: 6));
    expect(tester.takeException(), isNull);
  });
  testWidgets(
    'project folders and agent filter preserve sending in selected session',
    (tester) async {
      final c = OperableClient();
      final b = c.sessions[1];
      c.sessions[1] = AgentSession(
        id: b.id,
        agent: 'claude',
        title: b.title,
        source: b.source,
        status: b.status,
        project: b.project,
        managed: true,
      );
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
      await tester.pumpAndSettle();
      expect(find.text('project-a'), findsOneWidget);
      expect(find.text('project-b'), findsNothing);
      expect(find.text('全部'), findsNothing);
      expect(find.text('项目 A 会话'), findsOneWidget);
      await tester.tap(find.widgetWithText(ChoiceChip, 'Claude'));
      await tester.pumpAndSettle();
      expect(find.text('项目 A 会话'), findsNothing);
      expect(find.text('project-b'), findsOneWidget);
      expect(find.text('项目 B 会话'), findsOneWidget);
      await tester.tap(find.widgetWithText(ChoiceChip, 'Codex'));
      await tester.pumpAndSettle();
      expect(find.text('项目 B 会话'), findsNothing);
      await tester.tap(find.text('项目 A 会话'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), '请检查项目');
      await tester.pump();
      await tester.tap(find.byTooltip('发送消息'));
      await tester.pumpAndSettle();
      expect(c.sent, 'a:请检查项目');
      expect(find.text('真实协议回复位置'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'latest conversations sort across projects and projects expand after five',
    (tester) async {
      final c = OperableClient();
      addTearDown(c.dispose);
      c.sessions = [
        c.sessions[0],
        c.sessions[1],
        for (var i = 0; i < 5; i++)
          AgentSession(
            id: 'extra-$i',
            agent: 'codex',
            title: '历史会话 $i',
            source: 'stream',
            status: 'idle',
            project: '/project-a',
            updated: '2026-10-07T00:00:00Z',
          ),
      ];
      await tester.pumpWidget(
        AgentDeckApp(
          client: c,
          languages: LanguageController(
            c.vault,
            initial: AppLanguage.simplifiedChinese,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('历史会话 4'), findsNothing);
      await tester.ensureVisible(
        find.byKey(const ValueKey('expand-/project-a')),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('expand-/project-a')));
      await tester.pumpAndSettle();
      expect(find.text('历史会话 4'), findsOneWidget);
      await tester.tap(find.text('最新会话'));
      await tester.pumpAndSettle();
      expect(
        tester.getTopLeft(find.text('项目 B 会话')).dy,
        lessThan(tester.getTopLeft(find.text('项目 A 会话')).dy),
      );
      expect(find.text('project-b · Codex'), findsOneWidget);
      await tester.tap(find.text('项目'));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('收起显示'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('收起显示'));
      await tester.pumpAndSettle();
      expect(find.text('历史会话 4'), findsNothing);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'whole projects collapse and saved order survives reopening and live updates',
    (tester) async {
      final semantics = tester.ensureSemantics();
      final vault = MemoryLayoutVault();
      final c = OperableClient(vault: vault);
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
      await tester.pumpAndSettle();
      expect(
        tester
            .getSemantics(
              find.byKey(const ValueKey('project-toggle-/project-a')),
            )
            .getSemanticsData()
            .hasAction(ui.SemanticsAction.tap),
        isTrue,
      );
      await tester.tap(find.byKey(const ValueKey('project-toggle-/project-a')));
      await tester.pumpAndSettle();
      expect(find.text('项目 A 会话'), findsNothing);
      expect(find.text('project-a'), findsOneWidget);
      await tester.tap(find.byTooltip('项目管理'));
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('上移 project-b'));
      await tester.pumpAndSettle();
      expect(c.projectLayout.scoped('codex').order, [
        '/project-b',
        '/project-a',
      ]);
      await tester.tap(find.byType(BackButton));
      await tester.pumpAndSettle();
      expect(
        tester.getTopLeft(find.text('project-b')).dy,
        lessThan(tester.getTopLeft(find.text('project-a')).dy),
      );
      // An agent update changes timestamps but cannot override the saved project order.
      final old = c.sessions.first;
      c.sessions[0] = AgentSession(
        id: old.id,
        agent: old.agent,
        title: old.title,
        source: old.source,
        status: old.status,
        project: old.project,
        updated: '2099-01-01T00:00:00Z',
      );
      c.notifyListeners();
      await tester.pumpAndSettle();
      expect(
        tester.getTopLeft(find.text('project-b')).dy,
        lessThan(tester.getTopLeft(find.text('project-a')).dy),
      );
      await tester.pumpWidget(const SizedBox());
      final restored = OperableClient(vault: vault);
      addTearDown(restored.dispose);
      await restored.loadProjectLayout();
      await tester.pumpWidget(
        AgentDeckApp(
          client: restored,
          languages: LanguageController(
            restored.vault,
            initial: AppLanguage.simplifiedChinese,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('项目 A 会话'), findsNothing);
      expect(
        tester.getTopLeft(find.text('project-b')).dy,
        lessThan(tester.getTopLeft(find.text('project-a')).dy),
      );
      await tester.tap(find.byKey(const ValueKey('project-toggle-/project-a')));
      await tester.pumpAndSettle();
      expect(find.text('项目 A 会话'), findsOneWidget);
      await tester.tap(find.text('最新会话'));
      await tester.pumpAndSettle();
      expect(
        tester.getTopLeft(find.text('项目 B 会话')).dy,
        lessThan(tester.getTopLeft(find.text('项目 A 会话')).dy),
      );
      semantics.dispose();
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'project drag ordering and reset work on a narrow screen with large text',
    (tester) async {
      tester.view.physicalSize = const Size(320, 740);
      tester.view.devicePixelRatio = 1;
      tester.platformDispatcher.textScaleFactorTestValue = 1.6;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
      final c = OperableClient();
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
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('项目管理'));
      await tester.pumpAndSettle();
      final first = tester.getCenter(
        find.byKey(const ValueKey('drag-/project-a')),
      );
      final second = tester.getCenter(
        find.byKey(const ValueKey('drag-/project-b')),
      );
      final gesture = await tester.startGesture(first);
      await tester.pump(const Duration(milliseconds: 100));
      for (var i = 1; i <= 8; i++) {
        await gesture.moveTo(
          Offset(first.dx, first.dy + (second.dy + 60 - first.dy) * i / 8),
        );
        await tester.pump(const Duration(milliseconds: 70));
      }
      await tester.pump(const Duration(milliseconds: 300));
      await gesture.up();
      await tester.pumpAndSettle();
      expect(c.projectLayout.scoped('codex').order, [
        '/project-b',
        '/project-a',
      ]);
      await tester.tap(find.byTooltip('恢复默认排序'));
      await tester.pumpAndSettle();
      expect(
        c.projectLayout.scoped('codex').sorted(['/project-b', '/project-a']),
        ['/project-a', '/project-b'],
      );
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'project visibility persists and hiding all has a recovery action',
    (tester) async {
      final vault = MemoryLayoutVault();
      final c = OperableClient(vault: vault);
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
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('项目管理'));
      await tester.pumpAndSettle();
      await tester.tap(
        find.byWidgetPredicate(
          (w) => w is Checkbox && w.semanticLabel == '显示项目 project-a',
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byType(BackButton));
      await tester.pumpAndSettle();
      expect(find.text('project-a'), findsNothing);
      expect(find.text('项目 A 会话'), findsNothing);
      expect(find.text('project-b'), findsOneWidget);
      await tester.tap(find.text('最新会话'));
      await tester.pumpAndSettle();
      expect(find.text('项目 A 会话'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      final restored = OperableClient(vault: vault);
      addTearDown(restored.dispose);
      await restored.loadProjectLayout();
      await tester.pumpWidget(
        AgentDeckApp(
          client: restored,
          languages: LanguageController(
            restored.vault,
            initial: AppLanguage.simplifiedChinese,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('project-a'), findsNothing);
      await tester.tap(find.byTooltip('项目管理'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('全部隐藏'));
      await tester.pumpAndSettle();
      await tester.tap(find.byType(BackButton));
      await tester.pumpAndSettle();
      expect(find.text('当前项目已隐藏'), findsOneWidget);
      await tester.tap(find.text('管理项目'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('全部显示'));
      await tester.pumpAndSettle();
      await tester.tap(find.byType(BackButton));
      await tester.pumpAndSettle();
      expect(find.text('project-a'), findsOneWidget);
      expect(find.text('project-b'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'live running state animates in list and chat and clears on completion',
    (tester) async {
      final c = OperableClient();
      addTearDown(c.dispose);
      void setStatus(String status) {
        final s = c.sessions.first;
        c.sessions[0] = AgentSession(
          id: s.id,
          agent: s.agent,
          title: s.title,
          source: s.source,
          status: status,
          project: s.project,
          managed: true,
        );
      }

      setStatus('running');
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
      expect(find.byType(RunningIndicator), findsOneWidget);
      final progress = tester.widget<CircularProgressIndicator>(
        find.byType(CircularProgressIndicator),
      );
      expect(progress.value, isNull);
      expect(progress.semanticsLabel, '执行中');
      await tester.pump(const Duration(milliseconds: 300));
      expect(tester.binding.hasScheduledFrame, isTrue);
      await tester.tap(find.byKey(const ValueKey('project-toggle-/project-a')));
      await tester.pump();
      expect(find.text('项目 A 会话'), findsNothing);
      expect(find.byType(RunningIndicator), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('project-toggle-/project-a')));
      await tester.pump();
      await tester.tap(find.text('项目 A 会话'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 350));
      expect(find.byTooltip('停止任务'), findsOneWidget);
      expect(find.text('执行中'), findsOneWidget);
      expect(find.text('正在执行…'), findsOneWidget);
      await tester.pump(const Duration(milliseconds: 300));
      setStatus('idle');
      c.notifyListeners();
      await tester.pumpAndSettle();
      expect(find.byType(RunningIndicator), findsNothing);
      expect(find.byTooltip('停止任务'), findsNothing);
      expect(find.byTooltip('发送消息'), findsOneWidget);
      await tester.tap(find.byType(BackButton));
      await tester.pumpAndSettle();
      expect(find.byType(RunningIndicator), findsNothing);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'incoming output follows the end while reading older messages stays put',
    (tester) async {
      final c = OperableClient();
      addTearDown(c.dispose);
      await c.watch('a');
      final transcript = c.transcripts['a']!;
      void add(int n) => transcript.add({
        'id': 'm$n',
        'seq': n,
        'kind': 'message',
        'role': 'assistant',
        'text': '会话内容 $n\n\n这是可阅读的多行消息。',
        'mode': 'snapshot',
      });
      for (var i = 1; i <= 20; i++) {
        add(i);
      }
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: ConversationPage(client: c, session: c.sessions.first),
        ),
      );
      await tester.pumpAndSettle();
      final list = find.byType(ListView);
      final scrollable = find
          .descendant(of: list, matching: find.byType(Scrollable))
          .first;
      final position = tester.state<ScrollableState>(scrollable).position;
      expect(position.extentBefore, lessThan(1));
      await tester.drag(list, const Offset(0, 350));
      await tester.pumpAndSettle();
      expect(position.extentBefore, greaterThan(140));
      expect(find.byTooltip('回到最新消息'), findsOneWidget);
      final before = position.pixels;
      add(21);
      c.notifyListeners();
      await tester.pumpAndSettle();
      expect(position.pixels, closeTo(before, 1));
      await tester.tap(find.byTooltip('回到最新消息'));
      await tester.pumpAndSettle();
      expect(position.extentBefore, lessThan(1));
      add(22);
      c.notifyListeners();
      await tester.pumpAndSettle();
      expect(position.extentBefore, lessThan(1));
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'running desktop chat offers fork-and-send without a misleading stop button',
    (tester) async {
      final c = OperableClient();
      addTearDown(c.dispose);
      c.sessions[0] = AgentSession(
        id: 'a',
        agent: 'codex',
        title: '电脑执行中的会话',
        source: 'log',
        status: 'running',
        project: '/project-a',
      );
      await c.watch('a');
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: ConversationPage(client: c, session: c.sessions.first),
        ),
      );
      await tester.pump();
      expect(find.byType(RunningIndicator), findsWidgets);
      expect(find.byTooltip('停止任务'), findsNothing);
      expect(find.byTooltip('发送消息'), findsOneWidget);
      await tester.enterText(find.byType(TextField), '在手机继续');
      await tester.pump();
      await tester.tap(find.byTooltip('发送消息'));
      await tester.pumpAndSettle();
      expect(c.forkedFrom, 'a');
      expect(c.sent, 'child:在手机继续');
      expect(find.text('真实协议回复位置'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets('onboarding opens pairing and has an honest empty state', (
    tester,
  ) async {
    final c = AgentClient();
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
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.text('从连接开始'),
      200,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('从连接开始'), findsOneWidget);
    await tester.ensureVisible(find.text('连接工作站'));
    await tester.tap(find.text('连接工作站'));
    await tester.pumpAndSettle();
    expect(find.text('安全连接'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  testWidgets('mobile layout, conversation navigation and collapsible output', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    if (Platform.environment['AGENTDECK_SCREENSHOTS'] == '1') {
      await tester.runAsync(() async {
        final fonts = {
          'PingFang SC': '/System/Library/Fonts/Supplemental/Arial Unicode.ttf',
          'monospace': '/System/Library/Fonts/Menlo.ttc',
          'MaterialIcons':
              '/Users/tt/flutter/bin/cache/artifacts/material_fonts/MaterialIcons-Regular.otf',
        };
        for (final entry in fonts.entries) {
          final fontFile = File(entry.value);
          if (await fontFile.exists()) {
            final bytes = await fontFile.readAsBytes();
            final loader = FontLoader(entry.key)
              ..addFont(Future.value(ByteData.sublistView(bytes)));
            await loader.load();
          }
        }
      });
    }
    final c = presentationFixture();
    addTearDown(c.dispose);
    final key = GlobalKey();
    await tester.pumpWidget(
      RepaintBoundary(
        key: key,
        child: AgentDeckApp(
          client: c,
          languages: LanguageController(
            c.vault,
            initial: AppLanguage.simplifiedChinese,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('项目'), findsOneWidget);
    expect(find.text('最新会话'), findsOneWidget);
    expect(find.text('浏览界面示例'), findsNothing);
    expect(tester.takeException(), isNull);
    if (Platform.environment['AGENTDECK_SCREENSHOTS'] == '1') {
      await tester.runAsync(() => screenshot(key, '../artifacts/home.png'));
      final press = await tester.startGesture(
        tester.getCenter(find.text('最新会话')),
      );
      await tester.pump(const Duration(milliseconds: 100));
      await tester.runAsync(
        () => screenshot(key, '../artifacts/home-pressed.png'),
      );
      await press.up();
      await tester.pumpAndSettle();
      await tester.runAsync(
        () => screenshot(key, '../artifacts/home-recent.png'),
      );
      await tester.tap(find.text('项目'));
      await tester.pumpAndSettle();
    }
    await tester.ensureVisible(find.text('构建一个更好的移动会话体验'));
    await tester.tap(find.text('构建一个更好的移动会话体验'));
    await tester.pumpAndSettle();
    expect(find.text('思考摘要'), findsOneWidget);
    await tester.ensureVisible(find.text('工具输出'));
    await tester.tap(find.text('工具输出'));
    await tester.pumpAndSettle();
    expect(find.textContaining('agentdeck/internal/server'), findsOneWidget);
    expect(tester.takeException(), isNull);
    if (Platform.environment['AGENTDECK_SCREENSHOTS'] == '1') {
      await tester.runAsync(
        () => screenshot(key, '../artifacts/conversation.png'),
      );
    }
  });
  testWidgets('encrypted image decodes and opens a zoomable view', (
    tester,
  ) async {
    final c = AgentClient();
    addTearDown(c.dispose);
    final bytes = await tester.runAsync(
      () => File('test/fixtures/output.png').readAsBytes(),
    );
    c.images['fixture'] = bytes!;
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: UiStrings.delegates,
        supportedLocales: UiStrings.supportedLocales,
        locale: AppLanguage.simplifiedChinese.locale,
        home: Scaffold(
          body: EncryptedImage(client: c, id: 'fixture'),
        ),
      ),
    );
    await tester.runAsync(
      () => precacheImage(
        MemoryImage(bytes),
        tester.element(find.byType(EncryptedImage)),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byType(Image), findsOneWidget);
    expect(find.text('图片无法解码'), findsNothing);
    await tester.tap(find.byType(Image));
    await tester.pumpAndSettle();
    expect(find.byType(InteractiveViewer), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  testWidgets(
    'gateway login rejects plaintext and fits small accessible screens',
    (tester) async {
      tester.view.physicalSize = const Size(320, 740);
      tester.view.devicePixelRatio = 1;
      tester.platformDispatcher.textScaleFactorTestValue = 1.6;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
      final c = AgentClient(vault: EmptyGatewayVault());
      addTearDown(c.dispose);
      final key = GlobalKey();
      await tester.pumpWidget(
        RepaintBoundary(
          key: key,
          child: AgentDeckApp(
            client: c,
            languages: LanguageController(
              c.vault,
              initial: AppLanguage.simplifiedChinese,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('配对后端'));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('通过网关 · 飞书登录'));
      await tester.tap(find.text('通过网关 · 飞书登录'));
      await tester.pumpAndSettle();
      expect(find.text('用飞书登录'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.enterText(
        find.byType(TextField).last,
        'http://agent.example.com',
      );
      await tester.ensureVisible(find.text('用飞书登录'));
      await tester.tap(find.text('用飞书登录'));
      await tester.pumpAndSettle();
      expect(find.textContaining('HTTPS'), findsWidgets);
      expect(tester.takeException(), isNull);
      if (Platform.environment['AGENTDECK_SCREENSHOTS'] == '1') {
        await tester.runAsync(loadScreenshotFonts);
        tester.view.physicalSize = const Size(390, 844);
        tester.platformDispatcher.textScaleFactorTestValue = 1;
        await tester.tap(find.byType(BackButton));
        await tester.pumpAndSettle();
        await tester.tap(find.text('通过网关 · 飞书登录'));
        await tester.pumpAndSettle();
        await tester.enterText(
          find.byType(TextField).last,
          'https://agent.example.com',
        );
        await tester.pumpAndSettle();
        await tester.runAsync(
          () => screenshot(key, '../artifacts/gateway-login.png'),
        );
      }
    },
  );
  testWidgets('320px screen, 2x text and keyboard do not overflow', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 740);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    tester.platformDispatcher.textScaleFactorTestValue = 2;
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
    final c = presentationFixture();
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
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    await tester.tap(find.text('最新会话'));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    await tester.tap(find.text('构建一个更好的移动会话体验'));
    await tester.pumpAndSettle();
    expect(find.byTooltip('发送消息'), findsOneWidget);
    addTearDown(tester.view.resetViewInsets);
    tester.view.viewInsets = const FakeViewPadding(bottom: 280);
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
  });
  testWidgets('six-agent home can hide all and recover through settings', (
    tester,
  ) async {
    final c = OperableClient();
    addTearDown(c.dispose);
    c.agentVisibility = AgentVisibility(hidden: agentNames.keys);
    await tester.pumpWidget(
      AgentDeckApp(
        client: c,
        languages: LanguageController(
          c.vault,
          initial: AppLanguage.simplifiedChinese,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('请选择要显示的 Agent'), findsOneWidget);
    await tester.tap(find.byTooltip('设置'));
    await tester.pumpAndSettle();
    expect(find.text('选择首页显示的 Agent'), findsOneWidget);
    c.agentVisibility = AgentVisibility(hidden: ['qoder']);
    c.notifyListeners();
    await tester.tap(find.byType(BackButton));
    await tester.pumpAndSettle();
    expect(find.widgetWithText(ChoiceChip, 'Codex'), findsOneWidget);
    expect(find.widgetWithText(ChoiceChip, 'Qoder'), findsNothing);
    expect(find.text('项目 A 会话'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  testWidgets('OpenCode has its own projects and create selection', (
    tester,
  ) async {
    final c = OperableClient();
    addTearDown(c.dispose);
    c.agents = [
      ...defaultAgents().where((a) => a.id != 'opencode'),
      const AgentInfo(id: 'opencode', name: 'OpenCode', configured: true),
    ];
    c.sessions.add(
      AgentSession(
        id: 'native-open',
        agent: 'opencode',
        title: 'OpenCode original',
        project: '/project-b',
        source: 'opencode-api',
        status: 'idle',
        managed: true,
      ),
    );
    await tester.pumpWidget(
      AgentDeckApp(
        client: c,
        languages: LanguageController(
          c.vault,
          initial: AppLanguage.simplifiedChinese,
        ),
      ),
    );
    await tester.pumpAndSettle();
    final chip = find.widgetWithText(ChoiceChip, 'OpenCode');
    await tester.ensureVisible(chip);
    await tester.tap(chip);
    await tester.pumpAndSettle();
    expect(find.text('project-b'), findsOneWidget);
    expect(find.text('project-a'), findsNothing);
    expect(find.text('OpenCode original'), findsOneWidget);
    await tester.tap(find.byTooltip('新建会话'));
    await tester.pumpAndSettle();
    final dropdowns = tester
        .widgetList<DropdownButtonFormField<String>>(
          find.byType(DropdownButtonFormField<String>),
        )
        .toList();
    expect(dropdowns.any((d) => d.initialValue == 'opencode'), isTrue);
    expect(tester.takeException(), isNull);
  });
}

Future<void> screenshot(GlobalKey key, String path) async {
  final boundary =
      key.currentContext!.findRenderObject() as RenderRepaintBoundary;
  final image = await boundary.toImage(pixelRatio: 2);
  final data = await image.toByteData(format: ui.ImageByteFormat.png);
  await File(path).writeAsBytes(data!.buffer.asUint8List());
  image.dispose();
}

Future<void> loadScreenshotFonts() async {
  final fonts = {
    'PingFang SC': '/System/Library/Fonts/Supplemental/Arial Unicode.ttf',
    'monospace': '/System/Library/Fonts/Menlo.ttc',
    'MaterialIcons':
        '/Users/tt/flutter/bin/cache/artifacts/material_fonts/MaterialIcons-Regular.otf',
  };
  for (final entry in fonts.entries) {
    final f = File(entry.value);
    if (await f.exists()) {
      final bytes = await f.readAsBytes();
      await (FontLoader(
        entry.key,
      )..addFont(Future.value(ByteData.sublistView(bytes)))).load();
    }
  }
}

AgentClient presentationFixture() {
  final c = AgentClient()..showDemo();
  c.sessions = c.sessions
      .map(
        (s) => AgentSession(
          id: s.id,
          agent: s.agent,
          title: s.title,
          source: s.source,
          status: 'idle',
          project: s.agent == 'codex'
              ? '/workspace/agentdeck'
              : '/workspace/workbench',
          updated: s.agent == 'codex'
              ? '2026-10-08T02:00:00Z'
              : '2026-10-08T01:00:00Z',
        ),
      )
      .toList();
  return c;
}
