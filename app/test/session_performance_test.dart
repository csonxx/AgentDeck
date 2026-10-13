import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/main.dart';
import 'package:agentdeck/progressive_text.dart';
import 'conversation_presentation_test.dart'
    show SlowDeliveryClient, conversation;

Map<String, dynamic> event(int i, {String text = 'Synthetic', int? first}) => {
  'id': 'e$i',
  'item': 'm$i',
  'kind': 'message',
  'role': 'assistant',
  'seq': i,
  'first': first ?? i,
  'mode': 'snapshot',
  'text': text,
};
Widget app(Widget body) => LanguageScope(
  controller: LanguageController(DeviceVaultForPerformance()),
  child: MaterialApp(home: Scaffold(body: body)),
);

// Do not persist synthetic performance records.
class DeviceVaultForPerformance extends DeviceVault {
  @override
  Future<Map<String, dynamic>?> loadAppLock() async => null;
}

void main() {
  test(
    'forward paging restores evicted newer bodies and preserves live-race gaps',
    () {
      final t = Transcript();
      for (var i = 1; i <= 400; i++) {
        t.add(event(i));
      }
      t.history({
        'events': [for (var i = 161; i <= 200; i++) event(i)],
        'cursor': 400,
        'before': 161,
        'has_more': true,
      });
      expect(t.detached, true);
      expect(t.blocks.last.first, 360);
      t.history({
        'events': [for (var i = 361; i <= 400; i++) event(i)],
        'cursor': 400,
        'has_newer': false,
      }, newer: true);
      expect(t.blocks.length, 200);
      expect(t.blocks.first.first, 201);
      expect(t.blocks.last.first, 400);
      expect(t.detached, false);
      expect(t.hasMore, true);
      expect(t.before, 201);
      t.latestLiveCursor = 401;
      t.history({'events': [], 'cursor': 400, 'has_newer': false}, newer: true);
      expect(t.detached, true);
      t.history({
        'events': [event(401)],
        'cursor': 401,
        'has_newer': false,
      }, newer: true);
      expect(t.detached, false);
      expect(t.blocks.last.first, 401);
    },
  );
  test('live window bounds items and older pages keep their target window', () {
    final t = Transcript();
    for (var i = 1; i <= 1000; i++) {
      t.add(event(i));
    }
    expect(t.blocks.length, Transcript.maxBlocks);
    expect(t.blocks.first.first, 801);
    expect(t.hasMore, isTrue);
    expect(t.before, 801);
    t.history({
      'events': [for (var i = 761; i < 801; i++) event(i)],
      'cursor': 1000,
      'before': 761,
      'has_more': true,
    });
    expect(t.blocks.length, Transcript.maxBlocks);
    expect(t.blocks.first.first, 761);
    expect(t.detached, isTrue);
    t.history({
      'events': [for (var i = 961; i <= 1000; i++) event(i)],
      'cursor': 1000,
      'before': 961,
      'has_more': true,
    }, replace: true);
    expect(t.detached, isFalse);
    expect(t.blocks.last.first, 1000);
  });

  test(
    'a few huge records respect the text budget and updates are complete',
    () {
      final t = Transcript();
      final large = 'x' * (128 << 10);
      for (var i = 1; i <= 100; i++) {
        t.add(event(i, text: large));
      }
      expect(t.blocks.length, 8);
      expect(
        t.blocks.fold<int>(0, (n, b) => n + b.text.length),
        lessThanOrEqualTo(Transcript.maxTextUnits),
      );
      t.add({...event(100, text: 'short'), 'id': 'update', 'seq': 101});
      expect(t.blocks.last.text, 'short');
      expect(t.blocks.last.first, 100);
    },
  );

  testWidgets(
    'long output lays out a bounded prefix and expands without loss',
    (tester) async {
      final text = 'Line\n' * 30000;
      String rendered = '';
      await tester.pumpWidget(
        app(
          ProgressiveText(
            text: text,
            builder: (v) {
              rendered = v;
              return Text('shown ${v.length}');
            },
          ),
        ),
      );
      expect(rendered.length, lessThanOrEqualTo(8001));
      expect(text.startsWith(rendered), isTrue);
      await tester.tap(find.byKey(const ValueKey('continue-long-text')));
      await tester.pump();
      expect(rendered.length, greaterThan(8000));
      while (find
          .byKey(const ValueKey('continue-long-text'))
          .evaluate()
          .isNotEmpty) {
        await tester.tap(find.byKey(const ValueKey('continue-long-text')));
        await tester.pump();
      }
      expect(rendered, text);
      await tester.tap(find.byKey(const ValueKey('collapse-long-text')));
      await tester.pump();
      expect(rendered.length, lessThanOrEqualTo(8001));
    },
  );

  testWidgets('collapsed tool does not mount a huge selectable output', (
    tester,
  ) async {
    final c = AgentClient();
    addTearDown(c.dispose);
    await tester.pumpWidget(
      app(
        SingleChildScrollView(
          child: TranscriptTile(
            client: c,
            block: TranscriptBlock(
              key: 'tool',
              kind: 'tool_result',
              text: 'x' * (128 << 10),
            ),
          ),
        ),
      ),
    );
    expect(find.byType(SelectableText), findsNothing);
    await tester.tap(find.byType(ExpansionTile));
    await tester.pumpAndSettle();
    expect(find.byType(SelectableText), findsOneWidget);
    expect(
      tester.widget<SelectableText>(find.byType(SelectableText)).data!.length,
      4000,
    );
  });
  testWidgets('expand-all virtualizes a large activity group', (tester) async {
    final c = SlowDeliveryClient();
    addTearDown(c.dispose);
    await c.watch('a');
    for (var i = 1; i <= 190; i++) {
      c.event('tool_call', 'Read\nSynthetic arguments', i);
    }
    c.event('message', 'Final reply', 191, role: 'assistant');
    await tester.pumpWidget(conversation(c));
    await tester.pumpAndSettle();
    expect(find.byType(ExpansionTile), findsNothing);
    await tester.tap(find.byKey(const ValueKey('all-activities-toggle')));
    await tester.pumpAndSettle();
    expect(find.byType(ExpansionTile).evaluate().length, greaterThan(0));
    expect(find.byType(ExpansionTile).evaluate().length, lessThan(30));
    expect(c.transcripts['a']!.blocks.length, 191);
    await tester.drag(
      find.byKey(const ValueKey('transcript-a-0')),
      const Offset(0, 400),
    );
    await tester.pumpAndSettle();
    expect(find.byType(ExpansionTile).evaluate().length, lessThan(30));
  });

  testWidgets('native Claude waiting shows confirmation instead of executing', (
    tester,
  ) async {
    final c = SlowDeliveryClient();
    addTearDown(c.dispose);
    await c.watch('a');
    c.sessions = [
      AgentSession(
        id: 'a',
        agent: 'claude',
        title: 'Synthetic',
        source: 'claude-desktop',
        status: 'waiting',
      ),
    ];
    await tester.pumpWidget(conversation(c));
    await tester.pump();
    expect(find.text('等待确认'), findsOneWidget);
    expect(find.text('等待电脑端确认…'), findsOneWidget);
    expect(find.text('执行中'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    await tester.pump();
  });
}
