class AgentGoal {
  final String session, agent, objective, status, version, note, reason;
  final int tokens, seconds, iterations;
  final int? budget;
  final List<String> actions;
  AgentGoal(Map<String, dynamic> v)
    : session = v['session'] as String,
      agent = v['agent'] as String,
      objective = v['objective'] as String? ?? '',
      status = v['status'] as String? ?? '',
      version = v['version'] as String? ?? '',
      note = v['control_note'] as String? ?? '',
      reason = v['reason'] as String? ?? '',
      tokens = (v['tokens_used'] as num?)?.toInt() ?? 0,
      seconds = (v['time_used_seconds'] as num?)?.toInt() ?? 0,
      iterations = (v['iterations'] as num?)?.toInt() ?? 0,
      budget = (v['token_budget'] as num?)?.toInt(),
      actions = (v['actions'] as List? ?? []).whereType<String>().toList();
  bool get isCurrent => const {
    'active',
    'paused',
    'blocked',
    'usageLimited',
    'budgetLimited',
  }.contains(status);
  String get label => switch (status) {
    'active' => '目标执行中',
    'paused' => '目标已暂停',
    'blocked' => '目标受阻',
    'usageLimited' => '目标额度受限',
    'budgetLimited' => '目标预算耗尽',
    'complete' => '目标已完成',
    'failed' => '目标失败',
    _ => '目标',
  };
}
