import 'package:agentdeck/language.dart';
import 'dart:async';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/main.dart';
import 'package:agentdeck/input_composer.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class DesktopClient extends AgentClient {
  final adoption = Completer<AgentSession>();
  final ack = Completer<void>();
  String? parent, sent;
  DesktopClient() {
    health.transition(ConnectionPhase.live);
    codexDesktop = true;
    projects = [AgentProject(id: 'p', name: 'fixture', path: '/fixture')];
    sessions = [
      AgentSession(
        id: 'original',
        agent: 'codex',
        title: 'Desktop original',
        source: 'log',
        status: 'idle',
        project: '/fixture',
      ),
    ];
    selected = 'original';
    transcripts['original'] = Transcript();
  }
  @override
  Future<AgentSession> createSession(
    String agent,
    String project, {
    String parent = '',
    String permissionMode = '',
  }) {
    this.parent = parent;
    return adoption.future;
  }

  @override
  Future<void> sendPrompt(String session, String text, {String? requestId}) {
    sent = session;
    return ack.future;
  }

  void status(String value) {
    sessions = [
      AgentSession(
        id: 'original',
        agent: 'codex',
        title: 'Desktop original',
        source: 'desktop',
        managed: true,
        status: value,
        project: '/fixture',
      ),
    ];
    notifyListeners();
  }

  @override
  Future<void> watch(String id) async {
    selected = id;
  }

  @override
  Future<void> unwatch(String id) async {}
  @override
  Future<List<QueuedMessage>> queue(String id) async => [];
}

void main() {
  test(
    'queue state updates materialize once and retain original queue identity',
    () {
      final t = Transcript();
      for (final entry in [(1, 'queued'), (2, 'sent')]) {
        t.add({
          'id': 'event-${entry.$1}',
          'seq': entry.$1,
          'item': 'queue/request',
          'kind': 'queued_message',
          'role': entry.$2,
          'mode': 'snapshot',
          'text': 'Waiting input',
        });
      }
      expect(t.blocks.length, 1);
      expect(t.blocks.single.role, 'sent');
      expect(t.blocks.single.key, 'queue/request:queued_message');
      t.add({
        'id': 'late',
        'seq': 1,
        'item': 'queue/request',
        'kind': 'queued_message',
        'role': 'queued',
        'mode': 'snapshot',
        'text': 'Old',
      });
      expect(t.blocks.single.role, 'sent');
    },
  );

  testWidgets(
    'desktop send keeps original ID and explains each pending stage',
    (tester) async {
      final c = DesktopClient();
      addTearDown(c.dispose);
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: ConversationPage(client: c, session: c.sessions.first),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('发送到电脑 Codex 的同一个会话'), findsOneWidget);
      await tester.enterText(find.byType(TextField), 'fixture user prompt');
      await tester.pump();
      await tester.tap(find.byTooltip('发送消息'));
      await tester.pump();
      expect(find.text('正在连接电脑原会话…'), findsOneWidget);
      expect(c.parent, 'original');
      final adopted = AgentSession(
        id: 'original',
        agent: 'codex',
        title: 'Desktop original',
        source: 'desktop',
        status: 'running',
        managed: true,
        project: '/fixture',
      );
      c.sessions = [adopted];
      c.adoption.complete(adopted);
      await tester.pump();
      await tester.pump();
      expect(find.text('正在发送到电脑原会话…'), findsOneWidget);
      expect(c.sent, 'original');
      expect(find.byTooltip('停止任务'), findsNothing);
      c.ack.complete();
      await tester.pump();
      expect(find.text('正在发送到电脑原会话…'), findsNothing);
      expect(find.text('发送到电脑 Codex 的同一个会话'), findsOneWidget);
      expect(
        tester.widget<TextField>(find.byType(TextField)).controller!.text,
        isEmpty,
      );
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets(
    'project and recent lists refresh accepted, running and completed status',
    (tester) async {
      final c = DesktopClient();
      addTearDown(c.dispose);
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.english.locale,
          home: DeckHome(client: c),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byType(RunningIndicator), findsNothing);
      for (final status in ['pending', 'running']) {
        c.status(status);
        await tester.pump();
        expect(
          find.descendant(
            of: find.byKey(const ValueKey('session-original')),
            matching: find.byType(RunningIndicator),
          ),
          findsOneWidget,
        );
      }
      c.projectLayout = ProjectLayout().withScoped(
        'codex',
        ProjectLayout(collapsed: ['/fixture']),
      );
      c.status('pending');
      await tester.pump();
      expect(find.byKey(const ValueKey('session-original')), findsNothing);
      expect(find.byType(RunningIndicator), findsOneWidget);
      c.status('completed');
      await tester.pump();
      expect(find.byType(RunningIndicator), findsNothing);
      await tester.tap(find.text('Recent chats'));
      await tester.pump(const Duration(milliseconds: 300));
      c.status('running');
      await tester.pump();
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('session-original')),
          matching: find.byType(RunningIndicator),
        ),
        findsOneWidget,
      );
      c.status('completed');
      await tester.pump();
      expect(find.byType(RunningIndicator), findsNothing);
    },
  );
  testWidgets(
    'conversation shows accepted waiting and then execution without a tool',
    (tester) async {
      final c = DesktopClient();
      addTearDown(c.dispose);
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: ConversationPage(client: c, session: c.sessions.first),
        ),
      );
      await tester.pumpAndSettle();
      c.status('pending');
      await tester.pump();
      expect(find.text('已送达，等待 Agent 响应…'), findsWidgets);
      expect(find.byType(RunningIndicator), findsWidgets);
      c.status('running');
      await tester.pump();
      expect(find.text('执行中'), findsOneWidget);
      expect(find.text('已送达，等待 Agent 响应…'), findsNothing);
      c.status('completed');
      await tester.pump();
      expect(find.byType(RunningIndicator), findsNothing);
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets(
    'running Codex defaults to queue without pre-send steering and keeps failed drafts',
    (tester) async {
      final client = DesktopClient();
      addTearDown(client.dispose);
      final controller = TextEditingController(text: 'Follow up');
      addTearDown(controller.dispose);
      final modes = <String>[];
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.english.locale,
          home: Scaffold(
            body: InputComposer(
              client: client,
              session: 'original',
              agent: 'codex',
              controller: controller,
              sending: false,
              running: true,
              canSend: true,
              onSend: (o, f) async => false,
              onDelivery: (o, f, mode) async {
                modes.add(mode);
                return false;
              },
            ),
          ),
        ),
      );
      await tester.pump();
      await tester.tap(find.byTooltip('Queue message'));
      await tester.pump();
      expect(modes, ['queue']);
      expect(controller.text, 'Follow up');
      expect(find.text('Steer'), findsNothing);
      expect(find.byType(ChoiceChip), findsNothing);
      expect(tester.takeException(), isNull);
    },
  );
  test('prior mobile forks remain visibly distinct', () {
    final s = AgentSession.fromJson({
      'id': 'fork',
      'agent': 'codex',
      'title': 'Desktop original',
      'source': 'stream',
      'status': 'idle',
      'parent': 'original',
    });
    expect(s.displayTitle, 'Desktop original · 手机分支');
  });
}
