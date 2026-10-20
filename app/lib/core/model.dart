import 'dart:typed_data';

class SessionIssue {
  final String code, message, action;
  final bool blocksInput;
  const SessionIssue({
    required this.code,
    required this.message,
    this.action = '',
    this.blocksInput = false,
  });
  factory SessionIssue.fromJson(Map<String, dynamic> m) => SessionIssue(
    code: m['code'] as String? ?? '',
    message: m['message'] as String? ?? '',
    action: m['action'] as String? ?? '',
    blocksInput: m['blocks_input'] == true,
  );
  Map<String, dynamic> toJson() => {
    'code': code,
    'message': message,
    'action': action,
    'blocks_input': blocksInput,
  };
}

class AgentSession {
  final SessionIssue? issue;
  final String id, agent, title, source, status, updated, project, parent;
  final bool managed;
  final int completion;
  final String completedAt;
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
    this.completion = 0,
    this.completedAt = '',
    this.issue,
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
    completion: (m['completion'] as num?)?.toInt() ?? 0,
    completedAt: m['completed_at'] as String? ?? '',
    issue: m['issue'] is Map && (m['issue']['code'] as String? ?? '').isNotEmpty
        ? SessionIssue.fromJson(Map<String, dynamic>.from(m['issue']))
        : null,
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
  int position = 0;
  bool detail = false;
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
  static const maxBlocks = 200;
  static const maxTextUnits = 1024 * 1024;
  bool _loadingPage = false;
  int _textUnits = 0;
  final List<TranscriptBlock> blocks = [];
  final Map<String, TranscriptBlock> _items = {};
  final Set<String> _seen = {};
  int cursor = 0, before = 0;
  bool hasMore = false,
      loadingOlder = false,
      loadingNewer = false,
      detached = false;
  bool readingHistory = false;
  int latestLiveCursor = 0;
  int revision = 0;
  int windowVersion = 0;
  // Direction changes invalidate an in-flight history reply without clearing
  // what the user is reading. Its eventual response must not evict the new
  // reading edge or resurrect a window replaced by jump-to-latest.
  void cancelPaging() => windowVersion++;
  void liveSnapshot(Map<String, dynamic> page) {
    if ((!detached && !readingHistory) || blocks.isEmpty) {
      refreshLatest(page);
      return;
    }
    // A reconnect must not replace an older reading window with the latest
    // forty records. Reconcile retained items, and page forward on demand.
    final latest = (page['cursor'] as num).toInt();
    if (latest > latestLiveCursor) latestLiveCursor = latest;
    for (final raw in page['events'] as List) {
      final event = Map<String, dynamic>.from(raw as Map);
      if (containsEvent(event)) {
        add(event);
      } else if (((event['first'] ?? event['seq']) as num).toInt() >
          blocks.last.first) {
        detached = true;
      }
    }
    revision++;
  }

  void refreshLatest(Map<String, dynamic> page) {
    final pageCursor = (page['cursor'] as num).toInt();
    // A live delta can arrive while Local is preparing the refresh reply.
    // Keep any newer materialized values instead of rolling them backwards.
    final newer = blocks
        .where((b) => b.seq > pageCursor)
        .map(
          (b) => {
            'id': 'retained/${b.key}/${b.seq}',
            'item': b.key.substring(0, b.key.length - b.kind.length - 1),
            'seq': b.seq,
            'first': b.first,
            'position': b.position,
            'detail': b.detail,
            'kind': b.kind,
            'role': b.role,
            'text': b.text,
            'media': b.media,
            'status': b.status,
            'mode': 'snapshot',
          },
        )
        .toList();
    history(page, replace: true);
    for (final event in newer) {
      add(event);
    }
    if (latestLiveCursor > cursor) detached = true;
  }

  void history(
    Map<String, dynamic> page, {
    bool replace = false,
    bool newer = false,
  }) {
    if (replace) {
      windowVersion++;
      readingHistory = false;
      blocks.clear();
      _textUnits = 0;
      _items.clear();
      _seen.clear();
      cursor = (page['cursor'] as num).toInt();
      detached = page['has_newer'] == true;
    }
    _loadingPage = true;
    try {
      for (final e in page['events'] as List) {
        final event = Map<String, dynamic>.from(e as Map);
        if (!containsEvent(event)) _seen.remove(event['id']);
        add(event);
      }
    } finally {
      _loadingPage = false;
    }
    blocks.sort((a, b) => a.first.compareTo(b.first));
    if (newer) {
      detached =
          page['has_newer'] == true ||
          latestLiveCursor > (page['cursor'] as num).toInt();
    } else {
      before = (page['before'] as num).toInt();
      hasMore = page['has_more'] == true;
    }
    _trimWindow(keepOlder: !replace && !newer);
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
    b.position = (e['position'] as num?)?.toInt() ?? b.position;
    b.detail = e['detail'] == true;
    if (kind == 'queued_message') b.role = e['role'] as String? ?? b.role;
    revision++;
    final text = cleaned;
    _textUnits -= b.text.length;
    b.text = e['mode'] == 'delta' ? b.text + text : text;
    _textUnits += b.text.length;
    if (!_loadingPage) _trimWindow();
    b.status = e['status'] as String? ?? '';
  }

  void _trimWindow({bool keepOlder = false}) {
    var trimmed = false;
    while (blocks.length > 1 &&
        (blocks.length > maxBlocks || _textUnits > maxTextUnits)) {
      final removed = keepOlder ? blocks.removeLast() : blocks.removeAt(0);
      _items.remove(removed.key);
      _textUnits -= removed.text.length;
      trimmed = true;
    }
    if (trimmed) {
      if (keepOlder) {
        detached = true;
      } else {
        hasMore = true;
      }
    }
    if (blocks.isNotEmpty) before = blocks.first.first;
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
    'oai-mem-citation',
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
  final bool generatedName, truncated, downloadRequired;
  final int size;
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
    this.downloadRequired = false,
    this.size = 0,
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
