import 'dart:async';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/core/conversation_presentation.dart';
import 'package:agentdeck/main.dart';
import 'package:agentdeck/activity_group.dart';
import 'package:agentdeck/language.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class DetailClient extends AgentClient {
  int calls = 0;
  Completer<Map<String, dynamic>>? pending;
  bool fail = false;
  @override
  Future<Map<String, dynamic>> executionDetail(
    String session,
    TranscriptBlock b,
  ) async {
    calls++;
    if (fail) throw StateError('fixture offline');
    return pending?.future ??
        Future.value({
          'session': session,
          'item': 'call',
          'kind': b.kind,
          'seq': b.seq,
          'text': 'FULL_EXECUTION_$calls',
        });
  }
}

Widget view(DetailClient c, TranscriptBlock b) => MaterialApp(
  localizationsDelegates: UiStrings.delegates,
  supportedLocales: UiStrings.supportedLocales,
  locale: AppLanguage.simplifiedChinese.locale,
  home: Scaffold(
    body: SingleChildScrollView(
      child: TranscriptTile(block: b, client: c, session: 'a'),
    ),
  ),
);
void main() {
  testWidgets(
    'expanding the group lists summaries without fetching any bodies',
    (tester) async {
      final c = DetailClient();
      addTearDown(c.dispose);
      final blocks = [
        TranscriptBlock(key: 'call:tool_call', kind: 'tool_call', text: 'shell')
          ..seq = 1
          ..detail = true,
        TranscriptBlock(key: 'result:tool_result', kind: 'tool_result')
          ..seq = 2
          ..detail = true,
      ];
      var expanded = false;
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: Scaffold(
            body: StatefulBuilder(
              builder: (context, setState) => ActivityGroup(
                blocks: blocks,
                expanded: expanded,
                onToggle: () => setState(() => expanded = !expanded),
                tile: (b) => TranscriptTile(block: b, client: c, session: 'a'),
              ),
            ),
          ),
        ),
      );
      expect(c.calls, 0);
      await tester.tap(find.text('执行过程 · 2 项'));
      await tester.pumpAndSettle();
      expect(c.calls, 0);
      expect(find.text('工具调用 · shell'), findsOneWidget);
      expect(find.text('工具输出'), findsOneWidget);
      await tester.tap(find.text('工具调用 · shell'));
      await tester.pumpAndSettle();
      expect(c.calls, 1);
      expect(find.text('FULL_EXECUTION_1'), findsOneWidget);
    },
  );
  test(
    'question stays after source; paging remains in canonical coordinates',
    () {
      final t = Transcript();
      t.history({
        'cursor': 50,
        'before': 10,
        'has_more': true,
        'events': [
          {
            'id': 'a',
            'item': 'ask',
            'seq': 10,
            'first': 10,
            'kind': 'message',
            'role': 'assistant',
            'text': 'question',
          },
          {
            'id': 'b',
            'item': 'later',
            'seq': 20,
            'first': 20,
            'kind': 'message',
            'role': 'assistant',
            'text': 'later',
          },
          {
            'id': 'c',
            'item': 'card',
            'seq': 50,
            'first': 50,
            'position': 10,
            'kind': 'approval',
            'text': 'choices',
          },
        ],
      }, replace: true);
      expect(transcriptSections(t.blocks).map((s) => s.blocks.single.key), [
        'ask:message',
        'card:approval',
        'later:message',
      ]);
      expect(t.blocks.last.first, 50);
      expect(t.before, 10);
      t.add({
        'id': 'resolve',
        'item': 'card',
        'seq': 60,
        'position': 10,
        'kind': 'approval',
        'text': 'choices',
        'status': 'resolved',
      });
      expect(transcriptSections(t.blocks).map((s) => s.blocks.single.key), [
        'ask:message',
        'card:approval',
        'later:message',
      ]);
    },
  );
  testWidgets(
    'collapsed execution never fetches; explicit open loads and displays progress',
    (tester) async {
      final c = DetailClient();
      addTearDown(c.dispose);
      final b =
          TranscriptBlock(
              key: 'call:tool_call',
              kind: 'tool_call',
              text: 'shell',
            )
            ..seq = 1
            ..first = 1
            ..detail = true;
      await tester.pumpWidget(view(c, b));
      await tester.pump(const Duration(seconds: 1));
      expect(c.calls, 0);
      c.pending = Completer();
      await tester.tap(find.text('工具调用 · shell'));
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pump();
      expect(c.calls, 1);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
      c.pending!.complete({
        'session': 'a',
        'item': 'call',
        'kind': 'tool_call',
        'seq': 1,
        'text': 'EXPLICIT_FULL_BODY',
      });
      await tester.pumpAndSettle();
      expect(find.text('EXPLICIT_FULL_BODY'), findsOneWidget);
      await tester.tap(find.text('工具调用 · shell'));
      await tester.pumpAndSettle();
      b.seq = 2;
      await tester.pumpWidget(view(c, b));
      await tester.pump(const Duration(seconds: 1));
      expect(c.calls, 1);
      expect(find.text('EXPLICIT_FULL_BODY'), findsNothing);
      c.pending = null;
      await tester.tap(find.text('工具调用 · shell'));
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pumpAndSettle();
      expect(c.calls, 2);
      expect(find.text('FULL_EXECUTION_2'), findsOneWidget);
    },
  );
  testWidgets('failed detail retries; closing ignores late results', (
    tester,
  ) async {
    final c = DetailClient()..fail = true;
    addTearDown(c.dispose);
    final b = TranscriptBlock(key: 'call:tool_result', kind: 'tool_result')
      ..seq = 1
      ..detail = true;
    await tester.pumpWidget(view(c, b));
    await tester.tap(find.text('工具输出'));
    await tester.pump(const Duration(milliseconds: 200));
    await tester.pumpAndSettle();
    expect(find.text('执行详情加载失败，请重试'), findsOneWidget);
    c.fail = false;
    c.pending = Completer();
    await tester.tap(find.text('重试'));
    await tester.pump();
    expect(c.calls, 2);
    await tester.tap(find.text('工具输出'));
    await tester.pumpAndSettle();
    c.pending!.complete({
      'session': 'a',
      'item': 'call',
      'kind': 'tool_result',
      'seq': 1,
      'text': 'LATE_BODY',
    });
    await tester.pumpAndSettle();
    expect(find.text('LATE_BODY'), findsNothing);
    expect(c.calls, 2);
  });
}
