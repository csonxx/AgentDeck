const agentNames = <String, String>{
  'codex': 'Codex',
  'claude': 'Claude Code',
  'opencode': 'OpenCode',
  'qoder': 'Qoder',
  'trae': 'TRAE',
  'workbuddy': 'WorkBuddy',
};

class AgentInfo {
  final String id, name, detail;
  final bool configured, canCreate, canSend, canStop, canApprove;
  const AgentInfo({
    required this.id,
    required this.name,
    this.detail = '',
    this.configured = false,
    this.canCreate = true,
    this.canSend = true,
    this.canStop = true,
    this.canApprove = true,
  });
  String get shortName => id == 'claude' ? 'Claude' : name;
  factory AgentInfo.fromJson(Map<String, dynamic> m) => AgentInfo(
    id: m['id'] as String,
    name: m['name'] as String,
    detail: m['detail'] as String? ?? '',
    configured: m['configured'] == true,
    canCreate: m['create'] == true,
    canSend: m['send'] == true,
    canStop: m['stop'] == true,
    canApprove: m['approve'] == true,
  );
  Map<String, dynamic> toJson() => {
    'id': id,
    'name': name,
    'detail': detail,
    'configured': configured,
    'create': canCreate,
    'send': canSend,
    'stop': canStop,
    'approve': canApprove,
  };
}

List<AgentInfo> defaultAgents() => agentNames.entries
    .map(
      (e) => AgentInfo(
        id: e.key,
        name: e.value,
        configured: e.key == 'codex' || e.key == 'claude',
        canCreate: e.key != 'workbuddy',
        canStop: e.key != 'workbuddy',
        canApprove: e.key != 'workbuddy',
        detail: e.key == 'codex' || e.key == 'claude'
            ? '使用工作站 CLI 登录状态'
            : '请在工作站完成接入配置',
      ),
    )
    .toList();

// Store hidden IDs instead of a fixed visible list, so new adapters appear once.
class AgentVisibility {
  final Set<String> hidden;
  AgentVisibility({Iterable<String> hidden = const []})
    : hidden = Set.unmodifiable(hidden);
  factory AgentVisibility.fromJson(Map<String, dynamic> m) =>
      AgentVisibility(hidden: (m['hidden'] as List? ?? []).whereType<String>());
  Map<String, dynamic> toJson() => {'version': 1, 'hidden': hidden.toList()};
  AgentVisibility setVisible(String id, bool visible) {
    final next = hidden.toSet();
    visible ? next.remove(id) : next.add(id);
    return AgentVisibility(hidden: next);
  }
}
