import 'package:flutter_test/flutter_test.dart';
import 'package:agentdeck/core/model.dart';

void main() {
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
