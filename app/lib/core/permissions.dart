class PermissionChoice {
  final String id, label, detail;
  const PermissionChoice(this.id, this.label, this.detail);
  factory PermissionChoice.fromJson(Map<String, dynamic> v) => PermissionChoice(
    v['id'] as String? ?? '',
    v['label'] as String? ?? '',
    v['detail'] as String? ?? '',
  );
}

class SessionPermissions {
  final String mode, label, detail, source, version;
  final bool editable;
  final List<PermissionChoice> choices;
  SessionPermissions.fromJson(Map<String, dynamic> v)
    : mode = v['mode'] as String? ?? '',
      label = v['label'] as String? ?? '权限未提供',
      detail = v['detail'] as String? ?? '',
      source = v['source'] as String? ?? 'unknown',
      version = v['version'] as String? ?? '',
      editable = v['editable'] == true,
      choices = (v['choices'] as List? ?? [])
          .map(
            (c) =>
                PermissionChoice.fromJson(Map<String, dynamic>.from(c as Map)),
          )
          .toList();
}

List<PermissionChoice> creationPermissions(String agent) => switch (agent) {
  'codex' => const [
    PermissionChoice('read-only', 'Read only', ''),
    PermissionChoice('on-request', 'Default permissions', ''),
    PermissionChoice('auto_review', 'Auto review', ''),
    PermissionChoice('full-access', 'Full access', ''),
  ],
  'claude' => const [
    PermissionChoice('default', 'Default', ''),
    PermissionChoice('acceptEdits', 'Accept edits', ''),
    PermissionChoice('auto', 'Auto', ''),
    PermissionChoice('bypassPermissions', 'Bypass permissions', ''),
    PermissionChoice('plan', 'Plan', ''),
    PermissionChoice('dontAsk', "Don't ask", ''),
  ],
  _ => const [],
};
