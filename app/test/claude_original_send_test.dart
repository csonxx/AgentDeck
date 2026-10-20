import 'dart:async';
import 'outbox_fixtures.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class OriginalClaudeClient extends AgentClient {
  final commands = <Map<String, dynamic>>[];
  OriginalClaudeClient() : super(vault: MemoryOutgoingVault()) {
    health.transition(ConnectionPhase.live);
    projects = [AgentProject(id: 'p', name: 'fixture', path: '/fixture')];
    sessions = [
      AgentSession(
        id: 'original',
        agent: 'claude',
        title: 'Original Claude',
        source: 'claude-desktop',
        status: 'idle',
        project: '/fixture',
      ),
    ];
    transcripts['original'] = Transcript();
  }
  @override
  Future<void> watch(String id) async {
    selected = id;
  }

  @override
  Future<void> unwatch(String id) async {}
  @override
  Future<dynamic> request(
    Map<String, dynamic> data, {
    Duration timeout = const Duration(seconds: 70),
  }) async {
    commands.add(data);
    switch (data['method']) {
      case 'create':
        sessions = [
          AgentSession(
            id: 'original',
            agent: 'claude',
            title: 'Original Claude',
            source: 'claude-desktop',
            managed: true,
            status: 'idle',
            project: '/fixture',
          ),
        ];
        return {
          'id': 'original',
          'agent': 'claude',
          'title': 'Original Claude',
          'source': 'claude-desktop',
          'managed': true,
          'status': 'idle',
          'project': '/fixture',
        };
      case 'send':
        queues['original'] = [
          QueuedMessage(
            id: data['id'] as String,
            text: data['text'] as String,
            state: 'queued',
            origin: 'local',
            version: 'v',
          ),
        ];
        return {'delivery': 'queued'};
      case 'queue_list':
        return queues['original']
                ?.map(
                  (q) => {
                    'id': q.id,
                    'text': q.text,
                    'state': q.state,
                    'origin': q.origin,
                    'version': q.version,
                  },
                )
                .toList() ??
            [];
    }
    return null;
  }
}

class ReplyBeforeAcknowledgement extends OriginalClaudeClient {
  final acknowledgement = Completer<dynamic>();
  @override
  Future<dynamic> request(
    Map<String, dynamic> data, {
    Duration timeout = const Duration(seconds: 70),
  }) {
    if (data['method'] == 'send') {
      commands.add(data);
      return acknowledgement.future;
    }
    return super.request(data, timeout: timeout);
  }
}

void main() {
  testWidgets(
    'native reply before RPC acknowledgement still clears its draft',
    (tester) async {
      final c = ReplyBeforeAcknowledgement();
      addTearDown(c.dispose);
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: ConversationPage(client: c, session: c.sessions.single),
        ),
      );
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), 'original human input');
      await tester.pump();
      await tester.tap(find.byTooltip('发送消息'));
      await tester.pump();
      final send = c.commands.singleWhere((r) => r['method'] == 'send');
      final transcript = c.transcripts['original']!;
      for (final record in [
        (1, 'user', 'original human input'),
        (2, 'assistant', 'native reply'),
      ]) {
        transcript.add({
          'id': 'native-${record.$1}',
          'seq': record.$1,
          'item': record.$2 == 'user' ? 'native-user-uuid' : 'native-reply',
          'kind': 'message',
          'role': record.$2,
          'mode': 'snapshot',
          'text': record.$3,
        });
      }
      c.notifyListeners();
      await tester.pump();
      c.acknowledgement.complete({
        'delivery': 'submitted',
        'message_id': 'native-user-uuid',
      });
      await tester.pumpAndSettle();
      expect(send['session'], 'original');
      expect(
        tester.widget<TextField>(find.byType(TextField)).controller!.text,
        isEmpty,
      );
      expect(find.text('native reply'), findsOneWidget);
      expect(find.byType(SnackBar), findsNothing);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'App sends to original Claude ID and shows queued acknowledgement without an optimistic execution bubble',
    (tester) async {
      final c = OriginalClaudeClient();
      addTearDown(c.dispose);
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: ConversationPage(client: c, session: c.sessions.single),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('继续 Claude 原会话 · 不创建分支'), findsOneWidget);
      await tester.enterText(find.byType(TextField), 'queued original input');
      await tester.pump();
      await tester.tap(find.byTooltip('发送消息'));
      await tester.pumpAndSettle();
      final create = c.commands.singleWhere((r) => r['method'] == 'create');
      final send = c.commands.singleWhere((r) => r['method'] == 'send');
      expect(create['session'], 'original');
      expect(send['session'], 'original');
      expect(c.sessions.length, 1);
      expect(find.byKey(const ValueKey('message-queue')), findsOneWidget);
      expect(find.text('queued original input'), findsOneWidget);
      expect(find.text('已送达，等待 Agent 响应…'), findsNothing);
      expect(find.text('未确认送达，请先核对会话，避免重复发送'), findsNothing);
      expect(
        tester.widget<TextField>(find.byType(TextField)).controller!.text,
        isEmpty,
      );
      expect(tester.takeException(), isNull);
    },
  );
}
