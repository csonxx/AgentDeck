import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/session_reads.dart';
import 'package:flutter_test/flutter_test.dart';

final baseline = DateTime.utc(2026, 10, 14);
AgentSession session(
  int revision, {
  String status = 'completed',
  String id = 'a',
  String? at,
}) => AgentSession(
  id: id,
  agent: 'codex',
  title: 'Chat',
  source: 'stream',
  status: status,
  completion: revision,
  completedAt: at ?? '2026-10-14T00:01:00Z',
);
void main() {
  test('native Desktop unread imports and read changes reach the phone', () {
    final r = SessionReads()..observe([session(1)], now: baseline);
    void native(bool unread) => r.applyNative({
      'available': true,
      'writable': false,
      'states': {'a': unread},
    });
    native(true);
    expect(r.isUnread(session(1)), isTrue);
    native(false);
    expect(r.isUnread(session(1)), isFalse);
    native(true);
    r.markRead('a');
    expect(r.isUnread(session(1)), isFalse);
    final restored = SessionReads()..restore(r.toJson());
    expect(restored.isUnread(session(1)), isFalse);
    expect(restored.isUnread(session(2)), isTrue);
    // A separate native mark-read/mark-unread invalidates the local receipt.
    native(false);
    native(true);
    expect(r.isUnread(session(1)), isTrue);
    r.foreground = false;
    r.visible = 'a';
    r.observe([session(2)]);
    expect(r.isUnread(session(2)), isTrue);
    r.foreground = true;
    r.observe([session(2)]);
    expect(r.isUnread(session(2)), isFalse);
  });
  test(
    'background download reconnect does not consume unread completion',
    () async {
      final c = AgentClient();
      addTearDown(c.dispose);
      c.sessionReads.observe([session(1)], now: baseline);
      c.updateSessions([session(1)]);
      c.sessionReads.visible = 'a';
      c.suspend();
      c.updateSessions([session(2)]);
      expect(c.isUnread(session(2)), isTrue);
      c.retainDownloadConnection();
      await Future<void>.delayed(Duration.zero);
      expect(c.sessionReads.foreground, isFalse);
      expect(c.isUnread(session(2)), isTrue);
      c.releaseDownloadConnection();
    },
  );
  test('initial history is read; completion marks unread until opened', () {
    final r = SessionReads()..observe([session(1)], now: baseline);
    expect(r.isUnread(session(1)), isFalse);
    r.observe([session(2)]);
    expect(r.isUnread(session(2)), isTrue);
    r.markRead('a');
    expect(r.isUnread(session(2)), isFalse);
    r.observe([session(2)]); // List refresh / title edit.
    expect(r.isUnread(session(2)), isFalse);
    r.observe([session(3, status: 'running')]);
    expect(r.isUnread(session(3, status: 'running')), isFalse);
    r.observe([session(3)]);
    expect(r.isUnread(session(3)), isTrue);
  });
  test('visible foreground chat is read, background completion is unread', () {
    final r = SessionReads()
      ..observe([session(1)], now: baseline)
      ..visible = 'a';
    r.observe([session(2)]);
    expect(r.isUnread(session(2)), isFalse);
    r.foreground = false;
    r.observe([session(3)]);
    expect(r.isUnread(session(3)), isTrue);
    final restored = SessionReads()..restore(r.toJson());
    restored.observe([session(3)]);
    expect(restored.isUnread(session(3)), isTrue);
    restored.markRead('a');
    final reopened = SessionReads()..restore(restored.toJson());
    reopened.observe([session(3)]);
    expect(reopened.isUnread(session(3)), isFalse);
  });
  test('offline completion is found without seeing the running transition', () {
    final r = SessionReads()..observe([session(1)], now: baseline);
    final restart = SessionReads()..restore(r.toJson());
    restart.observe([session(9)]);
    expect(restart.isUnread(session(9)), isTrue);
  });
  test('historical imports and repeated idle snapshots never mark unread', () {
    final r = SessionReads()..observe([], now: baseline);
    final historical = session(5, at: '2026-10-13T00:00:00Z');
    r.observe([historical]);
    expect(r.isUnread(historical), isFalse);
    r.observe([historical, session(6, id: 'new')]);
    expect(r.isUnread(session(6, id: 'new')), isTrue);
    r.markRead('new');
    r.observe([historical, session(6, id: 'new', status: 'idle')]);
    expect(r.isUnread(session(6, id: 'new', status: 'idle')), isFalse);
  });
}
