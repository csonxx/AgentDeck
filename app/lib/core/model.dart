import 'dart:typed_data';

class AgentSession {
  final String id, agent, title, source, status, updated, project, parent;
  final bool managed;
  AgentSession({
    required this.id,
    required this.agent,
    required this.title,
    required this.source,
    required this.status,
    this.updated = '',
    this.project = '',
    this.parent = '',
    this.managed = false,
  });
  String get displayTitle =>
      parent.isNotEmpty && source != 'desktop' ? '$title · 手机分支' : title;
  factory AgentSession.fromJson(Map<String, dynamic> m) => AgentSession(
    id: m['id'] as String,
    agent: m['agent'] as String,
    title: cleanMessage(m['title'] as String),
    source: m['source'] as String,
    status: m['status'] as String,
    updated: m['updated'] as String? ?? '',
    project: m['project'] as String? ?? '',
    managed: m['managed'] == true,
    parent: m['parent'] as String? ?? '',
  );
}

class ProjectLayout {
  final List<String> order;
  final Set<String> collapsed, expanded, hidden;
  ProjectLayout({
    Iterable<String> order = const [],
    Iterable<String> collapsed = const [],
    Iterable<String> expanded = const [],
    Iterable<String> hidden = const [],
  }) : order = List.unmodifiable(order.toSet()),
       collapsed = Set.unmodifiable(collapsed),
       expanded = Set.unmodifiable(expanded),
       hidden = Set.unmodifiable(hidden);
  factory ProjectLayout.fromJson(Map<String, dynamic> value) {
    if (value['version'] != 1) return ProjectLayout();
    List<String> strings(String key) => value[key] is List
        ? (value[key] as List).whereType<String>().toList()
        : [];
    return ProjectLayout(
      order: strings('order'),
      collapsed: strings('collapsed'),
      expanded: strings('expanded'),
      hidden: strings('hidden'),
    );
  }
  Map<String, dynamic> toJson() => {
    'version': 1,
    'order': order,
    'collapsed': collapsed.toList(),
    'expanded': expanded.toList(),
    'hidden': hidden.toList(),
  };
  List<String> sorted(Iterable<String> paths) {
    final available = paths.toSet();
    final remaining = available.difference(order.toSet()).toList()..sort();
    return [...order.where(available.contains), ...remaining];
  }

  // Legacy layouts migrate on first edit; each agent then owns its layout.
  ProjectLayout scoped(String agent) {
    final prefix = '$agent\u001f';
    if (!order.contains(prefix)) {
      Iterable<String> legacy(Iterable<String> xs) =>
          xs.where((v) => !v.contains('\u001f'));
      return ProjectLayout(
        order: legacy(order),
        collapsed: legacy(collapsed),
        expanded: legacy(expanded),
        hidden: legacy(hidden),
      );
    }
    Iterable<String> values(Iterable<String> xs) => xs
        .where((v) => v.startsWith(prefix) && v != prefix)
        .map((v) => v.substring(prefix.length));
    return ProjectLayout(
      order: values(order),
      collapsed: values(collapsed),
      expanded: values(expanded),
      hidden: values(hidden),
    );
  }

  ProjectLayout withScoped(String agent, ProjectLayout value) {
    final prefix = '$agent\u001f';
    List<String> merge(Iterable<String> all, Iterable<String> next) => [
      ...all.where((v) => !v.startsWith(prefix)),
      ...next.map((v) => '$prefix$v'),
    ];
    return ProjectLayout(
      order: [...merge(order, value.order), prefix],
      collapsed: merge(collapsed, value.collapsed),
      expanded: merge(expanded, value.expanded),
      hidden: merge(hidden, value.hidden),
    );
  }

  ProjectLayout toggleCollapsed(String path) {
    final values = collapsed.toSet();
    if (!values.remove(path)) values.add(path);
    return ProjectLayout(
      order: order,
      collapsed: values,
      expanded: expanded,
      hidden: hidden,
    );
  }

  ProjectLayout toggleExpanded(String path) {
    final values = expanded.toSet();
    if (!values.remove(path)) values.add(path);
    return ProjectLayout(
      order: order,
      collapsed: collapsed,
      expanded: values,
      hidden: hidden,
    );
  }

  ProjectLayout toggleHidden(String path) {
    final values = hidden.toSet();
    if (!values.remove(path)) values.add(path);
    return withHidden(values);
  }

  ProjectLayout withHidden(Iterable<String> values) => ProjectLayout(
    order: order,
    collapsed: collapsed,
    expanded: expanded,
    hidden: values,
  );
  ProjectLayout reordered(List<String> visible) {
    final pending = visible.iterator, present = visible.toSet();
    final result = <String>[];
    for (final path in order) {
      if (!present.contains(path)) {
        result.add(path);
      } else if (pending.moveNext()) {
        result.add(pending.current);
      }
    }
    while (pending.moveNext()) {
      result.add(pending.current);
    }
    return ProjectLayout(
      order: result,
      collapsed: collapsed,
      expanded: expanded,
      hidden: hidden,
    );
  }

  ProjectLayout defaultOrder() =>
      ProjectLayout(collapsed: collapsed, expanded: expanded, hidden: hidden);
}

class TranscriptBlock {
  final String key, kind, media;
  String role, text, status;
  int first = 0, seq = 0;
  TranscriptBlock({
    required this.key,
    required this.kind,
    this.role = '',
    this.media = '',
    this.text = '',
    this.status = '',
  });
}

class Transcript {
  final List<TranscriptBlock> blocks = [];
  final Map<String, TranscriptBlock> _items = {};
  final Set<String> _seen = {};
  int cursor = 0, before = 0;
  bool hasMore = false, loadingOlder = false, detached = false;
  int revision = 0;
  void history(Map<String, dynamic> page, {bool replace = false}) {
    if (replace) {
      blocks.clear();
      _items.clear();
      _seen.clear();
      cursor = (page['cursor'] as num).toInt();
      detached = page['has_newer'] == true;
    }
    for (final e in page['events'] as List) {
      add(Map<String, dynamic>.from(e as Map));
    }
    blocks.sort((a, b) => a.first.compareTo(b.first));
    before = (page['before'] as num).toInt();
    hasMore = page['has_more'] == true;
    revision++;
  }

  bool containsEvent(Map<String, dynamic> e) =>
      _items.containsKey('${e['item'] ?? e['id']}:${e['kind']}');
  void add(Map<String, dynamic> e) {
    final id = e['id'] as String;
    if (!_seen.add(id)) return;
    final seq = (e['seq'] as num).toInt();
    if (seq > cursor) cursor = seq;
    if (_seen.length > 2048) _seen.remove(_seen.first);
    final kind = e['kind'] as String;
    if (kind == 'status') return;
    final cleaned = kind == 'message' && e['mode'] != 'delta'
        ? cleanMessage(e['text'] as String? ?? '')
        : e['text'] as String? ?? '';
    if (kind == 'message' && cleaned.isEmpty && e['mode'] != 'delta') return;
    final key = '${e['item'] ?? id}:$kind';
    var b = _items[key];
    if (b == null) {
      b = TranscriptBlock(
        key: key,
        kind: kind,
        role: e['role'] as String? ?? '',
        media: e['media'] as String? ?? '',
      );
      _items[key] = b;
      b.first = (e['first'] as num?)?.toInt() ?? seq;
      if (blocks.isEmpty || blocks.last.first <= b.first) {
        blocks.add(b);
      } else {
        blocks.insert(blocks.indexWhere((v) => v.first > b!.first), b);
      }
    }
    if (seq <= b.seq && b.seq != 0) return;
    b.seq = seq;
    if (kind == 'queued_message') b.role = e['role'] as String? ?? b.role;
    revision++;
    final text = cleaned;
    b.text = e['mode'] == 'delta' ? b.text + text : text;
    b.status = e['status'] as String? ?? '';
  }
}

class AgentProject {
  final String id, name, path;
  AgentProject({required this.id, required this.name, required this.path});
  factory AgentProject.fromJson(Map<String, dynamic> m) =>
      AgentProject(id: m['id'], name: m['name'], path: m['path']);
}

String cleanMessage(String value) {
  if (value.trimLeft().startsWith('# AGENTS.md instructions for ') ||
      value.trimLeft().startsWith('<turn_aborted>')) {
    return '';
  }
  for (final tag in [
    'environment_context',
    'user_instructions',
    'permissions instructions',
    'collaboration_mode',
    'app-context',
    'skills_instructions',
    'external_codex_apps_open_page',
    'codex_apps_open_page_instructions',
    'system-reminder',
    'local-command-caveat',
    'local-command-stdout',
    'command-name',
    'command-message',
    'command-args',
    'task-notification',
  ]) {
    value = value.replaceAll(
      RegExp(
        '<${RegExp.escape(tag)}>'
        r'[\s\S]*?</'
        '${RegExp.escape(tag)}>',
      ),
      '',
    );
  }
  return value.trim();
}

class SessionAttachment {
  final bool generatedName, truncated;
  final String name, mime;
  final Uint8List bytes;
  final int line;
  const SessionAttachment({
    required this.name,
    required this.mime,
    required this.bytes,
    this.line = 0,
    this.generatedName = false,
    this.truncated = false,
  });
  bool get isImage => mime.startsWith('image/');
}

class ConversationEntry {
  final String key, kind, role, preview, time;
  final int first;
  const ConversationEntry({
    required this.key,
    required this.first,
    required this.kind,
    required this.role,
    required this.preview,
    this.time = '',
  });
  factory ConversationEntry.fromJson(Map<String, dynamic> v) =>
      ConversationEntry(
        key: v['key'] as String,
        first: (v['first'] as num).toInt(),
        kind: v['kind'] as String,
        role: v['role'] as String? ?? '',
        preview: cleanMessage(v['preview'] as String),
        time: v['time'] as String? ?? '',
      );
}

class QueuedMessage {
  final String id, text, state, origin, version;
  final int attachments;
  const QueuedMessage({
    required this.id,
    required this.text,
    required this.state,
    required this.origin,
    required this.version,
    this.attachments = 0,
  });
  factory QueuedMessage.fromJson(Map<String, dynamic> v) => QueuedMessage(
    id: v['id'] as String,
    text: v['text'] as String? ?? '',
    state: v['state'] as String? ?? 'queued',
    origin: v['origin'] as String? ?? 'local',
    version: v['version'] as String? ?? '',
    attachments: v['attachments'] as int? ?? 0,
  );
  bool get editable => state == 'queued' || state == 'paused';
  bool get cancellable => editable || state == 'unknown';
}
