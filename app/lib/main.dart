import 'language.dart';
import 'dart:io';
import 'dart:async';
import 'core/local_speech.dart';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart'
    show LicenseRegistry, LicenseEntryWithLineBreaks;
import 'package:flutter/services.dart' show rootBundle;
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'core/client.dart';
import 'gateway_page.dart';
import 'core/model.dart';
import 'attachment_page.dart';
import 'media_preview.dart';
import 'conversation_outline.dart';
import 'dart:typed_data';
import 'design.dart';
import 'agent_settings.dart';
import 'core/agents.dart';
import 'core/input.dart';
import 'input_composer.dart';
import 'message_queue.dart';
import 'running_indicator.dart';
export 'running_indicator.dart';

const ink = DeckColors.ink;
const paper = DeckColors.paper;
const muted = DeckColors.muted;
const accent = DeckColors.accent;

String sessionDisplayTitle(BuildContext context, AgentSession session) {
  final title = session.title.isEmpty
      ? context.tr('新会话')
      : session.agent == 'workbuddy' &&
            session.source == 'workbuddy-local' &&
            session.title == '本地助理'
      ? context.tr('本地助理')
      : session.managed && session.title == '新会话'
      ? context.tr('新会话')
      : session.title;
  return session.parent.isNotEmpty && session.source != 'desktop'
      ? context.tr('{0} · 手机分支', [title])
      : title;
}

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  LicenseRegistry.addLicense(() async* {
    yield LicenseEntryWithLineBreaks([
      'Zipformer bilingual client speech model',
    ], await rootBundle.loadString('assets/asr/LICENSE'));
    yield LicenseEntryWithLineBreaks([
      'SenseVoiceSmall / Alibaba Group / FunAudioLLM',
    ], await rootBundle.loadString('assets/asr/REFINER_LICENSE'));
  });
  runApp(AgentDeckApp());
  unawaited(LocalSpeechEngine.shared.prepare().catchError((Object _) {}));
}

class AgentDeckApp extends StatefulWidget {
  final AgentClient? client;
  final LanguageController? languages;
  const AgentDeckApp({super.key, this.client, this.languages});
  @override
  State<AgentDeckApp> createState() => _AgentDeckAppState();
}

class _AgentDeckAppState extends State<AgentDeckApp>
    with WidgetsBindingObserver {
  late final AgentClient client = widget.client ?? AgentClient();
  late final LanguageController languages =
      widget.languages ?? LanguageController(client.vault);
  @override
  void initState() {
    super.initState();
    if (widget.languages == null) unawaited(languages.load());
    WidgetsBinding.instance.addObserver(this);
    if (widget.client == null) {
      client.initialize();
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    if (widget.languages == null) languages.dispose();
    if (widget.client == null) client.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.paused) client.suspend();
    if (state == AppLifecycleState.resumed) client.resume();
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: languages,
    builder: (context, _) => LanguageScope(
      controller: languages,
      child: MaterialApp(
        debugShowCheckedModeBanner: false,
        title: 'AgentDeck',
        theme: deckTheme(),
        locale: languages.language.locale,
        supportedLocales: UiStrings.supportedLocales,
        localizationsDelegates: UiStrings.delegates,
        home: DeckHome(client: client),
      ),
    ),
  );
}

String projectName(String path) => path.isEmpty
    ? '未归类'
    : path.split('/').where((s) => s.isNotEmpty).lastOrNull ?? path;
String projectDisplayName(BuildContext context, String path) =>
    path.isEmpty ? context.tr('未归类') : projectName(path);
String agentName(String agent) => agentNames[agent] ?? agent;

class DeckHome extends StatefulWidget {
  final AgentClient client;
  const DeckHome({super.key, required this.client});
  @override
  State<DeckHome> createState() => _DeckHomeState();
}

class _DeckHomeState extends State<DeckHome> {
  int tab = 0;
  String filter = 'codex', project = '';
  ProjectLayout get layout => c.projectLayout.scoped(filter);
  bool creating = false, recent = false;
  AgentClient get c => widget.client;
  Future<void> _pair() async {
    await Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => PairPage(client: c)));
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: c,
    builder: (context, _) => Scaffold(
      body: SafeArea(
        child: Align(
          alignment: Alignment.topCenter,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 760),
            child: tab == 0 ? _sessions() : _device(),
          ),
        ),
      ),
      bottomNavigationBar: DecoratedBox(
        decoration: const BoxDecoration(
          border: Border(top: BorderSide(color: DeckColors.line, width: .6)),
        ),
        child: NavigationBar(
          height: 64,
          backgroundColor: Colors.white,
          elevation: 0,
          indicatorColor: Colors.transparent,
          selectedIndex: tab,
          onDestinationSelected: (v) => setState(() => tab = v),
          destinations: [
            NavigationDestination(
              icon: Icon(Icons.chat_bubble_outline_rounded),
              selectedIcon: Icon(Icons.chat_bubble_rounded),
              label: context.tr('会话'),
            ),
            NavigationDestination(
              icon: Icon(Icons.devices_outlined),
              selectedIcon: Icon(Icons.devices),
              label: context.tr('设备'),
            ),
          ],
        ),
      ),
    ),
  );
  Future<void> _newSession([String? path]) async {
    if (c.projects.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(context.tr('请在后端配置 project-roots'))),
      );
      return;
    }
    final choices = c.projects;
    final initialProject = path ?? project;
    String selected = choices.any((p) => p.path == initialProject)
        ? choices.firstWhere((p) => p.path == initialProject).id
        : choices.first.id;
    final selectable = c.visibleAgents
        .where((a) => a.canCreate && a.configured)
        .toList();
    if (selectable.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(context.tr('请先在工作站安装或配置要使用的 Agent'))),
      );
      return;
    }
    String agent = selectable.any((a) => a.id == filter)
        ? filter
        : selectable.first.id;
    final ok = await showDialog<bool>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, set) => AlertDialog(
          title: Text(context.tr('新建会话')),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              DropdownButtonFormField<String>(
                initialValue: selected,
                isExpanded: true,
                decoration: InputDecoration(labelText: context.tr('项目')),
                items: choices
                    .map(
                      (p) => DropdownMenuItem(
                        value: p.id,
                        child: Text(p.name, overflow: TextOverflow.ellipsis),
                      ),
                    )
                    .toList(),
                onChanged: (v) => set(() => selected = v!),
              ),
              const SizedBox(height: 20),
              DropdownButtonFormField<String>(
                initialValue: agent,
                isExpanded: true,
                decoration: const InputDecoration(labelText: 'Agent'),
                items: selectable
                    .map(
                      (a) => DropdownMenuItem(value: a.id, child: Text(a.name)),
                    )
                    .toList(),
                onChanged: (v) => set(() => agent = v!),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: Text(context.tr('取消')),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: Text(context.tr('创建')),
            ),
          ],
        ),
      ),
    );
    if (ok != true || !mounted) return;
    setState(() => creating = true);
    try {
      final session = await c.createSession(agent, selected);
      if (mounted) {
        setState(() {
          project = session.project;
          filter = session.agent;
        });
        await Navigator.push(
          context,
          MaterialPageRoute(
            builder: (_) => ConversationPage(client: c, session: session),
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.trError(e))));
      }
    } finally {
      if (mounted) setState(() => creating = false);
    }
  }

  void _open(AgentSession session) {
    c.watch(session.id);
    Navigator.push(
      context,
      MaterialPageRoute(
        builder: (_) => ConversationPage(client: c, session: session),
      ),
    );
  }

  String _updatedLabel(String value) {
    final time = DateTime.tryParse(value)?.toLocal();
    if (time == null) return '';
    final now = DateTime.now();
    if (time.year == now.year &&
        time.month == now.month &&
        time.day == now.day) {
      return '${time.hour.toString().padLeft(2, '0')}:${time.minute.toString().padLeft(2, '0')}';
    }
    return '${time.month}/${time.day}';
  }

  Widget _sessionRow(AgentSession session, {bool grouped = false}) {
    final selected = c.selected == session.id;
    return Padding(
      padding: EdgeInsets.only(
        left: grouped ? 12 : 0,
        right: grouped ? 12 : 0,
        bottom: grouped ? 2 : 10,
      ),
      child: Material(
        color: selected
            ? const Color(0xFFFFF1EA)
            : grouped
            ? Colors.transparent
            : Colors.white,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(grouped ? 12 : 18),
          side: grouped
              ? BorderSide.none
              : const BorderSide(color: DeckColors.line, width: .6),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          key: ValueKey('session-${session.id}'),
          onTap: () => _open(session),
          child: Padding(
            padding: EdgeInsets.symmetric(
              horizontal: grouped ? 14 : 16,
              vertical: grouped ? 13 : 17,
            ),
            child: Row(
              children: [
                if (!grouped) ...[
                  AgentGlyph(session.agent, size: 36),
                  const SizedBox(width: 13),
                ],
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        sessionDisplayTitle(context, session),
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 14,
                          fontWeight: selected
                              ? FontWeight.w600
                              : FontWeight.w500,
                          height: 1.45,
                        ),
                      ),
                      if (!grouped) ...[
                        const SizedBox(height: 6),
                        Text(
                          '${projectDisplayName(context, session.project)} · ${agentName(session.agent)}',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(fontSize: 11, color: muted),
                        ),
                      ],
                    ],
                  ),
                ),
                const SizedBox(width: 12),
                Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.end,
                  children: [
                    if (session.status == 'running' ||
                        session.status == 'pending')
                      const RunningIndicator()
                    else
                      Text(
                        _updatedLabel(session.updated),
                        style: const TextStyle(fontSize: 10, color: muted),
                      ),
                    if (!grouped) ...[
                      const SizedBox(height: 9),
                      const Icon(
                        Icons.chevron_right_rounded,
                        size: 17,
                        color: muted,
                      ),
                    ],
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _saveLayout(ProjectLayout value) async {
    try {
      await c.updateProjectLayout(c.projectLayout.withScoped(filter, value));
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.tr('项目布局保存失败，请重试'))));
      }
    }
  }

  void _sortProjects() => Navigator.push(
    context,
    MaterialPageRoute(
      builder: (_) => ProjectOrderPage(client: c, agent: filter),
    ),
  );

  Widget _projectGroup(String path, List<AgentSession> sessions) {
    final collapsed = layout.collapsed.contains(path);
    final expanded = layout.expanded.contains(path);
    final canCreate =
        c.connected && !creating && c.projects.any((p) => p.path == path);
    final name = projectDisplayName(context, path);
    return Container(
      margin: const EdgeInsets.only(bottom: 14),
      padding: const EdgeInsets.only(bottom: 8),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: DeckColors.line, width: .6),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 8, 0),
            child: Row(
              children: [
                Expanded(
                  child: Semantics(
                    container: true,
                    button: true,
                    onTap: () => _saveLayout(layout.toggleCollapsed(path)),
                    label: context.tr('{0}项目 {1}', [
                      collapsed ? context.tr('展开') : context.tr('折叠'),
                      name,
                    ]),
                    excludeSemantics: true,
                    child: InkWell(
                      key: ValueKey('project-toggle-$path'),
                      borderRadius: BorderRadius.circular(12),
                      onTap: () => _saveLayout(layout.toggleCollapsed(path)),
                      child: Padding(
                        padding: const EdgeInsets.symmetric(vertical: 10),
                        child: Row(
                          children: [
                            Container(
                              width: 30,
                              height: 30,
                              decoration: BoxDecoration(
                                color: DeckColors.paper,
                                borderRadius: BorderRadius.circular(11),
                              ),
                              child: Icon(
                                collapsed
                                    ? Icons.folder_outlined
                                    : Icons.folder_open_outlined,
                                size: 18,
                                color: ink,
                              ),
                            ),
                            const SizedBox(width: 12),
                            Expanded(
                              child: Text(
                                name,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(
                                  fontSize: 14,
                                  fontWeight: FontWeight.w600,
                                  letterSpacing: -.2,
                                ),
                              ),
                            ),
                            const SizedBox(width: 8),
                            AnimatedRotation(
                              turns: collapsed ? -.25 : 0,
                              duration: const Duration(milliseconds: 180),
                              child: const Icon(
                                Icons.expand_more_rounded,
                                size: 19,
                                color: muted,
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
                if (collapsed &&
                    sessions.any(
                      (s) => s.status == 'running' || s.status == 'pending',
                    )) ...[
                  const RunningIndicator(),
                  const SizedBox(width: 8),
                ],
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 7,
                    vertical: 3,
                  ),
                  decoration: BoxDecoration(
                    color: DeckColors.paper,
                    borderRadius: BorderRadius.circular(6),
                  ),
                  child: Text(
                    '${sessions.length}',
                    style: const TextStyle(
                      fontSize: 10,
                      fontWeight: FontWeight.w500,
                      color: muted,
                    ),
                  ),
                ),
                if (canCreate)
                  IconButton(
                    tooltip: context.tr('在 {0} 新建会话', [name]),
                    onPressed: () => _newSession(path),
                    icon: const Icon(Icons.add_rounded, size: 19),
                  )
                else
                  const SizedBox(width: 10),
              ],
            ),
          ),
          if (!collapsed) ...[
            const Padding(
              padding: EdgeInsets.fromLTRB(24, 0, 24, 7),
              child: Divider(height: 1),
            ),
            ...sessions
                .take(expanded ? sessions.length : 5)
                .map((s) => _sessionRow(s, grouped: true)),
            if (sessions.length > 5)
              Padding(
                padding: const EdgeInsets.only(left: 19, top: 2),
                child: TextButton(
                  key: ValueKey('expand-$path'),
                  onPressed: () => _saveLayout(layout.toggleExpanded(path)),
                  style: TextButton.styleFrom(foregroundColor: muted),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        expanded
                            ? context.tr('收起显示')
                            : context.tr('展开显示 · 还有 {0} 条', [
                                sessions.length - 5,
                              ]),
                        style: const TextStyle(fontSize: 11),
                      ),
                      const SizedBox(width: 5),
                      Icon(
                        expanded
                            ? Icons.keyboard_arrow_up
                            : Icons.keyboard_arrow_down,
                        size: 16,
                      ),
                    ],
                  ),
                ),
              ),
          ],
        ],
      ),
    );
  }

  Future<void> _agentSettings() async {
    await Navigator.push(
      context,
      MaterialPageRoute<void>(builder: (_) => AgentSettingsPage(client: c)),
    );
  }

  Widget _sessions() {
    if (!c.visibleAgents.any((a) => a.id == filter)) {
      filter = c.visibleAgents.firstOrNull?.id ?? '';
    }

    final ss = c.sessions.where((s) => s.agent == filter).toList()
      ..sort((a, b) {
        final at = DateTime.tryParse(a.updated)?.millisecondsSinceEpoch ?? 0;
        final bt = DateTime.tryParse(b.updated)?.millisecondsSinceEpoch ?? 0;
        return bt.compareTo(at);
      });
    final groups = <String, List<AgentSession>>{};
    for (final s in ss) {
      groups.putIfAbsent(s.project, () => []).add(s);
    }
    final visiblePaths = layout
        .sorted(groups.keys)
        .where((path) => !layout.hidden.contains(path))
        .toList();
    final allHidden = !recent && ss.isNotEmpty && visiblePaths.isEmpty;
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 8, 12, 0),
          child: Row(
            children: [
              const Expanded(
                child: Row(
                  children: [
                    DeckBrandMark(size: 26),
                    SizedBox(width: 9),
                    Expanded(
                      child: Text(
                        'AgentDeck',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.w600,
                          letterSpacing: -.35,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
              IconButton(
                tooltip: context.tr('设置'),
                onPressed: _agentSettings,
                icon: const Icon(Icons.settings_outlined, size: 20),
              ),
              if (!recent)
                IconButton(
                  tooltip: context.tr('项目管理'),
                  onPressed: c.sessions.isEmpty ? null : _sortProjects,
                  icon: const Icon(Icons.tune_rounded, size: 20),
                ),
              IconButton(
                onPressed: _pair,
                tooltip: context.tr('配对后端'),
                icon: const Icon(Icons.devices_outlined, size: 20),
              ),
              const SizedBox(width: 4),
              IconButton.filled(
                onPressed:
                    c.connected &&
                        !creating &&
                        c.agentInfo(filter).canCreate &&
                        c.agentInfo(filter).configured
                    ? () => _newSession()
                    : null,
                tooltip: context.tr('新建会话'),
                style: IconButton.styleFrom(
                  backgroundColor: ink,
                  foregroundColor: Colors.white,
                  disabledBackgroundColor: DeckColors.panel,
                  disabledForegroundColor: muted,
                ),
                icon: creating
                    ? const RunningIndicator()
                    : const Icon(Icons.add_rounded, size: 22),
              ),
            ],
          ),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 12, 20, 4),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              Expanded(
                child: Text(
                  context.tr('工作空间'),
                  style: TextStyle(
                    fontSize: 22,
                    fontWeight: FontWeight.w600,
                    letterSpacing: -.5,
                    height: 1.2,
                  ),
                ),
              ),
              Flexible(
                child: Padding(
                  padding: const EdgeInsets.only(bottom: 3),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      ConnectionDot(connected: c.connected),
                      const SizedBox(width: 6),
                      Flexible(
                        child: Text(
                          context.tr(c.state),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(color: muted, fontSize: 10),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 10, 20, 4),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                child: Container(
                  padding: const EdgeInsets.all(4),
                  decoration: BoxDecoration(
                    color: DeckColors.panel,
                    borderRadius: BorderRadius.circular(14),
                  ),
                  child: Row(
                    children: [
                      for (final entry in {
                        for (final a in c.visibleAgents) a.id: a.shortName,
                      }.entries)
                        Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 2),
                          child: ChoiceChip(
                            selectedColor: Colors.white,
                            backgroundColor: Colors.transparent,
                            color: WidgetStateProperty.resolveWith(
                              (states) => states.contains(WidgetState.selected)
                                  ? Colors.white
                                  : DeckColors.panel,
                            ),
                            side: BorderSide.none,
                            shape: RoundedRectangleBorder(
                              borderRadius: BorderRadius.circular(10),
                            ),
                            label: Text(
                              entry.value,
                              style: TextStyle(
                                fontSize: 13,
                                fontWeight: FontWeight.w500,
                                color: filter == entry.key ? ink : muted,
                              ),
                            ),
                            avatar: Icon(
                              entry.key == 'codex'
                                  ? Icons.terminal_rounded
                                  : Icons.blur_on_rounded,
                              size: 15,
                              color: filter == entry.key ? ink : muted,
                            ),
                            padding: const EdgeInsets.symmetric(
                              horizontal: 8,
                              vertical: 3,
                            ),
                            selected: filter == entry.key,
                            showCheckmark: false,
                            onSelected: (_) =>
                                setState(() => filter = entry.key),
                          ),
                        ),
                    ],
                  ),
                ),
              ),
              if (filter.isNotEmpty && filter != 'codex' && filter != 'claude')
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text(
                    context.tr(c.agentInfo(filter).detail),
                    style: const TextStyle(fontSize: 11, color: muted),
                  ),
                ),
              const SizedBox(height: 10),
              DeckViewSwitch(
                recent: recent,
                onChanged: (value) => setState(() => recent = value),
              ),
              Padding(
                padding: const EdgeInsets.only(top: 8, bottom: 2),
                child: Align(
                  alignment: Alignment.centerRight,
                  child: Text(
                    context.strings.count(
                      recent ? ss.length : visiblePaths.length,
                      chats: recent,
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(fontSize: 10, color: muted),
                  ),
                ),
              ),
              if (c.error.isNotEmpty)
                Container(
                  margin: const EdgeInsets.only(top: 8, bottom: 8),
                  padding: const EdgeInsets.all(12),
                  decoration: BoxDecoration(
                    color: const Color(0xFFFFEEEB),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Text(
                    context.trError(c.error),
                    style: const TextStyle(
                      color: Color(0xFFBA3F38),
                      fontSize: 12,
                    ),
                  ),
                ),
            ],
          ),
        ),
        Expanded(
          child: ss.isEmpty || allHidden
              ? Center(
                  child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          c.visibleAgents.isEmpty
                              ? context.tr('请选择要显示的 Agent')
                              : allHidden
                              ? context.tr('当前项目已隐藏')
                              : c.device == null && !c.connected
                              ? context.tr('从连接开始')
                              : context.tr('暂无会话'),
                          style: const TextStyle(fontSize: 20),
                        ),
                        const SizedBox(height: 12),
                        Text(
                          allHidden
                              ? context.tr('在项目管理中勾选要显示的项目。')
                              : c.device == null && !c.connected
                              ? context.tr('连接工作站后，按 Agent 和项目管理会话。')
                              : context.tr('点击右上角 + 新建会话，发送消息开始工作。'),
                          textAlign: TextAlign.center,
                          style: const TextStyle(color: muted),
                        ),
                        if (allHidden)
                          TextButton(
                            onPressed: _sortProjects,
                            child: Text(context.tr('管理项目')),
                          )
                        else if (c.device == null && !c.connected)
                          TextButton(
                            onPressed: _pair,
                            child: Text(context.tr('连接工作站')),
                          ),
                      ],
                    ),
                  ),
                )
              : ListView.builder(
                  key: ValueKey('sessions-$recent-$filter'),
                  padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
                  itemCount: recent ? ss.length : visiblePaths.length,
                  itemBuilder: (_, i) => recent
                      ? _sessionRow(ss[i])
                      : _projectGroup(
                          visiblePaths[i],
                          groups[visiblePaths[i]]!,
                        ),
                ),
        ),
      ],
    );
  }

  Widget _device() => ListView(
    padding: const EdgeInsets.all(20),
    children: [
      const SizedBox(height: 18),
      Text(
        context.tr('我的工作站'),
        style: TextStyle(fontSize: 22, fontWeight: FontWeight.w500),
      ),
      const SizedBox(height: 18),
      Container(
        padding: const EdgeInsets.all(20),
        decoration: BoxDecoration(
          color: ink,
          borderRadius: BorderRadius.circular(18),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Row(
              children: [
                Icon(
                  Icons.desktop_windows_outlined,
                  size: 24,
                  color: Colors.white,
                ),
                SizedBox(width: 12),
                Text(
                  'WORKSTATION',
                  style: TextStyle(
                    fontSize: 10,
                    letterSpacing: 2,
                    color: Color(0xFFB8BDC6),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 18),
            Text(
              c.device?['url'] as String? ?? context.tr('尚未连接'),
              style: const TextStyle(
                fontSize: 15,
                color: Colors.white,
                fontWeight: FontWeight.w500,
              ),
            ),
            const SizedBox(height: 14),
            Row(
              children: [
                ConnectionDot(connected: c.connected),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    context.tr(c.state),
                    style: const TextStyle(
                      fontSize: 12,
                      color: Color(0xFFB8BDC6),
                    ),
                  ),
                ),
              ],
            ),
            if (c.device?['relay'] != null)
              Padding(
                padding: EdgeInsets.only(top: 12),
                child: Text(
                  context.tr('网关中继 · 端到端加密'),
                  style: TextStyle(fontSize: 11, color: Color(0xFFB8BDC6)),
                ),
              ),
          ],
        ),
      ),
      const SizedBox(height: 26),
      ListTile(
        contentPadding: EdgeInsets.zero,
        leading: const Icon(Icons.settings_outlined),
        title: Text(context.tr('设置'), style: TextStyle(fontSize: 14)),
        subtitle: Text(
          context.tr('选择首页显示哪些 Agent'),
          style: TextStyle(fontSize: 12),
        ),
        onTap: _agentSettings,
      ),
      ListTile(
        contentPadding: EdgeInsets.zero,
        leading: Icon(Icons.lock_outline),
        title: Text(context.tr('加密传输与本机缓存'), style: TextStyle(fontSize: 14)),
        subtitle: Text(
          context.tr('设备密钥保存在系统安全存储中'),
          style: TextStyle(fontSize: 12),
        ),
      ),
      ListTile(
        contentPadding: EdgeInsets.zero,
        leading: Icon(Icons.autorenew),
        title: Text(context.tr('自动更新会话密钥'), style: TextStyle(fontSize: 14)),
        subtitle: Text(
          context.tr('每次连接独立加密，连接期间定时轮换'),
          style: TextStyle(fontSize: 12),
        ),
      ),
      const SizedBox(height: 18),
      FilledButton(onPressed: _pair, child: Text(context.tr('配对工作站'))),
      if (c.device != null || c.demo) ...[
        const SizedBox(height: 12),
        OutlinedButton(
          onPressed: () async {
            final ok = await showDialog<bool>(
              context: context,
              builder: (ctx) => AlertDialog(
                title: Text(context.tr('忘记这台工作站？')),
                content: Text(context.tr('将清除手机上的设备密钥和加密缓存。后端的设备授权可在工作站撤销。')),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.pop(ctx, false),
                    child: Text(context.tr('取消')),
                  ),
                  TextButton(
                    onPressed: () => Navigator.pop(ctx, true),
                    child: Text(context.tr('忘记')),
                  ),
                ],
              ),
            );
            if (ok == true) await c.forget();
          },
          child: Text(context.tr('忘记工作站')),
        ),
      ],
    ],
  );
}

class StatusLabel extends StatelessWidget {
  final String status;
  const StatusLabel(this.status, {super.key});
  @override
  Widget build(BuildContext context) => Text(
    switch (status) {
      'running' => context.tr('进行中'),
      'pending' => context.tr('已送达'),
      'completed' => context.tr('已完成'),
      'failed' => context.tr('失败'),
      'interrupted' => context.tr('已中断'),
      _ => context.tr('空闲'),
    },
    style: TextStyle(
      fontSize: 12,
      color: status == 'running' || status == 'pending'
          ? const Color(0xFF88611E)
          : muted,
    ),
  );
}

class ProjectOrderPage extends StatelessWidget {
  final AgentClient client;
  final String agent;
  ProjectLayout get layout => client.projectLayout.scoped(agent);
  const ProjectOrderPage({
    super.key,
    required this.client,
    required this.agent,
  });

  Future<void> _save(BuildContext context, ProjectLayout value) async {
    try {
      await client.updateProjectLayout(
        client.projectLayout.withScoped(agent, value),
      );
    } catch (_) {
      if (context.mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.tr('项目布局保存失败，请重试'))));
      }
    }
  }

  void _move(BuildContext context, List<String> paths, int from, int to) {
    final next = paths.toList();
    next.insert(to, next.removeAt(from));
    _save(context, layout.reordered(next));
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: client,
    builder: (context, _) {
      final paths = layout.sorted(
        client.sessions.where((s) => s.agent == agent).map((s) => s.project),
      );
      return Scaffold(
        appBar: AppBar(
          title: Text(context.tr('项目管理')),
          actions: [
            IconButton(
              tooltip: context.tr('恢复默认排序'),
              icon: const Icon(Icons.restart_alt),
              onPressed: () => _save(context, layout.defaultOrder()),
            ),
          ],
        ),
        body: SafeArea(
          top: false,
          child: Column(
            children: [
              Padding(
                padding: EdgeInsets.fromLTRB(20, 8, 20, 16),
                child: Text(
                  context.tr('勾选要显示的项目，拖动右侧把手或用箭头调整顺序。隐藏只影响项目列表，最新会话仍可查看。'),
                  style: TextStyle(color: muted, fontSize: 13),
                ),
              ),
              Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: Wrap(
                  spacing: 12,
                  children: [
                    TextButton(
                      onPressed: () =>
                          _save(context, layout.withHidden(const [])),
                      child: Text(context.tr('全部显示')),
                    ),
                    TextButton(
                      onPressed: () => _save(
                        context,
                        layout.withHidden({...layout.hidden, ...paths}),
                      ),
                      child: Text(context.tr('全部隐藏')),
                    ),
                  ],
                ),
              ),
              Expanded(
                child: ReorderableListView.builder(
                  padding: const EdgeInsets.symmetric(horizontal: 16),
                  buildDefaultDragHandles: false,
                  itemCount: paths.length,
                  onReorder: (from, to) =>
                      _move(context, paths, from, to > from ? to - 1 : to),
                  itemBuilder: (_, i) {
                    final path = paths[i],
                        name = projectDisplayName(context, paths[i]);
                    return Container(
                      key: ValueKey('order-$path'),
                      margin: const EdgeInsets.only(bottom: 8),
                      decoration: BoxDecoration(
                        color: Colors.white,
                        borderRadius: BorderRadius.circular(12),
                      ),
                      child: Row(
                        children: [
                          Checkbox(
                            value: !layout.hidden.contains(path),
                            semanticLabel: context.tr('显示项目 {0}', [name]),
                            onChanged: (_) =>
                                _save(context, layout.toggleHidden(path)),
                          ),
                          Expanded(
                            child: Tooltip(
                              message: path.isEmpty ? context.tr('未归类') : path,
                              child: Text(
                                name,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(fontSize: 16),
                              ),
                            ),
                          ),
                          IconButton(
                            tooltip: context.tr('上移 {0}', [name]),
                            key: ValueKey('up-$path'),
                            onPressed: i == 0
                                ? null
                                : () => _move(context, paths, i, i - 1),
                            icon: const Icon(Icons.arrow_upward, size: 18),
                          ),
                          IconButton(
                            tooltip: context.tr('下移 {0}', [name]),
                            key: ValueKey('down-$path'),
                            onPressed: i == paths.length - 1
                                ? null
                                : () => _move(context, paths, i, i + 1),
                            icon: const Icon(Icons.arrow_downward, size: 18),
                          ),
                          ReorderableDragStartListener(
                            index: i,
                            child: Semantics(
                              label: context.tr('拖动 {0} 调整顺序', [name]),
                              child: Padding(
                                key: ValueKey('drag-$path'),
                                padding: const EdgeInsets.all(14),
                                child: const Icon(
                                  Icons.drag_handle,
                                  size: 22,
                                  color: muted,
                                ),
                              ),
                            ),
                          ),
                        ],
                      ),
                    );
                  },
                ),
              ),
            ],
          ),
        ),
      );
    },
  );
}

class PairPage extends StatefulWidget {
  final AgentClient client;
  const PairPage({super.key, required this.client});
  @override
  State<PairPage> createState() => _PairPageState();
}

class _PairPageState extends State<PairPage> {
  final text = TextEditingController();
  bool busy = false;
  String error = '';
  @override
  void dispose() {
    text.dispose();
    super.dispose();
  }

  Future<void> pair(String value) async {
    if (busy) return;
    setState(() {
      busy = true;
      error = '';
    });
    try {
      await widget.client.pair(value);
      if (mounted) Navigator.pop(context);
    } catch (e) {
      if (mounted) {
        setState(
          () => error = e is FormatException
              ? e.message
              : context.tr('配对失败，请检查配对码、证书和网络'),
        );
      }
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(context.tr('连接工作站'))),
    body: SafeArea(
      child: ListView(
        padding: const EdgeInsets.all(24),
        children: [
          const SizedBox(height: 24),
          Text(
            context.tr('一次配对。\n持续同步。'),
            style: TextStyle(fontSize: 24, height: 1.3),
          ),
          const SizedBox(height: 22),
          Text(
            context.tr('扫描工作站上生成的二维码，或粘贴配对信息。请使用你信任的工作站提供的配对码。'),
            style: TextStyle(color: muted),
          ),
          const SizedBox(height: 24),
          FilledButton.icon(
            onPressed: busy
                ? null
                : () async {
                    await Navigator.push(
                      context,
                      MaterialPageRoute(
                        builder: (_) => GatewayPage(client: widget.client),
                      ),
                    );
                    if (context.mounted && widget.client.connected) {
                      Navigator.pop(context);
                    }
                  },
            icon: const Icon(Icons.account_tree_outlined),
            label: Text(context.tr('通过网关 · 飞书登录')),
          ),
          const SizedBox(height: 32),
          if (Platform.isAndroid || Platform.isIOS || Platform.isMacOS)
            OutlinedButton.icon(
              onPressed: busy
                  ? null
                  : () async {
                      final v = await Navigator.of(context).push<String>(
                        MaterialPageRoute(builder: (_) => const ScanPage()),
                      );
                      if (v != null) await pair(v);
                    },
              icon: const Icon(Icons.qr_code_scanner),
              label: Text(context.tr('扫描配对码')),
            ),
          const SizedBox(height: 24),
          TextField(
            controller: text,
            minLines: 4,
            maxLines: 8,
            autocorrect: false,
            enableSuggestions: false,
            decoration: InputDecoration(
              labelText: context.tr('配对信息'),
              hintText: context.tr('粘贴配对串或 JSON'),
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 20),
          if (error.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 16),
              child: Text(
                context.trError(error),
                style: const TextStyle(color: Color(0xFF9D3C2C)),
              ),
            ),
          FilledButton(
            onPressed: busy ? null : () => pair(text.text),
            child: Text(busy ? context.tr('正在安全配对…') : context.tr('安全连接')),
          ),
        ],
      ),
    ),
  );
}

class ScanPage extends StatefulWidget {
  const ScanPage({super.key});
  @override
  State<ScanPage> createState() => _ScanPageState();
}

class _ScanPageState extends State<ScanPage> {
  final controller = MobileScannerController();
  bool done = false;
  @override
  void dispose() {
    controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(context.tr('扫描配对码'))),
    body: MobileScanner(
      controller: controller,
      onDetect: (capture) {
        if (done || capture.barcodes.isEmpty) return;
        final v = capture.barcodes.first.rawValue;
        if (v != null && v.startsWith('agentdeck://pair/')) {
          done = true;
          Navigator.pop(context, v);
        }
      },
    ),
  );
}

class ConversationPage extends StatefulWidget {
  final AgentClient client;
  final AgentSession session;
  const ConversationPage({
    super.key,
    required this.client,
    required this.session,
  });
  @override
  State<ConversationPage> createState() => _ConversationPageState();
}

class _ConversationPageState extends State<ConversationPage> {
  final scroll = ScrollController();
  final prompt = TextEditingController();
  late AgentSession active;
  bool sending = false;
  String sendStage = '';
  List<Object?> sendStageArgs = const [];
  bool follow = true;
  String? focusedRecord;
  GlobalKey focusAnchor = GlobalKey();
  int focusGeneration = 0;
  bool jumping = false;
  AgentClient get c => widget.client;
  @override
  void initState() {
    super.initState();
    active = widget.session;
    c.addListener(_updated);
    WidgetsBinding.instance.addPostFrameCallback((_) => _updated());
  }

  void _updated() {
    if (follow) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && scroll.hasClients) {
          if (scroll.offset != 0) {
            scroll.jumpTo(0);
          }
        }
      });
    }
  }

  @override
  void dispose() {
    c.removeListener(_updated);
    c.unwatch(active.id).catchError((Object _) {});
    prompt.dispose();
    scroll.dispose();
    super.dispose();
  }

  Future<void> _directory() async {
    final record = await showModalBottomSheet<ConversationEntry>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (sheetContext) => SizedBox(
        height: MediaQuery.sizeOf(sheetContext).height * .85,
        child: Padding(
          padding: EdgeInsets.only(
            bottom: MediaQuery.viewInsetsOf(sheetContext).bottom,
          ),
          child: ConversationOutline(client: c, session: active.id),
        ),
      ),
    );
    if (record == null || !mounted) return;
    setState(() {
      jumping = true;
      follow = false;
    });
    try {
      await c.jumpToRecord(active.id, record.first);
      if (!mounted) return;
      setState(() {
        focusedRecord = record.key;
        focusAnchor = GlobalKey();
        focusGeneration++;
      });
      for (var attempt = 0; attempt < 5; attempt++) {
        await WidgetsBinding.instance.endOfFrame;
        if (!mounted || !scroll.hasClients) return;
        scroll.jumpTo(scroll.position.maxScrollExtent);
        await WidgetsBinding.instance.endOfFrame;
        final target = focusAnchor.currentContext;
        if (target != null && target.mounted) {
          await Scrollable.ensureVisible(target, alignment: 0);
          break;
        }
      }
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.tr('记录定位失败，请重试'))));
      }
    } finally {
      if (mounted) setState(() => jumping = false);
    }
  }

  Future<void> _latest() async {
    setState(() {
      follow = true;
      focusedRecord = null;
    });
    if (c.transcripts[active.id]?.detached == true) {
      try {
        await c.reload(active.id);
      } catch (_) {
        if (mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text(context.tr('最新记录加载失败，请重试'))));
        }
      }
    }
    _updated();
  }

  Future<void> _older() async {
    try {
      await c.loadOlder(active.id);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.tr('历史加载失败，请重试'))));
      }
    }
  }

  Future<bool> submit(
    TurnOptions options,
    List<DraftFile> files, {
    String delivery = 'send',
  }) async {
    final text = prompt.text.trim();
    if (text.isEmpty && files.isEmpty || sending) return false;
    setState(() {
      sending = true;
      sendStageArgs = const [];
      sendStage = !active.managed
          ? (active.agent == 'codex' && c.codexDesktop
                ? '正在连接电脑原会话…'
                : '正在建立续聊会话…')
          : '正在发送…';
    });
    try {
      if (files.any((file) => file.image)) {
        setState(() => sendStage = '正在检查图片支持…');
        await c.validateAttachments(active.agent, active.id, files);
      }
      if (!active.managed) {
        final projects = c.projects
            .where((p) => p.path == active.project)
            .toList();
        if (projects.isEmpty) {
          throw const HttpException('此项目尚未授权，需在后端配置 project-roots');
        }
        active = await c.createSession(
          active.agent,
          projects.first.id,
          parent: active.id,
        );
      }
      if (mounted) {
        setState(
          () =>
              sendStage = active.source == 'desktop' ? '正在发送到电脑原会话…' : '正在发送…',
        );
      }
      if (!options.inherited) {
        await c.saveTurnOptions(active.id, options);
      }
      final uploads = <String>[];
      for (final file in files) {
        uploads.add(
          await c.uploadFile(
            active.id,
            file,
            progress: (done, total) {
              if (mounted) {
                setState(() {
                  sendStage = '上传 {0} {1}%';
                  sendStageArgs = [file.name, (done * 100 / total).round()];
                });
              }
            },
          ),
        );
      }
      if (mounted) {
        setState(
          () =>
              sendStage = active.source == 'desktop' ? '正在发送到电脑原会话…' : '正在发送…',
        );
      }
      await c.sendDelivery(active.id, text, options, uploads, delivery);
      if (mounted && active.source == 'desktop') {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              context.tr(
                delivery == 'queue'
                    ? '已加入 Codex 原生队列'
                    : delivery == 'steer'
                    ? '已引导当前任务'
                    : '已发送到电脑原会话',
              ),
            ),
          ),
        );
      }
      if (mounted) {
        prompt.clear();
      }
      follow = true;
      return true;
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.trError(e))));
      }
      return false;
    } finally {
      if (mounted) setState(() => sending = false);
    }
  }

  Future<void> stop() async {
    try {
      await c.interrupt(active.id);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.trError(e))));
      }
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: c,
    builder: (_, _) {
      final transcript = c.transcripts[active.id];
      final blocks =
          transcript?.blocks
              .where((b) => b.kind != 'queued_message')
              .toList() ??
          <TranscriptBlock>[];
      final latest =
          c.sessions.where((s) => s.id == active.id).firstOrNull ?? active;
      final pending = latest.status == 'pending';
      final running = latest.status == 'running' || pending;
      final showRunningTail = running && transcript?.detached != true;
      return Scaffold(
        backgroundColor: Colors.white,
        appBar: AppBar(
          titleSpacing: 0,
          title: Text(
            sessionDisplayTitle(context, latest),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
          ),
          actions: [
            IconButton(
              tooltip: context.tr('对话目录'),
              onPressed: jumping ? null : _directory,
              icon: const Icon(Icons.format_list_bulleted, size: 21),
            ),
            IconButton(
              tooltip: context.tr('会话附件'),
              onPressed: () => Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (_) =>
                      SessionAttachmentsPage(client: c, session: active.id),
                ),
              ),
              icon: const Icon(Icons.attach_file, size: 21),
            ),
            IconButton(
              tooltip: context.tr('重新载入历史'),
              onPressed: () => c.reload(active.id),
              icon: const Icon(Icons.refresh, size: 21),
            ),
          ],
        ),
        body: Column(
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 2, 20, 14),
              child: Row(
                children: [
                  AgentGlyph(active.agent, size: 24),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      '${projectDisplayName(context, active.project)} · ${agentName(active.agent)}',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(color: muted, fontSize: 12),
                    ),
                  ),
                  if (running) ...[
                    const RunningIndicator(),
                    const SizedBox(width: 7),
                    Flexible(
                      child: Text(
                        context.tr(pending ? '等待电脑响应…' : '执行中'),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(color: muted, fontSize: 12),
                      ),
                    ),
                  ] else
                    Text(
                      c.connected ? context.tr('就绪') : context.tr('连接中'),
                      style: const TextStyle(color: muted, fontSize: 12),
                    ),
                ],
              ),
            ),
            const Divider(height: 1),
            Expanded(
              child: Stack(
                children: [
                  NotificationListener<ScrollNotification>(
                    onNotification: (notification) {
                      if (notification.depth == 0 &&
                          (notification is ScrollUpdateNotification &&
                                  notification.dragDetails != null ||
                              notification is UserScrollNotification)) {
                        final v =
                            !jumping && notification.metrics.extentBefore < 140;
                        if (!v &&
                            !jumping &&
                            notification.metrics.extentAfter < 200 &&
                            transcript?.hasMore == true) {
                          _older();
                        }
                        if (v != follow) setState(() => follow = v);
                      }
                      return false;
                    },
                    child: blocks.isEmpty && !running
                        ? Center(
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                const Icon(
                                  Icons.chat_bubble_outline_rounded,
                                  size: 30,
                                  color: muted,
                                ),
                                const SizedBox(height: 14),
                                Text(
                                  active.managed
                                      ? context.tr('发送消息，开始一起工作')
                                      : context.tr('正在读取会话…'),
                                  style: const TextStyle(
                                    color: muted,
                                    fontSize: 14,
                                  ),
                                ),
                              ],
                            ),
                          )
                        : ListView.builder(
                            key: ValueKey(
                              'transcript-${active.id}-$focusGeneration',
                            ),
                            controller: scroll,
                            reverse: true,
                            keyboardDismissBehavior:
                                ScrollViewKeyboardDismissBehavior.onDrag,
                            padding: const EdgeInsets.fromLTRB(20, 16, 20, 28),
                            itemCount:
                                blocks.length +
                                (showRunningTail ? 1 : 0) +
                                (transcript?.hasMore == true ? 1 : 0),
                            itemBuilder: (_, index) {
                              if (index ==
                                  blocks.length + (showRunningTail ? 1 : 0)) {
                                return Center(
                                  child: TextButton.icon(
                                    onPressed: transcript!.loadingOlder
                                        ? null
                                        : _older,
                                    icon: transcript.loadingOlder
                                        ? const RunningIndicator()
                                        : const Icon(Icons.history, size: 18),
                                    label: Text(
                                      transcript.loadingOlder
                                          ? context.tr('正在加载…')
                                          : context.tr('加载更早消息'),
                                    ),
                                  ),
                                );
                              }
                              final i =
                                  blocks.length -
                                  1 -
                                  index +
                                  (showRunningTail ? 1 : 0);
                              return Align(
                                alignment: Alignment.topCenter,
                                child: ConstrainedBox(
                                  constraints: const BoxConstraints(
                                    maxWidth: 760,
                                  ),
                                  child: i == blocks.length
                                      ? Padding(
                                          padding: EdgeInsets.symmetric(
                                            vertical: 12,
                                          ),
                                          child: Row(
                                            children: [
                                              RunningIndicator(),
                                              SizedBox(width: 10),
                                              Expanded(
                                                child: Text(
                                                  context.tr(
                                                    pending
                                                        ? '等待电脑响应…'
                                                        : '正在执行…',
                                                  ),
                                                  style: TextStyle(
                                                    fontSize: 13,
                                                    color: muted,
                                                  ),
                                                ),
                                              ),
                                            ],
                                          ),
                                        )
                                      : Container(
                                          key: blocks[i].key == focusedRecord
                                              ? focusAnchor
                                              : null,
                                          decoration:
                                              blocks[i].key == focusedRecord
                                              ? BoxDecoration(
                                                  color: const Color(
                                                    0x10EF633C,
                                                  ),
                                                  border: Border(
                                                    left: BorderSide(
                                                      color: accent,
                                                      width: 2,
                                                    ),
                                                  ),
                                                )
                                              : null,
                                          padding:
                                              blocks[i].key == focusedRecord
                                              ? const EdgeInsets.only(left: 10)
                                              : null,
                                          child: TranscriptTile(
                                            key: ValueKey(blocks[i].key),
                                            block: blocks[i],
                                            session: active.id,
                                            client: c,
                                          ),
                                        ),
                                ),
                              );
                            },
                          ),
                  ),
                  if (jumping)
                    const Positioned(
                      top: 8,
                      right: 16,
                      child: RunningIndicator(),
                    ),
                  if (!follow || transcript?.detached == true)
                    Positioned(
                      bottom: 12,
                      right: 16,
                      child: IconButton.filled(
                        tooltip: context.tr('回到最新消息'),
                        style: IconButton.styleFrom(
                          backgroundColor: Colors.white,
                          foregroundColor: ink,
                          side: const BorderSide(color: DeckColors.line),
                        ),
                        onPressed: _latest,
                        icon: const Icon(Icons.arrow_downward, size: 20),
                      ),
                    ),
                ],
              ),
            ),
            SafeArea(
              top: false,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    if (sending ||
                        !active.managed ||
                        active.source == 'desktop')
                      Padding(
                        padding: EdgeInsets.only(bottom: 8),
                        child: Text(
                          sending
                              ? context.tr(sendStage, sendStageArgs)
                              : (active.source == 'desktop' ||
                                        active.agent == 'codex' &&
                                            c.codexDesktop
                                    ? context.tr('发送到电脑 Codex 的同一个会话')
                                    : context.tr('发送后复制电脑会话上下文，在手机继续')),
                          style: TextStyle(fontSize: 11, color: muted),
                        ),
                      ),
                    if ((active.managed ||
                            active.agent == 'codex' && c.codexDesktop) &&
                        (active.agent == 'codex' || active.agent == 'claude'))
                      MessageQueue(
                        key: ValueKey('queue-${active.id}'),
                        client: c,
                        session: active.id,
                        agent: active.agent,
                        running: running,
                      ),
                    InputComposer(
                      client: c,
                      session: active.id,
                      agent: active.agent,
                      controller: prompt,
                      sending: sending,
                      running:
                          running &&
                          (active.managed ||
                              active.agent == 'codex' && c.codexDesktop),
                      canSend: c.agentInfo(active.agent).canSend,
                      onSend: submit,
                      onDelivery: (options, files, mode) =>
                          submit(options, files, delivery: mode),
                      onStop:
                          active.managed &&
                              active.source != 'desktop' &&
                              c.agentInfo(active.agent).canStop
                          ? stop
                          : null,
                    ),
                  ],
                ),
              ),
            ),
          ],
        ),
      );
    },
  );
}

class TranscriptTile extends StatefulWidget {
  final TranscriptBlock block;
  final AgentClient client;
  final String session;
  const TranscriptTile({
    super.key,
    required this.block,
    required this.client,
    this.session = '',
  });
  @override
  State<TranscriptTile> createState() => _TranscriptTileState();
}

class _TranscriptTileState extends State<TranscriptTile> {
  TranscriptBlock get block => widget.block;
  AgentClient get client => widget.client;
  String get session => widget.session;
  Widget? _rendered;
  String? _text, _status, _session;
  bool? _connected;
  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _rendered = null;
  }

  @override
  Widget build(BuildContext context) {
    // Reuse unchanged Markdown subtrees during stream ticks and typing.
    if (_rendered == null ||
        _text != block.text ||
        _status != block.status ||
        _session != session ||
        _connected != client.connected) {
      _text = block.text;
      _status = block.status;
      _session = session;
      _connected = client.connected;
      _rendered = _body(context);
    }
    return _rendered!;
  }

  Widget markdown(BuildContext context) => MarkdownBody(
    data: block.text,
    selectable: true,
    imageBuilder: (uri, title, alt) => mediaHref(uri.toString())
        ? ListTile(
            leading: Icon(
              audioHref(uri.toString())
                  ? Icons.headphones_rounded
                  : Icons.play_circle_outline,
            ),
            title: Text(
              alt ?? context.tr(audioHref(uri.toString()) ? '音频' : '视频'),
            ),
            onTap: () =>
                openSessionLink(context, client, session, uri.toString()),
          )
        : Padding(
            padding: const EdgeInsets.symmetric(vertical: 8),
            child: EncryptedImage(
              client: client,
              session: session,
              href: uri.toString(),
            ),
          ),
    onTapLink: (text, href, title) {
      if (href != null) {
        openSessionLink(
          context,
          client,
          session,
          href,
          onSession: (target) async {
            await client.watch(target.id);
            if (!context.mounted) return;
            await Navigator.push(
              context,
              MaterialPageRoute<void>(
                builder: (_) =>
                    ConversationPage(client: client, session: target),
              ),
            );
            if (context.mounted) await client.watch(session);
          },
        );
      }
    },
    styleSheet: MarkdownStyleSheet(
      p: const TextStyle(fontSize: 14, height: 1.65, color: Color(0xFF2B3037)),
      h1: const TextStyle(
        fontSize: 19,
        height: 1.4,
        fontWeight: FontWeight.w600,
        color: ink,
      ),
      h2: const TextStyle(
        fontSize: 17,
        height: 1.4,
        fontWeight: FontWeight.w600,
        color: ink,
      ),
      h3: const TextStyle(
        fontSize: 15,
        height: 1.4,
        fontWeight: FontWeight.w600,
        color: ink,
      ),
      a: const TextStyle(
        color: Color(0xFF326994),
        decoration: TextDecoration.underline,
      ),
      blockquote: const TextStyle(color: muted, height: 1.6),
      blockquoteDecoration: const BoxDecoration(
        color: DeckColors.paper,
        border: Border(left: BorderSide(color: DeckColors.line, width: 3)),
      ),
      blockSpacing: 14,
      code: const TextStyle(fontFamily: 'monospace', fontSize: 13, color: ink),
      codeblockDecoration: BoxDecoration(
        color: DeckColors.paper,
        borderRadius: BorderRadius.circular(10),
      ),
      codeblockPadding: const EdgeInsets.all(14),
    ),
  );
  Widget _body(BuildContext context) {
    if (block.kind == 'approval') {
      return ApprovalTile(block: block, client: client, session: session);
    }
    if (block.kind == 'queued_message') {
      final label = switch (block.role) {
        'queued' => context.tr('排队中'),
        'dispatching' => context.tr('正在发送…'),
        'sent' => context.tr('队列消息已发送'),
        'cancelled' => context.tr('已取消'),
        'unknown' => context.tr('发送结果待确认，请检查电脑会话'),
        _ => context.tr('排队中'),
      };
      return Container(
        margin: const EdgeInsets.symmetric(vertical: 8),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: DeckColors.paper,
          borderRadius: BorderRadius.circular(12),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.playlist_play, size: 18),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    label,
                    style: const TextStyle(fontSize: 12, color: muted),
                  ),
                ),
                if (block.role == 'queued' || block.role == 'unknown')
                  IconButton(
                    tooltip: context.tr(
                      block.role == 'unknown' ? '移除待确认消息' : '取消排队',
                    ),
                    icon: const Icon(Icons.close, size: 18),
                    onPressed: () async {
                      try {
                        await client.cancelDelivery(
                          session,
                          block.key
                              .replaceFirst(RegExp(r':queued_message$'), '')
                              .replaceFirst('queue/', ''),
                        );
                      } catch (e) {
                        if (context.mounted) {
                          ScaffoldMessenger.of(context).showSnackBar(
                            SnackBar(content: Text(context.trError(e))),
                          );
                        }
                      }
                    },
                  ),
              ],
            ),
            if (block.text.isNotEmpty)
              Text(block.text, maxLines: 8, overflow: TextOverflow.ellipsis),
          ],
        ),
      );
    }
    if (block.kind == 'image') {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 14),
        child: EncryptedImage(
          client: client,
          id: block.media,
          session: session,
          href: block.media.isEmpty ? block.text : '',
        ),
      );
    }
    if ([
      'thinking',
      'tool_call',
      'tool_result',
      'file_change',
    ].contains(block.kind)) {
      final title = switch (block.kind) {
        'thinking' => context.tr('思考摘要'),
        'tool_call' => context.tr('工具调用'),
        'tool_result' => context.tr('工具输出'),
        _ => context.tr('文件变更'),
      };
      return Container(
        margin: const EdgeInsets.only(bottom: 8),
        decoration: BoxDecoration(
          color: DeckColors.paper,
          borderRadius: BorderRadius.circular(14),
          border: Border.all(color: DeckColors.line, width: .6),
        ),
        child: ExpansionTile(
          shape: const Border(),
          collapsedShape: const Border(),
          dense: true,
          tilePadding: const EdgeInsets.symmetric(horizontal: 12),
          childrenPadding: const EdgeInsets.fromLTRB(14, 0, 14, 16),
          title: Text(
            block.kind == 'tool_call'
                ? '$title · ${block.text.split('\n').first}'
                : title,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w500,
              color: muted,
            ),
          ),
          leading: Icon(
            block.kind == 'thinking' ? Icons.lightbulb_outline : Icons.terminal,
            size: 17,
            color: muted,
          ),
          children: [
            Align(
              alignment: Alignment.centerLeft,
              child: block.kind == 'thinking'
                  ? markdown(context)
                  : SelectableText(
                      block.text,
                      style: const TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 12,
                        height: 1.6,
                      ),
                    ),
            ),
          ],
        ),
      );
    }
    if (block.kind == 'notice') {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 10),
        child: Text(
          context.strings.notice(block.text),
          style: const TextStyle(color: muted, fontSize: 12),
        ),
      );
    }
    final user = block.role == 'user';
    return Padding(
      padding: EdgeInsets.only(top: user ? 10 : 14, bottom: 20),
      child: Column(
        crossAxisAlignment: user
            ? CrossAxisAlignment.end
            : CrossAxisAlignment.start,
        children: [
          if (!user) ...[
            Row(
              children: [
                AgentGlyph(
                  client.sessions
                          .where((s) => s.id == session)
                          .firstOrNull
                          ?.agent ??
                      'codex',
                  size: 24,
                ),
                const SizedBox(width: 7),
                Text(
                  agentName(
                    client.sessions
                            .where((s) => s.id == session)
                            .firstOrNull
                            ?.agent ??
                        'codex',
                  ),
                  style: const TextStyle(
                    fontSize: 11,
                    color: muted,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 10),
          ],
          if (user)
            Align(
              alignment: Alignment.centerRight,
              child: FractionallySizedBox(
                widthFactor: .9,
                child: Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 16,
                    vertical: 12,
                  ),
                  decoration: BoxDecoration(
                    color: DeckColors.panel,
                    borderRadius: const BorderRadius.only(
                      topLeft: Radius.circular(18),
                      topRight: Radius.circular(18),
                      bottomLeft: Radius.circular(18),
                      bottomRight: Radius.circular(6),
                    ),
                  ),
                  child: markdown(context),
                ),
              ),
            )
          else
            markdown(context),
        ],
      ),
    );
  }
}

class ApprovalTile extends StatefulWidget {
  final TranscriptBlock block;
  final AgentClient client;
  final String session;
  const ApprovalTile({
    super.key,
    required this.block,
    required this.client,
    required this.session,
  });
  @override
  State<ApprovalTile> createState() => _ApprovalTileState();
}

class _ApprovalTileState extends State<ApprovalTile> {
  bool busy = false;
  final Map<String, String> answers = {};
  Future<void> decide(String decision) async {
    setState(() => busy = true);
    try {
      await widget.client.approve(
        widget.session,
        widget.block.key.replaceFirst(RegExp(r':approval$'), ''),
        decision,
        answer: jsonEncode(answers),
      );
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.trError(e))));
      }
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final b = widget.block;
    if (b.status != 'pending') return const SizedBox.shrink();
    Map<String, dynamic> params = {};
    try {
      params = Map<String, dynamic>.from(jsonDecode(b.text));
    } catch (_) {}
    final questions =
        (params['questions'] ??
                (params['input'] is Map ? params['input']['questions'] : null))
            as List? ??
        [];
    final title =
        (params['tool_name'] ??
                params['command'] ??
                params['reason'] ??
                context.tr('Agent 请求操作许可'))
            .toString();
    final description =
        (params['description'] ??
                params['reason'] ??
                params['input'] ??
                params['permissions'] ??
                '')
            .toString();
    return Card(
      color: const Color(0xFFFFF3DD),
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              context.tr('需要你的确认'),
              style: TextStyle(fontWeight: FontWeight.w600),
            ),
            const SizedBox(height: 8),
            SelectableText(
              title,
              style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w500),
            ),
            if (description.isNotEmpty)
              ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 200),
                child: SingleChildScrollView(
                  child: SelectableText(
                    description,
                    style: const TextStyle(fontSize: 12),
                  ),
                ),
              ),
            for (final q in questions)
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(q['question']?.toString() ?? ''),
                    TextFormField(
                      key: ValueKey(
                        '${q['id'] ?? q['question']}:${answers[(q['id'] ?? q['question']).toString()]}',
                      ),
                      initialValue:
                          answers[(q['id'] ?? q['question']).toString()] ?? '',
                      decoration: InputDecoration(
                        labelText: context.tr('你的回答'),
                      ),
                      onChanged: (v) =>
                          answers[(q['id'] ?? q['question']).toString()] = v,
                    ),

                    Wrap(
                      spacing: 8,
                      children: [
                        for (final o in q['options'] as List? ?? [])
                          ActionChip(
                            label: Text(o['label']?.toString() ?? ''),
                            onPressed: () {
                              setState(
                                () =>
                                    answers[(q['id'] ?? q['question'])
                                            .toString()] =
                                        o['label']?.toString() ?? '',
                              );
                            },
                          ),
                      ],
                    ),
                  ],
                ),
              ),
            const SizedBox(height: 10),
            Row(
              children: [
                TextButton(
                  onPressed: busy ? null : () => decide('decline'),
                  child: Text(context.tr('拒绝')),
                ),
                const Spacer(),
                FilledButton(
                  onPressed: busy ? null : () => decide('accept'),
                  child: Text(
                    busy
                        ? context.tr('提交中…')
                        : questions.isEmpty
                        ? context.tr('允许此次')
                        : context.tr('提交回答'),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class EncryptedImage extends StatefulWidget {
  final AgentClient client;
  final String id, session, href;
  const EncryptedImage({
    super.key,
    required this.client,
    this.id = '',
    this.session = '',
    this.href = '',
  });
  @override
  State<EncryptedImage> createState() => _EncryptedImageState();
}

class _EncryptedImageState extends State<EncryptedImage> {
  Future<Uint8List> load() => widget.href.isEmpty
      ? widget.client.image(widget.id)
      : widget.client
            .attachment(widget.session, widget.href)
            .then((a) => a.bytes);
  late Future<Uint8List> data = load();
  @override
  void didUpdateWidget(EncryptedImage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.id != widget.id ||
        oldWidget.session != widget.session ||
        oldWidget.href != widget.href ||
        oldWidget.client != widget.client) {
      data = load();
    }
  }

  @override
  Widget build(BuildContext context) => FutureBuilder<dynamic>(
    future: data,
    builder: (_, snap) {
      if (snap.hasError) {
        return TextButton(
          onPressed: () => setState(() {
            data = load();
          }),
          child: Text(
            context.tr('{0}，点击重试', [context.tr(attachmentError(snap.error))]),
          ),
        );
      }
      if (!snap.hasData) {
        return const SizedBox(
          height: 120,
          child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
        );
      }
      return GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTap: () => Navigator.push(
          context,
          MaterialPageRoute<void>(
            builder: (_) => Scaffold(
              backgroundColor: ink,
              appBar: AppBar(
                title: Text(context.tr('图片预览')),
                backgroundColor: ink,
                foregroundColor: Colors.white,
              ),
              body: InteractiveViewer(
                minScale: .5,
                maxScale: 6,
                child: Center(
                  child: Image.memory(
                    snap.data,
                    errorBuilder: (_, _, _) => Text(context.tr('图片无法解码')),
                  ),
                ),
              ),
            ),
          ),
        ),
        child: Semantics(
          button: true,
          label: context.tr('会话图片，点击放大'),
          child: ConstrainedBox(
            constraints: const BoxConstraints(
              minHeight: 64,
              minWidth: 64,
              maxHeight: 320,
            ),
            child: ClipRRect(
              borderRadius: BorderRadius.circular(16),
              child: Image.memory(
                snap.data,
                fit: BoxFit.contain,
                errorBuilder: (_, _, _) => Text(context.tr('图片无法解码')),
              ),
            ),
          ),
        ),
      );
    },
  );
}
