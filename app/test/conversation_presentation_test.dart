import 'dart:async';
import 'package:agentdeck/core/conversation_presentation.dart';
import 'package:agentdeck/core/input.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/main.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/reading_scroll_view.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'widget_test.dart' show OperableClient;

class SlowDeliveryClient extends OperableClient {
  Completer<void> ack = Completer<void>();
  String requestId = '';
  int sends = 0;
  @override
  Future<void> sendDelivery(
    String session,
    String text,
    TurnOptions options,
    List<String> files,
    String delivery, {
    String? requestId,
  }) {
    sends++;
    this.requestId = requestId!;
    return ack.future;
  }

  @override
  Future<List<QueuedMessage>> queue(String session) async => [];
  void event(
    String kind,
    String text,
    int seq, {
    String role = '',
    String? item,
  }) {
    transcripts['a']!.add({
      'id': 'e$seq',
      'seq': seq,
      'first': seq,
      'kind': kind,
      'text': text,
      'role': role,
      'item': item ?? 'item$seq',
      'mode': 'snapshot',
    });
    notifyListeners();
  }
}

Widget conversation(SlowDeliveryClient c) => MaterialApp(
  localizationsDelegates: UiStrings.delegates,
  supportedLocales: UiStrings.supportedLocales,
  locale: AppLanguage.simplifiedChinese.locale,
  home: ConversationPage(client: c, session: c.sessions.first),
);

void main() {
  testWidgets(
    'definite pre-write rejection preserves only the editable draft',
    (tester) async {
      final c = SlowDeliveryClient();
      addTearDown(c.dispose);
      await c.watch('a');
      await tester.pumpWidget(conversation(c));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), 'not submitted');
      await tester.pump();
      await tester.tap(find.byTooltip('发送消息'));
      await tester.pump();
      c.ack.completeError(
        AgentRequestException('bridge unavailable', notSent: true),
      );
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(
        tester.widget<TextField>(find.byType(TextField)).controller!.text,
        'not submitted',
      );
      expect(find.text('not submitted'), findsOneWidget);
      expect(find.text('未确认送达，请先核对会话，避免重复发送'), findsNothing);
      expect(find.byType(RunningIndicator), findsNothing);
      expect(c.sends, 1);
    },
  );
  test(
    'activity sections preserve order and never hide approvals or media',
    () {
      final blocks = [
        TranscriptBlock(key: 'u', kind: 'message', role: 'user'),
        TranscriptBlock(key: 't', kind: 'thinking'),
        TranscriptBlock(key: 'c', kind: 'tool_call'),
        TranscriptBlock(key: 'r', kind: 'tool_result'),
        TranscriptBlock(key: 'a', kind: 'approval'),
        TranscriptBlock(key: 'f', kind: 'file_change'),
        TranscriptBlock(key: 'i', kind: 'image'),
        TranscriptBlock(key: 'm', kind: 'message', role: 'assistant'),
      ];
      final sections = transcriptSections(blocks);
      expect(sections.map((s) => s.blocks.length), [1, 3, 1, 1, 1, 1]);
      expect(sections.expand((s) => s.blocks), blocks);
      expect(sections.where((s) => s.activity).length, 2);
      expect(sections[2].activity, isFalse);
      expect(sections[4].activity, isFalse);
    },
  );

  test(
    'native submitted input ignores previous turn activity until its own UUID',
    () {
      final r = InputReceipt(id: 'new-user', text: 'next', cursor: 10)
        ..phase = InputDeliveryPhase.submitted;
      final oldTool = TranscriptBlock(key: 'old-tool', kind: 'tool_call')
        ..first = 11
        ..seq = 11;
      r.reconcile([oldTool]);
      expect(r.waiting([oldTool]), isTrue);
      expect(r.responded([oldTool]), isFalse);
      final own =
          TranscriptBlock(
              key: 'new-user:0:message',
              kind: 'message',
              role: 'user',
              text: 'next',
            )
            ..first = 12
            ..seq = 12;
      expect(r.waiting([oldTool, own]), isTrue);
      final reply =
          TranscriptBlock(key: 'reply', kind: 'message', role: 'assistant')
            ..first = 13
            ..seq = 13;
      r.reconcile([oldTool, own, reply]);
      expect(r.waiting([]), isFalse);
    },
  );
  test(
    'submitted input matches the confirmed native UUID rather than RPC ID',
    () {
      final r = InputReceipt(id: 'rpc-id', text: 'next', cursor: 10)
        ..phase = InputDeliveryPhase.submitted
        ..nativeMessageID = 'native-user-uuid';
      final own = TranscriptBlock(
        key: 'native-user-uuid:message',
        kind: 'message',
        role: 'user',
        text: 'next',
      )..first = 12;
      final previous = TranscriptBlock(
        key: 'previous:message',
        kind: 'message',
        role: 'assistant',
      )..first = 11;
      expect(r.responded([previous, own]), isFalse);
      final reply = TranscriptBlock(
        key: 'reply:message',
        kind: 'message',
        role: 'assistant',
      )..first = 13;
      expect(r.responded([previous, own, reply]), isTrue);
    },
  );

  testWidgets('native submitted input keeps waiting while previous turn runs', (
    tester,
  ) async {
    final c = SlowDeliveryClient();
    addTearDown(c.dispose);
    await c.watch('a');
    await tester.pumpWidget(conversation(c));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'append during work');
    await tester.pump();
    await tester.tap(find.byTooltip('发送消息'));
    await tester.pump();
    c.submittedInputs.add(c.requestId);
    c.ack.complete();
    await tester.pump();
    c.event('tool_call', 'existing turn tool', 11);
    await tester.pump();
    expect(find.text('已提交到电脑 Claude，等待原会话处理…'), findsOneWidget);
    expect(find.text('append during work'), findsOneWidget);
    expect(find.byType(TextField), findsOneWidget);
    expect(find.text('未确认送达，请先核对会话，避免重复发送'), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets(
    'consecutive busy inputs retain every submitted bubble until native history arrives',
    (tester) async {
      final c = SlowDeliveryClient();
      addTearDown(c.dispose);
      c.sessions[0] = AgentSession(
        id: 'a',
        agent: 'claude',
        title: 'Claude transport fixture',
        updated: '',
        project: '/project',
        source: 'claude-desktop',
        managed: true,
        status: 'idle',
      );
      await c.watch('a');
      await tester.pumpWidget(conversation(c));
      await tester.pumpAndSettle();
      for (final text in ['first pending input', 'second pending input']) {
        c.ack = Completer<void>();
        await tester.enterText(find.byType(TextField), text);
        await tester.pump();
        await tester.tap(
          find.byTooltip(text == 'first pending input' ? '发送消息' : '排队发送'),
        );
        await tester.pump();
        c.submittedInputs.add(c.requestId);
        c.ack.complete();
        await tester.pump();
      }
      expect(c.sends, 2);
      expect(find.text('first pending input'), findsOneWidget);
      expect(find.text('second pending input'), findsOneWidget);
      expect(find.text('已提交到电脑 Claude，等待原会话处理…'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    },
  );

  test('receipt rejects old identical messages and old streaming deltas', () {
    final r = InputReceipt(id: 'request', text: 'same', cursor: 10);
    final old =
        TranscriptBlock(
            key: 'old:message',
            kind: 'message',
            role: 'user',
            text: 'same',
          )
          ..seq = 9
          ..first = 9;
    final delta =
        TranscriptBlock(
            key: 'assistant:message',
            kind: 'message',
            role: 'assistant',
          )
          ..seq = 12
          ..first = 8;
    expect(r.observed([old]), isFalse);
    old.seq = 15;
    expect(r.observed([old]), isFalse);
    expect(r.responded([delta]), isFalse);
    r.phase = InputDeliveryPhase.uncertain;
    expect(r.waiting([old]), isFalse);
    final native =
        TranscriptBlock(
            key: 'request:0:message',
            kind: 'message',
            role: 'user',
            text: 'redacted',
          )
          ..seq = 13
          ..first = 13;
    expect(r.observed([native]), isTrue);
    expect(r.waiting([native]), isTrue);
  });

  test(
    'terminal native status ends waiting and response remains observed across paging',
    () {
      final r = InputReceipt(
        id: 'request',
        text: 'same',
        cursor: 10,
        baselineUpdated: 'before',
      )..phase = InputDeliveryPhase.accepted;
      final user =
          TranscriptBlock(
              key: 'request:0:message',
              kind: 'message',
              role: 'user',
              text: 'same',
            )
            ..first = 11
            ..seq = 11;
      expect(r.waiting([user], status: 'completed', updated: 'before'), isTrue);
      expect(r.waiting([user], status: 'completed', updated: 'after'), isFalse);
      final answer =
          TranscriptBlock(
              key: 'reply:message',
              kind: 'message',
              role: 'assistant',
            )
            ..first = 12
            ..seq = 12;
      r.reconcile([user, answer]);
      expect(r.waiting([]), isFalse);
    },
  );

  testWidgets(
    'sending, acknowledgement, delayed history and execution never lose the input',
    (tester) async {
      final c = SlowDeliveryClient();
      addTearDown(c.dispose);
      await c.watch('a');
      await tester.pumpWidget(conversation(c));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), 'confirmed input');
      await tester.pump();
      await tester.tap(find.byTooltip('发送消息'));
      await tester.pump();
      expect(find.text('confirmed input'), findsWidgets);
      expect(find.text('发送中'), findsOneWidget);
      expect(c.sends, 1);
      c.ack.complete();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.text('confirmed input'), findsOneWidget);
      expect(find.text('已送达，等待 Agent 响应…'), findsOneWidget);
      expect(find.byType(SnackBar), findsNothing);
      c.event(
        'message',
        'confirmed input',
        1,
        role: 'user',
        item: '${c.requestId}:0',
      );
      await tester.pump();
      expect(find.text('confirmed input'), findsOneWidget);
      // A stale history replacement temporarily omits the accepted native item.
      c.transcripts['a']!.history({
        'events': [],
        'cursor': 1,
        'before': 0,
        'has_more': false,
      }, replace: true);
      c.notifyListeners();
      await tester.pump();
      expect(find.text('confirmed input'), findsOneWidget);
      expect(find.text('已送达，等待 Agent 响应…'), findsOneWidget);
      c.event(
        'message',
        'confirmed input',
        2,
        role: 'user',
        item: '${c.requestId}:0',
      );
      c.event('message', 'native answer', 3, role: 'assistant');
      await tester.pumpAndSettle();
      expect(find.text('confirmed input'), findsOneWidget);
      expect(find.text('native answer'), findsOneWidget);
      expect(find.text('已送达，等待 Agent 响应…'), findsNothing);
      expect(find.byType(RunningIndicator), findsNothing);
      expect(c.sends, 1);
    },
  );

  testWidgets(
    'uncertain delivery preserves draft and input without automatic retry',
    (tester) async {
      final c = SlowDeliveryClient();
      addTearDown(c.dispose);
      await c.watch('a');
      await tester.pumpWidget(conversation(c));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), 'uncertain input');
      await tester.pump();
      await tester.tap(find.byTooltip('发送消息'));
      await tester.pump();
      c.ack.completeError(TimeoutException('test acknowledgement timeout'));
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(
        tester.widget<TextField>(find.byType(TextField)).controller!.text,
        'uncertain input',
      );
      expect(find.text('未确认送达，请先核对会话，避免重复发送'), findsOneWidget);
      expect(c.sends, 1);
      c.event(
        'message',
        'uncertain input',
        1,
        role: 'user',
        item: '${c.requestId}:0',
      );
      await tester.pump();
      expect(find.text('未确认送达，请先核对会话，避免重复发送'), findsNothing);
      expect(find.text('已送达，等待 Agent 响应…'), findsOneWidget);
      expect(c.sends, 1);
    },
  );

  testWidgets('one control expands and collapses all activity groups', (
    tester,
  ) async {
    final c = SlowDeliveryClient();
    addTearDown(c.dispose);
    await c.watch('a');
    c.event('tool_call', 'Read\nsynthetic argument', 1);
    c.event('tool_result', 'large synthetic output', 2);
    c.event('message', 'visible answer', 3, role: 'assistant');
    c.event('file_change', 'synthetic diff', 4);
    await tester.pumpWidget(conversation(c));
    await tester.pumpAndSettle();
    expect(find.text('执行过程 · 2 项'), findsOneWidget);
    expect(find.text('执行过程 · 1 项'), findsOneWidget);
    expect(find.byType(ExpansionTile), findsNothing);
    expect(find.text('visible answer'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('all-activities-toggle')));
    await tester.pumpAndSettle();
    // Expanded rows are lazy and need not all fit beside message actions.
    // Verify all three become expanded as they enter the viewport.
    final expanded = <String>{};
    for (var i = 0; i < 4; i++) {
      for (final tile in tester.widgetList<TranscriptTile>(
        find.byType(TranscriptTile),
      )) {
        if (isActivity(tile.block)) expanded.add(tile.block.key);
      }
      if (expanded.length == 3) break;
      await tester.drag(find.byType(ReadingScrollView), const Offset(0, 400));
      await tester.pumpAndSettle();
    }
    expect(expanded, {
      'item1:tool_call',
      'item2:tool_result',
      'item4:file_change',
    });
    expect(find.byType(ExpansionTile), findsWidgets);
    await tester.tap(find.byKey(const ValueKey('all-activities-toggle')));
    await tester.pumpAndSettle();
    expect(find.byType(ExpansionTile), findsNothing);
    await tester.drag(find.byType(ReadingScrollView), const Offset(0, -800));
    await tester.pumpAndSettle();
    expect(find.text('visible answer'), findsOneWidget);
  });
}
