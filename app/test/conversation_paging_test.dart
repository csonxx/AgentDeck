import 'dart:math';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/main.dart';
import 'package:agentdeck/reading_scroll_view.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'conversation_outline_test.dart' show DirectoryClient;

class PagingClient extends DirectoryClient {
  final bool stall;
  final int total, pageSize;
  int olderCalls = 0, newerCalls = 0;
  PagingClient({
    required bool atStart,
    this.stall = false,
    this.total = 40,
    this.pageSize = 2,
  }) {
    transcripts['s']!.history({
      'events': [
        for (var i = atStart ? 1 : total - 1; i <= (atStart ? 2 : total); i++)
          event(i),
      ],
      'cursor': total,
      'before': atStart ? 1 : total - 1,
      'has_more': !atStart,
      'has_newer': atStart,
    }, replace: true);
  }
  @override
  Future<void> loadOlder(String id) async {
    final t = transcripts[id]!;
    if (!t.hasMore || t.loadingOlder || t.loadingNewer) return;
    olderCalls++;
    t.loadingOlder = true;
    notifyListeners();
    await Future<void>.delayed(Duration.zero);
    if (!stall) {
      final start = max(1, t.before - pageSize);
      t.history({
        'events': [for (var i = start; i < t.before; i++) event(i)],
        'cursor': total,
        'before': start,
        'has_more': start > 1,
      });
    }
    t.loadingOlder = false;
    notifyListeners();
  }

  @override
  Future<void> loadNewer(String id) async {
    final t = transcripts[id]!;
    if (!t.detached || t.loadingOlder || t.loadingNewer) return;
    newerCalls++;
    t.loadingNewer = true;
    notifyListeners();
    await Future<void>.delayed(Duration.zero);
    final last = t.blocks.last.first;
    final end = min(total, last + pageSize);
    t.history({
      'events': [for (var i = last + 1; i <= end; i++) event(i)],
      'cursor': total,
      'has_newer': end < total,
    }, newer: true);
    t.loadingNewer = false;
    notifyListeners();
  }
}

// Tool-only pages include separate notices, so folded group headers can move
// the scroll edge away without providing another dialogue message.
class ToolGapClient extends PagingClient {
  ToolGapClient({required super.atStart}) : super(total: 260, pageSize: 8);
  @override
  Map<String, dynamic> event(int i) => i > 2 && i < 259
      ? {
          ...super.event(i),
          'kind': i % 7 == 0 ? 'notice' : 'tool_call',
          'text': i % 7 == 0 ? 'Process notice $i' : 'Tool execution $i',
        }
      : super.event(i);
}

class FoldedRunClient extends PagingClient {
  FoldedRunClient({required super.atStart}) : super(total: 620, pageSize: 30);
  @override
  Map<String, dynamic> event(int i) => i > 2 && i < 619
      ? {...super.event(i), 'kind': 'tool_call', 'text': 'Tool execution $i'}
      : {
          ...super.event(i),
          'text':
              'Full message $i\n${List.filled(6, 'Conversation text for reading').join('\n')}',
        };
}

Future<void> mount(WidgetTester t, PagingClient c) async {
  await t.pumpWidget(
    MaterialApp(
      localizationsDelegates: UiStrings.delegates,
      supportedLocales: UiStrings.supportedLocales,
      locale: AppLanguage.simplifiedChinese.locale,
      home: ConversationPage(client: c, session: c.sessions.first),
    ),
  );
  await t.pumpAndSettle();
}

void main() {
  for (final older in [true, false]) {
    testWidgets(
      'scroll ${older ? "older" : "newer"} pages automatically through the end',
      (t) async {
        final c = PagingClient(atStart: !older);
        addTearDown(c.dispose);
        await mount(t, c);
        final view = find.byType(ReadingScrollView);
        for (var i = 0; i < 35; i++) {
          await t.drag(view, Offset(0, older ? 650 : -650));
          await t.pumpAndSettle();
          final transcript = c.transcripts['s']!;
          if (older ? !transcript.hasMore : !transcript.detached) break;
        }
        final transcript = c.transcripts['s']!;
        expect(older ? transcript.hasMore : transcript.detached, false);
        expect(
          older ? transcript.blocks.first.first : transcript.blocks.last.first,
          older ? 1 : 40,
        );
        expect(older ? c.olderCalls : c.newerCalls, 19);
        expect(older ? c.newerCalls : c.olderCalls, 0);
        expect(find.text('加载更早消息'), findsNothing);
        expect(find.text('加载后续消息'), findsNothing);
        await t.drag(view, Offset(0, older ? 650 : -650));
        await t.pumpAndSettle();
        expect(older ? c.olderCalls : c.newerCalls, 19);
        expect(t.takeException(), isNull);
      },
    );
  }
  testWidgets(
    'scrolling older then back newer crosses bounded window evictions',
    (t) async {
      final c = PagingClient(atStart: false, total: 260, pageSize: 10);
      addTearDown(c.dispose);
      await mount(t, c);
      final view = find.byType(ReadingScrollView);
      for (var i = 0; i < 100 && c.transcripts['s']!.hasMore; i++) {
        await t.drag(view, const Offset(0, 800));
        await t.pumpAndSettle();
      }
      final transcript = c.transcripts['s']!;
      expect(transcript.hasMore, false);
      expect(transcript.blocks.first.first, 1);
      expect(transcript.detached, true);
      expect(transcript.blocks.length, lessThanOrEqualTo(200));
      for (var i = 0; i < 100 && transcript.detached; i++) {
        await t.drag(view, const Offset(0, -800));
        await t.pumpAndSettle();
      }
      expect(transcript.detached, false);
      expect(transcript.blocks.last.first, 260);
      expect(transcript.blocks.length, lessThanOrEqualTo(200));
      expect(t.takeException(), isNull);
    },
  );

  for (final older in [true, false]) {
    testWidgets(
      'one gesture skips folded tool pages ${older ? "older" : "newer"}',
      (t) async {
        final c = ToolGapClient(atStart: !older);
        addTearDown(c.dispose);
        await mount(t, c);
        await t.drag(
          find.byType(ReadingScrollView),
          Offset(0, older ? 650 : -650),
        );
        await t.pumpAndSettle();
        final transcript = c.transcripts['s']!;
        expect(older ? transcript.hasMore : transcript.detached, false);
        expect(
          transcript.blocks.any(
            (b) => b.kind == 'message' && b.first == (older ? 1 : 260),
          ),
          true,
        );
        expect(older ? c.olderCalls : c.newerCalls, greaterThan(25));
        expect(find.textContaining('Tool execution'), findsNothing);
        expect(t.takeException(), isNull);
      },
    );
  }
  testWidgets('opening a folded-tool-only page loads dialogue automatically', (
    t,
  ) async {
    final c = ToolGapClient(atStart: false);
    addTearDown(c.dispose);
    c.transcripts['s']!.history({
      'events': [c.event(250), c.event(251)],
      'cursor': 260,
      'before': 250,
      'has_more': true,
      'has_newer': true,
    }, replace: true);
    await mount(t, c);
    expect(c.transcripts['s']!.blocks.any((b) => b.kind == 'message'), true);
    expect(c.olderCalls, greaterThan(25));
    expect(t.takeException(), isNull);
  });

  testWidgets('unchanged history page stops automatic retries', (t) async {
    final c = PagingClient(atStart: false, stall: true);
    addTearDown(c.dispose);
    await mount(t, c);
    await t.drag(find.byType(ReadingScrollView), const Offset(0, 650));
    await t.pumpAndSettle();
    final requests = c.olderCalls;
    expect(requests, greaterThan(0));
    await t.pump(const Duration(seconds: 3));
    expect(c.olderCalls, requests);
    expect(t.takeException(), isNull);
  });
  for (final older in [true, false]) {
    testWidgets(
      'long folded run reaches visible dialogue without a blank viewport ${older ? "older" : "newer"}',
      (tester) async {
        final c = FoldedRunClient(atStart: !older);
        addTearDown(c.dispose);
        await mount(tester, c);
        await tester.drag(
          find.byType(ReadingScrollView),
          Offset(0, older ? 400 : -400),
        );
        await tester.pumpAndSettle();
        final marker = older ? 'Full message 1' : 'Full message 620';
        final text = find.textContaining(marker);
        expect(text, findsOneWidget);
        final viewport = tester.getRect(find.byType(ReadingScrollView));
        expect(tester.getRect(text).overlaps(viewport), isTrue);
        expect(tester.takeException(), isNull);
      },
    );
  }
  testWidgets(
    'expanded execution does not automatically skip the entire tool run',
    (tester) async {
      final c = FoldedRunClient(atStart: false);
      addTearDown(c.dispose);
      c.transcripts['s']!.history({
        'events': [for (var i = 590; i <= 620; i++) c.event(i)],
        'cursor': 620,
        'before': 590,
        'has_more': true,
      }, replace: true);
      await mount(tester, c);
      await tester.tap(find.byKey(const ValueKey('all-activities-toggle')));
      await tester.pumpAndSettle();
      await tester.drag(find.byType(ReadingScrollView), const Offset(0, 800));
      await tester.pumpAndSettle();
      expect(c.olderCalls, lessThan(5));
      expect(c.transcripts['s']!.hasMore, isTrue);
      expect(tester.takeException(), isNull);
    },
  );
}
