import 'package:flutter_test/flutter_test.dart';
import 'package:agentdeck/core/model.dart';

void main() {
  test(
    'reading a cached window preserves it without inventing a missing tail',
    () {
      Map<String, dynamic> record(int n) => {
        'id': 'cached-$n',
        'item': 'record-$n',
        'seq': n,
        'first': n,
        'kind': 'message',
        'text': 'Record $n',
        'mode': 'snapshot',
      };
      final t = Transcript();
      t.history({
        'events': [for (var n = 1; n <= 80; n++) record(n)],
        'cursor': 80,
        'before': 1,
        'has_more': false,
      }, replace: true);
      t.readingHistory = true;
      t.liveSnapshot({
        'events': [for (var n = 41; n <= 80; n++) record(n)],
        'cursor': 80,
        'before': 41,
        'has_more': true,
      });
      expect(t.blocks.length, 80);
      expect(t.detached, isFalse);
      t.liveSnapshot({
        'events': [record(81)],
        'cursor': 81,
        'before': 81,
        'has_more': true,
      });
      expect(t.blocks.length, 80);
      expect(t.detached, isTrue);
      expect(t.latestLiveCursor, 81);
    },
  );

  test(
    'reconnect reconciles retained items without replacing the reading window',
    () {
      Map<String, dynamic> event(String id, int first, int seq, String text) =>
          {
            'id': id,
            'item': 'record-$first',
            'seq': seq,
            'first': first,
            'kind': 'message',
            'role': 'assistant',
            'mode': 'snapshot',
            'text': text,
          };
      final t = Transcript();
      t.history({
        'events': [event('old', 10, 10, 'Retained history')],
        'cursor': 100,
        'before': 10,
        'has_more': true,
        'has_newer': true,
      }, replace: true);
      t.liveSnapshot({
        'events': [
          event('update', 10, 109, 'Updated retained history'),
          event('tail', 110, 110, 'Latest response'),
        ],
        'cursor': 110,
        'before': 100,
        'has_more': true,
      });
      expect(t.blocks.single.first, 10);
      expect(t.blocks.single.text, 'Updated retained history');
      expect(t.before, 10);
      expect(t.hasMore, isTrue);
      expect(t.detached, isTrue);
      expect(t.latestLiveCursor, 110);
    },
  );

  test(
    'older snapshots cannot overwrite newer live output or duplicate items',
    () {
      final t = Transcript();
      Map<String, dynamic> event(
        String id,
        String item,
        int seq,
        int first,
        String text,
      ) => {
        'id': id,
        'item': item,
        'seq': seq,
        'first': first,
        'kind': 'message',
        'mode': 'snapshot',
        'text': text,
      };
      t.history({
        'events': [event('recent', 'new', 100, 90, 'recent')],
        'cursor': 100,
        'before': 90,
        'has_more': true,
      }, replace: true);
      t.add(event('live', 'old', 101, 1, 'complete updated prefix'));
      t.history({
        'events': [event('older', 'old', 50, 1, 'stale')],
        'cursor': 100,
        'before': 1,
        'has_more': false,
      });
      expect(t.blocks.length, 2);
      expect(t.blocks.first.text, 'complete updated prefix');
      expect(t.cursor, 101);
      expect(t.hasMore, isFalse);
    },
  );
  test('control context hidden and literal HTML preserved', () {
    expect(
      cleanMessage(
        '<environment_context>private</environment_context>修复 <div>hello</div>',
      ),
      '修复 <div>hello</div>',
    );
    final t = Transcript();
    t.add({
      'id': 'ctx',
      'seq': 1,
      'kind': 'message',
      'role': 'user',
      'text': '<environment_context>private</environment_context>',
      'mode': 'snapshot',
    });
    expect(t.blocks, isEmpty);
    expect(t.cursor, 1);
  });

  test('deltas append, completed snapshots replace, replay is ignored', () {
    final t = Transcript();
    Map<String, dynamic> ev(String id, int seq, String mode, String text) => {
      'id': id,
      'seq': seq,
      'kind': 'message',
      'item': 'm',
      'role': 'assistant',
      'text': text,
      'mode': mode,
    };
    t.add(ev('a', 1, 'delta', 'Hello '));
    t.add(ev('b', 2, 'delta', 'world'));
    t.add(ev('b', 2, 'delta', 'world'));
    expect(t.blocks.single.text, 'Hello world');
    t.add(ev('c', 3, 'snapshot', 'Hello world!'));
    expect(t.blocks.single.text, 'Hello world!');
    expect(t.cursor, 3);
  });
  test('reasoning, tools and images remain independent display blocks', () {
    final t = Transcript();
    for (final kind in ['thinking', 'tool_call', 'tool_result', 'image']) {
      t.add({
        'id': kind,
        'seq': 1,
        'kind': kind,
        'item': 'same',
        'text': kind,
        'media': 'blob',
        'mode': 'snapshot',
      });
    }
    expect(t.blocks.length, 4);
    expect(t.blocks.last.media, 'blob');
  });
}
