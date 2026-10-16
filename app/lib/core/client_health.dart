enum ConnectionPhase {
  unpaired,
  connecting,
  synchronizing,
  live,
  reconnecting,
  suspended,
  revoked,
  demo,
}

enum LocalIssueKind {
  deviceStorage,
  deviceName,
  cacheRead,
  cacheWrite,
  projectLayout,
  agentVisibility,
}

class LocalIssue {
  final LocalIssueKind kind;
  final String message;
  bool dismissed = false;
  LocalIssue(this.kind, this.message);
  bool get blocking => kind == LocalIssueKind.deviceStorage;
}

/// Connection health and local persistence have independent recovery paths.
/// Request failures belong to their caller, never to this application status.
class ClientHealth {
  ConnectionPhase _phase = ConnectionPhase.unpaired;
  ConnectionPhase get phase => _phase;
  String connectionFailure = '';
  final Map<LocalIssueKind, LocalIssue> _issues = {};
  List<LocalIssue> get issues => List.unmodifiable(_issues.values);
  List<LocalIssue> get visibleIssues =>
      issues.where((issue) => !issue.dismissed).toList();
  LocalIssue? get blockingIssue =>
      issues.where((issue) => issue.blocking).firstOrNull;
  bool get connected =>
      phase == ConnectionPhase.synchronizing || phase == ConnectionPhase.live;
  bool get live => phase == ConnectionPhase.live;
  bool get busy =>
      phase == ConnectionPhase.connecting ||
      phase == ConnectionPhase.synchronizing;
  bool get needsPairing => phase == ConnectionPhase.revoked;

  String get label => switch (phase) {
    ConnectionPhase.unpaired => '尚未配对',
    ConnectionPhase.connecting => '正在连接',
    ConnectionPhase.synchronizing => '正在同步会话',
    ConnectionPhase.live => '实时连接',
    ConnectionPhase.reconnecting => '连接中断 · 自动重连',
    ConnectionPhase.suspended => '已暂停连接',
    ConnectionPhase.revoked => '需要重新配对',
    ConnectionPhase.demo => '界面示例',
  };

  void transition(ConnectionPhase next, {String? failure}) {
    _phase = next;
    if (failure != null) {
      connectionFailure = failure;
    } else if (next == ConnectionPhase.synchronizing ||
        next == ConnectionPhase.live ||
        next == ConnectionPhase.unpaired ||
        next == ConnectionPhase.demo) {
      connectionFailure = '';
    }
  }

  void synchronized() {
    if (connected) transition(ConnectionPhase.live);
  }

  void report(LocalIssueKind kind, String message) {
    // A recurring background failure must not undo the user's dismissal.
    if (_issues[kind]?.message != message) {
      _issues[kind] = LocalIssue(kind, message);
    }
  }

  void resolve(LocalIssueKind kind) => _issues.remove(kind);
  void dismiss(LocalIssueKind kind) {
    final issue = _issues[kind];
    if (issue != null && !issue.blocking) issue.dismissed = true;
  }

  void reset() {
    _issues.clear();
    transition(ConnectionPhase.unpaired);
  }
}
