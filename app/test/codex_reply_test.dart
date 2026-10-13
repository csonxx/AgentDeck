import 'package:flutter_test/flutter_test.dart';
import 'package:agentdeck/core/model.dart';

void main() {
  test(
    'internal Codex memory citations are filtered in history and live snapshots',
    () {
      final t = Transcript();
      final event = <String, dynamic>{
        'id': 'history',
        'item': 'reply',
        'kind': 'message',
        'role': 'assistant',
        'seq': 1,
        'first': 1,
        'mode': 'snapshot',
        'text':
            'Done.\n<oai-mem-citation><citation_entries>internal</citation_entries></oai-mem-citation>',
      };
      t.history({
        'events': [event],
        'cursor': 1,
        'before': 1,
        'has_more': false,
      }, replace: true);
      expect(t.blocks.single.text, 'Done.');
      t.add({...event, 'id': 'live', 'seq': 2, 'text': 'Done.'});
      expect(t.blocks.length, 1);
      t.add({
        ...event,
        'id': 'next-turn',
        'item': 'other-reply',
        'seq': 3,
        'first': 3,
      });
      expect(t.blocks.length, 2);
      expect(t.blocks.last.text, 'Done.');
    },
  );
}
