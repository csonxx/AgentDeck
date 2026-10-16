import 'model.dart';

// Device-local read receipts, persisted inside the encrypted transcript cache.
// An initial snapshot establishes a baseline instead of marking old chats unread.
class SessionReads {
  final Map<String, int> seen = {};
  final Map<String, int> latest = {};
  final Map<String, bool> native = {};
  final Map<String, int> nativeSeen = {};
  DateTime? baseline;
  String visible = '';
  bool foreground = true;

  bool isUnread(AgentSession s) =>
      s.status != 'running' &&
      s.status != 'pending' &&
      s.status != 'waiting' &&
      (native.containsKey(s.id)
          ? native[s.id] == true && nativeSeen[s.id] != s.completion
          : (latest[s.id] ?? 0) > (seen[s.id] ?? 0));

  void applyNative(Map<String, dynamic> state) {
    final next = <String, bool>{};
    if (state['available'] == true && state['states'] is Map) {
      for (final e in (state['states'] as Map).entries) {
        if (e.key is String && e.value is bool) next[e.key] = e.value;
      }
    }
    for (final e in next.entries) {
      if (!e.value || native[e.key] == false) nativeSeen.remove(e.key);
    }
    native
      ..clear()
      ..addAll(next);
  }

  void observe(List<AgentSession> sessions, {DateTime? now}) {
    final first = baseline == null;
    baseline ??= (now ?? DateTime.now()).toUtc();
    for (final s in sessions) {
      final revision = s.completion;
      final prior = latest[s.id];
      if (first || prior == null) {
        final stamp = DateTime.tryParse(s.completedAt);
        // New catalog entries can be historical imports rather than new work.
        seen[s.id] = first || stamp == null || stamp.isBefore(baseline!)
            ? revision
            : 0;
      }
      if (revision > (prior ?? 0)) latest[s.id] = revision;
      final completedAt = DateTime.tryParse(s.completedAt);
      if (completedAt != null && completedAt.isBefore(baseline!)) {
        seen[s.id] = revision;
      }
      if (foreground && visible == s.id) markRead(s.id);
    }
  }

  bool markRead(String id) {
    final revision = latest[id] ?? 0;
    var changed = false;
    if (native[id] == true && nativeSeen[id] != revision) {
      nativeSeen[id] = revision;
      changed = true;
    }
    if (revision > (seen[id] ?? 0)) {
      seen[id] = revision;
      changed = true;
    }
    return changed;
  }

  void restore(Map<String, dynamic> value) {
    if (value['version'] != 1) return;
    baseline = DateTime.tryParse(value['baseline'] as String? ?? '');
    if (baseline == null) return;
    for (final pair in [(seen, value['seen']), (latest, value['latest'])]) {
      final data = pair.$2;
      if (data is Map) {
        for (final entry in data.entries) {
          if (entry.key is String && entry.value is num && entry.value >= 0) {
            pair.$1[entry.key as String] = (entry.value as num).toInt();
          }
        }
      }
    }
    if (value['native'] is Map) {
      for (final e in (value['native'] as Map).entries) {
        if (e.key is String && e.value is bool) native[e.key] = e.value;
      }
    }
    if (value['native_seen'] is Map) {
      for (final e in (value['native_seen'] as Map).entries) {
        if (e.key is String && e.value is num && e.value >= 0) {
          nativeSeen[e.key] = (e.value as num).toInt();
        }
      }
    }
  }

  Map<String, dynamic> toJson() => {
    'version': 1,
    'baseline': baseline?.toIso8601String(),
    'seen': Map<String, int>.from(seen),
    'latest': Map<String, int>.from(latest),
    'native': Map<String, bool>.from(native),
    'native_seen': Map<String, int>.from(nativeSeen),
  };
}
