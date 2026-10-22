import 'core/permissions.dart';
import 'language.dart';
import 'dart:io';
import 'dart:async';
import 'core/local_speech.dart';
import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart'
    show LicenseRegistry, LicenseEntryWithLineBreaks;
import 'package:flutter/services.dart'
    show rootBundle, Clipboard, ClipboardData;
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'core/client.dart';
import 'gateway_page.dart';
import 'core/model.dart';
import 'attachment_page.dart';
import 'download_page.dart';
import 'media_preview.dart';
import 'conversation_outline.dart';
import 'dart:typed_data';
import 'design.dart';
import 'agent_settings.dart';
import 'core/display.dart';
import 'display_setting.dart';
import 'core/agents.dart';
import 'core/input.dart';
import 'input_composer.dart';
import 'message_queue.dart';
import 'running_indicator.dart';
import 'usage_sheet.dart';
import 'goal_panel.dart';
import 'app_lock.dart';
import 'activity_group.dart';
import 'app_update_page.dart';
import 'progressive_text.dart';
import 'approval_tile.dart';
export 'approval_tile.dart' show ApprovalTile;
import 'reading_scroll_view.dart';
import 'core/conversation_presentation.dart';
import 'core/outbox.dart';
import 'outbox_page.dart';
import 'client_status.dart';
import 'loading_image.dart';
export 'running_indicator.dart';

const ink = DeckColors.ink;
const paper = DeckColors.paper;
const muted = DeckColors.muted;
const accent = DeckColors.accent;
final deckRoutes = RouteObserver<ModalRoute<dynamic>>();

Widget unreadDot(BuildContext context, String id) => Semantics(
  label: context.tr('执行完成，未读'),
  child: Tooltip(
    message: context.tr('执行完成，未读'),
    child: Container(
      key: ValueKey('unread-$id'),
      width: 8,
      height: 8,
      decoration: const BoxDecoration(
        color: Color(0xFF3884FF),
        shape: BoxShape.circle,
      ),
    ),
  ),
);

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
  final DisplayController? display;
  const AgentDeckApp({super.key, this.client, this.languages, this.display});
  @override
  State<AgentDeckApp> createState() => _AgentDeckAppState();
}

class _AgentDeckAppState extends State<AgentDeckApp>
    with WidgetsBindingObserver {
  late final AgentClient client = widget.client ?? AgentClient();
  late final LanguageController languages =
      widget.languages ?? LanguageController(client.vault);
  late final DisplayController display =
      widget.display ?? DisplayController(client.vault);
  @override
  void initState() {
    super.initState();
    unawaited(client.appLock.load());
    if (widget.languages == null) unawaited(languages.load());
    if (widget.display == null) unawaited(display.load());
    WidgetsBinding.instance.addObserver(this);
    if (widget.client == null) {
      client.initialize();
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    if (widget.languages == null) languages.dispose();
    if (widget.display == null) display.dispose();
    if (widget.client == null) client.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    client.setReadingForeground(state == AppLifecycleState.resumed);
    if (state == AppLifecycleState.paused) {
      client.suspend();
      client.updates.background();
    }
    if (state == AppLifecycleState.resumed) {
      client.resume();
      unawaited(client.updates.foreground());
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: languages,
    builder: (context, _) => LanguageScope(
      controller: languages,
      child: DisplayScope(
        controller: display,
        child: MaterialApp(
          debugShowCheckedModeBanner: false,
          title: 'AgentDeck',
          navigatorObservers: [deckRoutes],
          theme: deckTheme(),
          locale: languages.language.locale,
          supportedLocales: UiStrings.supportedLocales,
          localizationsDelegates: UiStrings.delegates,
          builder: (context, child) => DisplayScale(
            controller: display,
            child: AppLockScope(
              controller: client.appLock,
              child: AppLockGate(controller: client.appLock, child: child!),
            ),
          ),
          home: DeckHome(client: client),
        ),
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
    String permissionMode = '';
    final ok = await showDialog<bool>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, set) => AlertDialog(
          title: Text(context.tr('新建会话')),
          content: SingleChildScrollView(
            child: Column(
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
                        (a) =>
                            DropdownMenuItem(value: a.id, child: Text(a.name)),
                      )
                      .toList(),
                  onChanged: (v) => set(() {
                    agent = v!;
                    permissionMode = '';
                  }),
                ),
                if (creationPermissions(agent).isNotEmpty) ...[
                  const SizedBox(height: 20),
                  DropdownButtonFormField<String>(
                    key: ValueKey('create-permission-$agent'),
                    initialValue: permissionMode,
                    isExpanded: true,
                    decoration: InputDecoration(labelText: context.tr('权限')),
                    items: [
                      DropdownMenuItem(
                        value: '',
                        child: Text(context.tr('工作站默认权限')),
                      ),
                      for (final choice in creationPermissions(agent))
                        DropdownMenuItem(
                          value: choice.id,
                          child: Text(choice.label),
                        ),
                    ],
                    onChanged: (v) => set(() => permissionMode = v ?? ''),
                  ),
                ],
              ],
            ),
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
      final session = await c.createSession(
        agent,
        selected,
        permissionMode: permissionMode,
      );
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
    unawaited(
      c.watch(session.id).catchError((Object e) {
        if (mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text(context.trError(e))));
        }
      }),
    );
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
                      if (c.goals
                              .where(
                                (g) => g.session == session.id && g.isCurrent,
                              )
                              .firstOrNull
                          case final goal?)
                        Text(
                          context.tr(goal.label),
                          style: const TextStyle(fontSize: 10, color: muted),
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
                    if (c.live &&
                        (session.status == 'running' ||
                            session.status == 'pending'))
                      const RunningIndicator()
                    else if (c.live && session.status == 'waiting')
                      Tooltip(
                        message: context.tr('等待确认'),
                        child: const Icon(
                          Icons.hourglass_top,
                          size: 17,
                          color: muted,
                        ),
                      )
                    else if (c.isUnread(session))
                      unreadDot(context, session.id)
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
        c.live && !creating && c.projects.any((p) => p.path == path);
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
                if (sessions.any(c.isUnread)) ...[
                  unreadDot(context, 'project-$path'),
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

  Future<void> _refreshHome() async {
    try {
      await c.refreshSessions();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              context.trError(e is TimeoutException ? '刷新超时，请检查连接后重试' : e),
            ),
          ),
        );
      }
    }
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
        UpdateNotice(updater: c.updates),
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
                tooltip: context.tr('下载列表'),
                onPressed: () => Navigator.push(
                  context,
                  MaterialPageRoute<void>(
                    builder: (_) => DownloadPage(client: c),
                  ),
                ),
                icon: const Icon(Icons.downloading_outlined, size: 20),
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
                    c.live &&
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
                child: ClientStatusButton(client: c, onPair: _pair),
              ),
            ],
          ),
        ),
        OutboxSummary(client: c),
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
                padding: const EdgeInsets.only(top: 2, bottom: 2),
                child: Row(
                  key: const ValueKey('session-meta-row'),
                  children: [
                    if (filter == 'codex' || filter == 'claude')
                      TextButton.icon(
                        key: const ValueKey('account-usage'),
                        style: TextButton.styleFrom(
                          padding: const EdgeInsets.symmetric(horizontal: 8),
                          minimumSize: const Size(0, 36),
                          tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                          textStyle: const TextStyle(fontSize: 11),
                          foregroundColor: muted,
                        ),
                        onPressed: !c.connected
                            ? null
                            : () => showModalBottomSheet<void>(
                                context: context,
                                isScrollControlled: true,
                                showDragHandle: true,
                                builder: (_) => FractionallySizedBox(
                                  heightFactor: .72,
                                  child: UsageSheet(client: c, agent: filter),
                                ),
                              ),
                        icon: const Icon(Icons.data_usage_rounded, size: 14),
                        label: Text(context.tr('用量')),
                      ),
                    if (c.goals.any((g) => g.agent == filter && g.isCurrent) ||
                        c.goalError.isNotEmpty)
                      TextButton.icon(
                        key: const ValueKey('agent-goals'),
                        onPressed: () => showModalBottomSheet<void>(
                          context: context,
                          isScrollControlled: true,
                          showDragHandle: true,
                          builder: (_) => FractionallySizedBox(
                            heightFactor: .75,
                            child: ListenableBuilder(
                              listenable: c,
                              builder: (context, _) => ListView(
                                children: [
                                  ListTile(
                                    title: Text(context.tr('目标')),
                                    trailing: IconButton(
                                      tooltip: context.tr('刷新目标'),
                                      onPressed: c.connected
                                          ? c.refreshGoals
                                          : null,
                                      icon: const Icon(Icons.refresh),
                                    ),
                                  ),
                                  if (c.goalError.isNotEmpty)
                                    Padding(
                                      padding: const EdgeInsets.all(16),
                                      child: Text(context.trError(c.goalError)),
                                    ),
                                  for (final g in c.goals.where(
                                    (g) => g.agent == filter && g.isCurrent,
                                  ))
                                    GoalPanel(
                                      client: c,
                                      goal: g,
                                      open: () {
                                        final session = c.sessions
                                            .where((s) => s.id == g.session)
                                            .firstOrNull;
                                        if (session != null) {
                                          Navigator.pop(context);
                                          _open(session);
                                        }
                                      },
                                    ),
                                ],
                              ),
                            ),
                          ),
                        ),
                        style: TextButton.styleFrom(
                          minimumSize: const Size(0, 36),
                          padding: const EdgeInsets.symmetric(horizontal: 8),
                          tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                          foregroundColor: muted,
                          textStyle: const TextStyle(fontSize: 11),
                        ),
                        icon: const Icon(Icons.flag_outlined, size: 14),
                        label: Text(
                          '${context.tr('目标')} · ${c.goals.where((g) => g.agent == filter && g.isCurrent).length}',
                        ),
                      ),
                    const Spacer(),
                    Text(
                      key: const ValueKey('session-count'),
                      context.strings.count(
                        recent ? ss.length : visiblePaths.length,
                        chats: recent,
                      ),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 10, color: muted),
                    ),
                  ],
                ),
              ),
              ClientBlockingNotice(client: c, onPair: _pair),
            ],
          ),
        ),
        Expanded(
          child: RefreshIndicator(
            key: const ValueKey('home-refresh'),
            onRefresh: _refreshHome,
            color: ink,
            child: ss.isEmpty || allHidden
                ? LayoutBuilder(
                    builder: (context, constraints) => SingleChildScrollView(
                      physics: const AlwaysScrollableScrollPhysics(),
                      child: ConstrainedBox(
                        constraints: BoxConstraints(
                          minHeight: constraints.maxHeight,
                        ),
                        child: Center(
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
                        ),
                      ),
                    ),
                  )
                : ListView.builder(
                    physics: const AlwaysScrollableScrollPhysics(),
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
            ClientStatusButton(client: c, onPair: _pair, light: true),
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
      const SizedBox(height: 12),
      ListTile(
        leading: const Icon(Icons.outbox_outlined),
        title: Text(context.tr('发件箱')),
        onTap: () => Navigator.push(
          context,
          MaterialPageRoute<void>(builder: (_) => OutboxPage(client: c)),
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

String sessionStatusText(BuildContext context, AgentSession s) => context.tr(
  s.issue != null
      ? '需要处理'
      : switch (s.status) {
          'failed' => '执行失败',
          'interrupted' => '已中断',
          'unknown' => '状态未知',
          'waiting' => '等待确认',
          _ => '就绪',
        },
);

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
      'unknown' => context.tr('状态未知'),
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

class _ConversationPageState extends State<ConversationPage> with RouteAware {
  ModalRoute<dynamic>? readingRoute;
  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final route = ModalRoute.of(context);
    if (readingRoute != route) {
      deckRoutes.unsubscribe(this);
      readingRoute = route;
      if (route != null) deckRoutes.subscribe(this, route);
    }
    _becameVisible();
  }

  void _becameVisible() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && readingRoute?.isCurrent == true) {
        c.setReadingSession(active.id);
      }
    });
  }

  void _becameHidden() {
    if (c.sessionReads.visible == active.id) c.setReadingSession('');
  }

  @override
  void didPush() => _becameVisible();
  @override
  void didPopNext() => _becameVisible();
  @override
  void didPushNext() => _becameHidden();
  @override
  void didPop() => _becameHidden();
  final scroll = ReadingScrollController();
  final readingView = GlobalKey<ReadingScrollViewState>();
  bool userScrolling = false;
  int pagingDirection =
      0; // Positive offsets move into older records (reverse view).
  bool pagingScheduled = false, pagingBusy = false;
  int seekingDialogue = 0;
  bool initialDialogueCheckDone = false;

  final prompt = TextEditingController();
  late AgentSession active;
  bool savingMessage = false;
  List<OutgoingMessage> get outgoing => c.outbox.forSession(active.id);
  bool get sending => savingMessage || outgoing.any((t) => t.running);
  String get sendStage =>
      outgoing.where((t) => t.pending).lastOrNull?.stage ?? '正在保存消息…';
  List<Object?> get sendStageArgs => const [];
  List<InputReceipt> get inputReceipts =>
      outgoing.where((t) => t.showReceipt).map((t) => t.receipt).toList();
  InputReceipt? get receipt => inputReceipts.lastOrNull;

  Transcript? _sectionTranscript;
  int _sectionRevision = -1;
  List<TranscriptBlock> _sectionBlocks = [];
  List<TranscriptSection> _sections = [];
  bool expandAllActivities = false;
  final activityOverrides = <String, bool>{};
  bool get _foldedPaging =>
      !expandAllActivities &&
      !_sections.any(
        (section) => section.activity && _activityExpanded(section),
      );
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
    active = c.sessions.where((s) => s.id == active.id).firstOrNull ?? active;
    if (readingRoute?.isCurrent == true) {
      final moved = outgoing.where((t) => t.session != active.id).lastOrNull;
      if (moved != null) {
        final target = c.sessions
            .where((s) => s.id == moved.session)
            .firstOrNull;
        if (target != null) {
          active = target;
          unawaited(c.watch(target.id).catchError((Object _) {}));
        }
      }
    }
    _schedulePaging();
    if (follow) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted &&
            follow &&
            !jumping &&
            c.transcripts[active.id]?.detached != true &&
            scroll.hasClients &&
            !scroll.position.isScrollingNotifier.value) {
          if (scroll.offset != scroll.position.minScrollExtent) {
            scroll.jumpTo(scroll.position.minScrollExtent);
          }
        }
      });
    }
  }

  void _schedulePaging() {
    final current = c.transcripts[active.id];
    // A first page may contain only folded execution records. Fill it with a
    // real dialogue message without requiring a gesture on an empty viewport.
    if (!initialDialogueCheckDone &&
        current != null &&
        current.blocks.isNotEmpty) {
      initialDialogueCheckDone = true;
      if (pagingDirection == 0 &&
          _foldedPaging &&
          !current.blocks.any(_isDialogue)) {
        pagingDirection = current.hasMore ? 1 : (current.detached ? -1 : 0);
        seekingDialogue = pagingDirection;
      }
    }
    if (pagingScheduled || pagingBusy || pagingDirection == 0 || jumping) {
      return;
    }
    pagingScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      pagingScheduled = false;
      if (!mounted ||
          !scroll.hasClients ||
          jumping ||
          pagingBusy ||
          !c.connected) {
        return;
      }
      final t = c.transcripts[active.id];
      if (t == null || t.loadingOlder || t.loadingNewer) return;
      final direction = pagingDirection;
      final wasSeekingDialogue = seekingDialogue == direction;
      final atEdge = direction > 0
          ? t.hasMore && scroll.position.extentAfter < 200
          : direction < 0 && t.detached && scroll.position.extentBefore < 200;
      if (!atEdge && (seekingDialogue != direction || !_foldedPaging)) return;
      final dialogueBoundary = direction > 0
          ? t.blocks.where(_isDialogue).firstOrNull?.first
          : t.blocks.where(_isDialogue).lastOrNull?.first;
      final before = (
        t.before,
        t.blocks.lastOrNull?.first,
        t.hasMore,
        t.detached,
      );
      pagingBusy = true;
      try {
        if (direction > 0) {
          await _older();
        } else {
          await _newer();
        }
        final after = (
          t.before,
          t.blocks.lastOrNull?.first,
          t.hasMore,
          t.detached,
        );
        // A malformed or repeated page must not create an endless request loop.
        if (before == after && pagingDirection == direction) {
          pagingDirection = 0;
          seekingDialogue = 0;
        } else if (pagingDirection == direction) {
          final reachedDialogue = t.blocks.any(
            (b) =>
                _isDialogue(b) &&
                (dialogueBoundary == null ||
                    (direction > 0
                        ? b.first < dialogueBoundary
                        : b.first > dialogueBoundary)),
          );
          // Raw pages contain tool events too. Continue past those pages even
          // if their collapsed headers have pushed the scroll edge away.
          seekingDialogue = reachedDialogue || !_foldedPaging ? 0 : direction;
          if (wasSeekingDialogue && reachedDialogue && _foldedPaging) {
            WidgetsBinding.instance.addPostFrameCallback((_) {
              if (mounted && !jumping && pagingDirection == direction) {
                scroll.revealPagingEdge(older: direction > 0);
                if (direction < 0 && !t.detached) {
                  t.readingHistory = false;
                  setState(() => follow = true);
                }
              }
            });
            WidgetsBinding.instance.ensureVisualUpdate();
          }
        }
      } finally {
        pagingBusy = false;
        if (mounted) _schedulePaging();
      }
    });
    // Initial history can already be loaded before this page is mounted. A
    // post-frame callback alone would otherwise wait for an unrelated tick.
    WidgetsBinding.instance.ensureVisualUpdate();
  }

  bool _isDialogue(TranscriptBlock block) =>
      block.kind == 'message' && block.text.trim().isNotEmpty ||
      block.kind == 'image';

  @override
  void dispose() {
    deckRoutes.unsubscribe(this);
    _becameHidden();
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
      pagingDirection = 0;
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
    c.transcripts[active.id]?.readingHistory = false;
    setState(() {
      follow = true;
      pagingDirection = 0;
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

  Future<void> _reloadHistory() async {
    try {
      readingView.currentState?.anchorReadingPosition();
      await c.reload(active.id);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.tr('历史加载失败，请重试'))));
      }
    }
  }

  Future<void> _older() async {
    final t = c.transcripts[active.id];
    if (t == null || t.loadingOlder || t.loadingNewer || !t.hasMore) return;
    readingView.currentState?.anchorReadingPosition();
    try {
      await c.loadOlder(active.id);
    } catch (_) {
      pagingDirection = 0;
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.tr('历史加载失败，请重试'))));
      }
    }
  }

  Future<void> _newer() async {
    final t = c.transcripts[active.id];
    if (t == null || t.loadingOlder || t.loadingNewer || !t.detached) return;
    readingView.currentState?.anchorReadingPosition();
    try {
      await c.loadNewer(active.id);
    } catch (_) {
      pagingDirection = 0;
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.tr('历史加载失败，请重试'))));
      }
    }
  }

  String _rowKey(Object row) => switch (row) {
    final TranscriptSection section => 'section/${section.key}',
    final TranscriptBlock block => 'block/${block.key}',
    _ => throw StateError('Unknown transcript row'),
  };

  Widget _block(TranscriptBlock block) => Container(
    key: block.key == focusedRecord ? focusAnchor : null,
    decoration: block.key == focusedRecord
        ? BoxDecoration(
            color: const Color(0x10EF633C),
            border: Border(left: BorderSide(color: accent, width: 2)),
          )
        : null,
    padding: block.key == focusedRecord
        ? const EdgeInsets.only(left: 10)
        : null,
    child: TranscriptTile(
      key: ValueKey('${block.key}-${block.key == focusedRecord}'),
      initiallyExpanded: block.key == focusedRecord,
      block: block,
      session: active.id,
      client: c,
    ),
  );

  bool _activityExpanded(TranscriptSection section) =>
      section.contains(focusedRecord) ||
      (activityOverrides[section.key] ?? expandAllActivities);

  Widget _section(TranscriptSection section) {
    if (!section.activity) return _block(section.blocks.first);
    final expanded = _activityExpanded(section);
    return ActivityGroup(
      key: ValueKey('activity-${section.key}'),
      blocks: section.blocks,
      expanded: expanded,
      showChildren: false,
      tile: _block,
      onToggle: () => setState(() {
        readingView.currentState?.anchorReadingPosition();
        if (section.contains(focusedRecord)) focusedRecord = null;
        activityOverrides[section.key] = !expanded;
      }),
    );
  }

  Future<bool> submit(
    TurnOptions options,
    List<DraftFile> files, {
    String delivery = 'send',
  }) async {
    final text = prompt.text.trim();
    if (text.isEmpty && files.isEmpty || savingMessage) return false;
    setState(() => savingMessage = true);
    try {
      await c.outbox.enqueue(active, text, options, files, delivery);
      if (mounted) {
        if (prompt.text.trim() == text) prompt.clear();
        setState(() => follow = true);
      }
      return true;
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.trError(e))));
      }
      return false;
    } finally {
      if (mounted) setState(() => savingMessage = false);
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
      if (!identical(_sectionTranscript, transcript) ||
          _sectionRevision != transcript?.revision) {
        _sectionTranscript = transcript;
        _sectionRevision = transcript?.revision ?? -1;
        _sectionBlocks =
            transcript?.blocks
                .where((b) => b.kind != 'queued_message')
                .toList() ??
            [];
        _sections = transcriptSections(_sectionBlocks, previous: _sections);
      }
      final blocks = _sectionBlocks;
      final paintedSession = active.id;
      final paintedRevision = transcript?.revision ?? -1;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && readingRoute?.isCurrent == true) {
          c.historyPresented(paintedSession, paintedRevision);
        }
      });
      final latest =
          c.sessions.where((s) => s.id == active.id).firstOrNull ?? active;
      final waiting = inputReceipts.any(
        (r) =>
            r.phase != InputDeliveryPhase.preparing &&
            r.waiting(blocks, status: latest.status, updated: latest.updated),
      );
      final pending =
          latest.status == 'pending' || (waiting && latest.status != 'running');
      final running =
          c.live &&
              latest.issue?.blocksInput != true &&
              (latest.status == 'running' ||
                  latest.status == 'waiting' ||
                  pending ||
                  waiting) ||
          sending;
      final sections = [..._sections];
      for (final localReceipt in inputReceipts) {
        if (transcript?.detached != true &&
            !localReceipt.observed(blocks) &&
            !localReceipt.responded(blocks)) {
          sections.add(
            TranscriptSection([
              TranscriptBlock(
                key: 'local-input-${localReceipt.id}',
                kind: 'message',
                role: 'user',
                text: localReceipt.text,
              ),
            ]),
          );
        }
      }
      // Activity rows share the outer lazy viewport, including expand-all.
      final rows = <Object>[];
      for (final section in sections) {
        rows.add(section);
        if (section.activity && _activityExpanded(section)) {
          rows.addAll(section.blocks);
        }
      }
      final activityCount = sections.where((s) => s.activity).length;
      final receiptLabel = sending
          ? context.tr(sendStage, sendStageArgs)
          : waiting
          ? context.tr(
              receipt?.phase == InputDeliveryPhase.submitted &&
                      receipt?.observed(blocks) != true
                  ? '已提交到电脑 Claude，等待原会话处理…'
                  : '已送达，等待 Agent 响应…',
            )
          : '';
      final showRunningTail =
          running &&
          transcript?.detached != true &&
          !outgoing.any((t) => t.pending);
      final rowByKey = {for (final row in rows) _rowKey(row): row};
      final rowKeys = [
        'padding-bottom',
        if (transcript?.detached == true) 'newer',
        if (showRunningTail) 'running',
        ...rows.reversed.map(_rowKey),
        if (transcript?.hasMore == true) 'older',
        'padding-top',
      ];
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
              onPressed: _reloadHistory,
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
                        sending
                            ? context.tr('发送中')
                            : context.tr(
                                latest.status == 'waiting'
                                    ? '等待确认'
                                    : pending
                                    ? '等待响应'
                                    : '执行中',
                              ),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(color: muted, fontSize: 12),
                      ),
                    ),
                  ] else
                    Text(
                      c.live
                          ? sessionStatusText(context, latest)
                          : context.tr('状态待同步'),
                      style: const TextStyle(color: muted, fontSize: 12),
                    ),
                ],
              ),
            ),
            if (latest.issue != null)
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 0, 16, 10),
                child: Semantics(
                  liveRegion: true,
                  child: Container(
                    width: double.infinity,
                    padding: const EdgeInsets.all(12),
                    decoration: BoxDecoration(
                      color: const Color(0xFFFFF4E5),
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: Text(
                      context.tr(latest.issue!.message),
                      style: const TextStyle(fontSize: 12, height: 1.5),
                    ),
                  ),
                ),
              ),
            if (!c.live && !c.demo)
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        context.tr('当前显示上次同步的内容，执行状态待确认'),
                        style: const TextStyle(fontSize: 11, color: muted),
                      ),
                    ),
                    ClientStatusButton(
                      client: c,
                      onPair: () => Navigator.push(
                        context,
                        MaterialPageRoute<void>(
                          builder: (_) => PairPage(client: c),
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            if (c.goals
                    .where((g) => g.session == active.id && g.isCurrent)
                    .firstOrNull
                case final goal?)
              GoalPanel(client: c, goal: goal),
            const Divider(height: 1),
            if (activityCount > 0)
              Align(
                alignment: Alignment.centerRight,
                child: TextButton.icon(
                  key: const ValueKey('all-activities-toggle'),
                  style: TextButton.styleFrom(
                    foregroundColor: muted,
                    minimumSize: const Size(0, 32),
                    padding: const EdgeInsets.symmetric(horizontal: 20),
                  ),
                  onPressed: () => setState(() {
                    readingView.currentState?.anchorReadingPosition();
                    expandAllActivities = !expandAllActivities;
                    activityOverrides.clear();
                    focusedRecord = null;
                  }),
                  icon: Icon(
                    expandAllActivities ? Icons.unfold_less : Icons.unfold_more,
                    size: 16,
                  ),
                  label: Text(
                    context.tr(expandAllActivities ? '全部折叠执行过程' : '全部展开执行过程'),
                    style: const TextStyle(fontSize: 11),
                  ),
                ),
              ),
            Expanded(
              child: Stack(
                children: [
                  NotificationListener<ScrollMetricsNotification>(
                    onNotification: (notification) {
                      if (notification.depth == 0) _schedulePaging();
                      return false;
                    },
                    child: NotificationListener<ScrollNotification>(
                      onNotification: (notification) {
                        if (notification.depth != 0 || scroll.correcting) {
                          return false;
                        }
                        if (notification is ScrollStartNotification &&
                            notification.dragDetails != null) {
                          readingView.currentState?.anchorReadingPosition();
                          userScrolling = true;
                        }
                        if (userScrolling &&
                            (notification is ScrollUpdateNotification ||
                                notification is OverscrollNotification)) {
                          final delta = notification is ScrollUpdateNotification
                              ? notification.scrollDelta ?? 0
                              : (notification as OverscrollNotification)
                                    .overscroll;
                          if (delta != 0 && !jumping) {
                            final direction = delta > 0 ? 1 : -1;
                            if (direction != pagingDirection) {
                              seekingDialogue = 0;
                              if (direction > 0 &&
                                      transcript?.loadingNewer == true ||
                                  direction < 0 &&
                                      transcript?.loadingOlder == true) {
                                transcript?.cancelPaging();
                              }
                            }
                            // Keep newly streamed items out of the window being
                            // read. Existing items can still receive updates;
                            // forward paging rejoins the live tail.
                            if (delta > 0 && transcript != null) {
                              transcript.readingHistory = true;
                            }
                            pagingDirection = direction;
                            _schedulePaging();
                          }
                          final v =
                              !jumping &&
                              transcript?.detached != true &&
                              notification.metrics.extentBefore < 140 &&
                              pagingDirection < 0;
                          if (v) transcript?.readingHistory = false;
                          if (v != follow) {
                            follow = v;
                            WidgetsBinding.instance.addPostFrameCallback((_) {
                              if (mounted) setState(() {});
                            });
                          }
                        }
                        if (notification is ScrollEndNotification) {
                          userScrolling = false;
                          if (follow) _updated();
                        }
                        return false;
                      },
                      child: sections.isEmpty && !running
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
                          : ReadingScrollView(
                              key: readingView,
                              scrollKey: ValueKey(
                                'transcript-${active.id}-$focusGeneration',
                              ),
                              controller: scroll,
                              itemKeys: rowKeys,
                              anchorKeys: rowByKey.keys.toSet(),
                              itemBuilder: (_, index) {
                                final itemKey = rowKeys[index];
                                if (itemKey == 'padding-bottom') {
                                  return const SizedBox(height: 28);
                                }
                                if (itemKey == 'padding-top') {
                                  return const SizedBox(height: 16);
                                }
                                if (itemKey == 'newer' || itemKey == 'older') {
                                  final loading = itemKey == 'older'
                                      ? transcript!.loadingOlder
                                      : transcript!.loadingNewer;
                                  return SizedBox(
                                    height: 32,
                                    child: loading
                                        ? const Center(
                                            child: RunningIndicator(),
                                          )
                                        : null,
                                  );
                                }
                                return Align(
                                  alignment: Alignment.topCenter,
                                  child: ConstrainedBox(
                                    constraints: const BoxConstraints(
                                      maxWidth: 760,
                                    ),
                                    child: itemKey == 'running'
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
                                                    sending
                                                        ? context.tr(
                                                            sendStage,
                                                            sendStageArgs,
                                                          )
                                                        : context.tr(
                                                            latest.status ==
                                                                    'waiting'
                                                                ? '等待电脑端确认…'
                                                                : pending
                                                                ? (receipt?.phase ==
                                                                              InputDeliveryPhase.submitted &&
                                                                          receipt?.observed(
                                                                                blocks,
                                                                              ) !=
                                                                              true
                                                                      ? '已提交到电脑 Claude，等待原会话处理…'
                                                                      : '已送达，等待 Agent 响应…')
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
                                        : switch (rowByKey[itemKey]) {
                                            final TranscriptSection section =>
                                              _section(section),
                                            final TranscriptBlock block =>
                                              _block(block),
                                            _ => const SizedBox.shrink(),
                                          },
                                  ),
                                );
                              },
                            ),
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
                    if ((!sending && !waiting && receiptLabel.isNotEmpty) ||
                        !active.managed ||
                        active.source == 'desktop' ||
                        active.agent == 'claude')
                      Padding(
                        padding: EdgeInsets.only(bottom: 8),
                        child: Text(
                          !sending && !waiting && receiptLabel.isNotEmpty
                              ? receiptLabel
                              : (active.source == 'desktop' ||
                                        active.agent == 'codex' &&
                                            c.codexDesktop
                                    ? context.tr('发送到电脑 Codex 的同一个会话')
                                    : active.agent == 'claude'
                                    ? context.tr('继续 Claude 原会话 · 不创建分支')
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
                    ConversationOutbox(client: c, session: active.id),
                    InputComposer(
                      durableSending: true,
                      client: c,
                      session: active.id,
                      agent: active.agent,
                      controller: prompt,
                      sending: sending,
                      running:
                          running &&
                          (active.managed ||
                              active.agent == 'codex' && c.codexDesktop),
                      canSend:
                          c.agentInfo(active.agent).canSend &&
                          latest.issue?.blocksInput != true,
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
  final bool initiallyExpanded;
  const TranscriptTile({
    super.key,
    required this.block,
    required this.client,
    this.session = '',
    this.initiallyExpanded = false,
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
  late bool _expanded = widget.initiallyExpanded;
  String? _detailText;
  int _detailSeq = 0, _detailGeneration = 0;
  bool _detailLoading = false, _detailError = false;
  Timer? _detailTimer;
  String get _bodyText => block.detail ? (_detailText ?? '') : block.text;

  void _clearDetail() {
    _detailGeneration++;
    _detailTimer?.cancel();
    _detailTimer = null;
    _detailText = null;
    _detailSeq = 0;
    _detailLoading = false;
    _detailError = false;
  }

  Future<void> _loadDetail() async {
    _detailTimer?.cancel();
    _detailTimer = null;
    if (!mounted || !_expanded || !block.detail || _detailLoading) return;
    final generation = ++_detailGeneration;
    setState(() {
      _detailLoading = true;
      _detailError = false;
      _rendered = null;
    });
    try {
      final event = await client.executionDetail(session, block);
      final item = block.key.substring(
        0,
        block.key.length - block.kind.length - 1,
      );
      if (event['session'] != session ||
          event['item'] != item ||
          event['kind'] != block.kind ||
          event['detail'] == true) {
        throw const FormatException();
      }
      if (!mounted || generation != _detailGeneration || !_expanded) return;
      setState(() {
        _detailText = event['text'] as String? ?? '';
        _detailSeq = (event['seq'] as num).toInt();
        _rendered = null;
      });
    } catch (_) {
      if (mounted && generation == _detailGeneration) {
        setState(() {
          _detailError = true;
          _rendered = null;
        });
      }
    } finally {
      if (mounted && generation == _detailGeneration) {
        setState(() {
          _detailLoading = false;
          _rendered = null;
        });
      }
    }
  }

  @override
  void dispose() {
    _detailTimer?.cancel();
    _detailGeneration++;
    super.dispose();
  }

  @override
  void didUpdateWidget(covariant TranscriptTile oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.session != session || oldWidget.block.key != block.key) {
      _clearDetail();
      _expanded = widget.initiallyExpanded;
      _rendered = null;
    }
    if (widget.initiallyExpanded != oldWidget.initiallyExpanded) {
      _expanded = widget.initiallyExpanded;
      if (!_expanded) _clearDetail();
      _rendered = null;
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _rendered = null;
  }

  @override
  Widget build(BuildContext context) {
    if (_expanded &&
        block.detail &&
        !_detailLoading &&
        !_detailError &&
        _detailTimer == null &&
        (_detailText == null || _detailSeq < block.seq)) {
      _detailTimer = Timer(const Duration(milliseconds: 150), _loadDetail);
    }
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

  Widget markdown(BuildContext context) => ProgressiveText(
    key: ValueKey('message-body-${block.key}'),
    text: _bodyText,
    builder: (text) => markdownBody(context, text),
  );
  Widget markdownBody(BuildContext context, String text) => MarkdownBody(
    data: text,
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
          initiallyExpanded: _expanded,
          onExpansionChanged: (value) {
            setState(() {
              _expanded = value;
              if (!value) _clearDetail();
              _rendered = null;
            });
            if (value && block.detail) unawaited(_loadDetail());
          },
          shape: const Border(),
          collapsedShape: const Border(),
          dense: true,
          tilePadding: const EdgeInsets.symmetric(horizontal: 12),
          childrenPadding: const EdgeInsets.fromLTRB(14, 0, 14, 16),
          title: Text(
            block.kind == 'tool_call'
                ? '$title · ${block.text.substring(0, block.text.length.clamp(0, 160)).split('\n').first}'
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
          children: _expanded
              ? [
                  if (_detailLoading)
                    const Padding(
                      padding: EdgeInsets.all(8),
                      child: SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      ),
                    ),
                  if (_detailError)
                    Column(
                      children: [
                        Text(
                          context.tr('执行详情加载失败，请重试'),
                          style: const TextStyle(fontSize: 12, color: muted),
                        ),
                        TextButton(
                          onPressed: _loadDetail,
                          child: Text(context.tr('重试')),
                        ),
                      ],
                    ),
                  if (!block.detail || _detailText != null)
                    Align(
                      alignment: Alignment.centerLeft,
                      child: block.kind == 'thinking'
                          ? markdown(context)
                          : ProgressiveText(
                              key: ValueKey('tool-body-${block.key}'),
                              text: _bodyText,
                              step: 4000,
                              builder: (text) => SelectableText(
                                text,
                                style: const TextStyle(
                                  fontFamily: 'monospace',
                                  fontSize: 12,
                                  height: 1.6,
                                ),
                              ),
                            ),
                    ),
                ]
              : const [],
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
          if (block.text.isNotEmpty)
            TextButton.icon(
              key: ValueKey('copy-message-${block.key}'),
              style: TextButton.styleFrom(
                foregroundColor: muted,
                padding: const EdgeInsets.symmetric(horizontal: 8),
                textStyle: const TextStyle(fontSize: 12),
              ),
              onPressed: () async {
                await Clipboard.setData(ClipboardData(text: block.text));
                if (!context.mounted) return;
                ScaffoldMessenger.of(
                  context,
                ).showSnackBar(SnackBar(content: Text(context.tr('已复制'))));
              },
              icon: const Icon(Icons.copy_outlined, size: 15),
              label: Text(context.tr('复制')),
            ),
        ],
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
            .then((a) => a.downloadRequired ? Uint8List(0) : a.bytes);
  late Future<Uint8List> data = load();
  void retry() => setState(() {
    if (widget.id.isNotEmpty) widget.client.images.remove(widget.id);
    data = load();
  });
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
  Widget build(BuildContext context) => FutureBuilder<Uint8List>(
    future: data,
    builder: (_, snap) {
      if (snap.connectionState != ConnectionState.done) {
        return const ImageLoading();
      }
      if (snap.hasError) {
        return TextButton(
          onPressed: retry,
          child: Text(
            context.tr('{0}，点击重试', [context.tr(attachmentError(snap.error))]),
          ),
        );
      }
      if (!snap.hasData) {
        return const ImageLoading();
      }
      if (snap.data!.isEmpty && widget.href.isNotEmpty) {
        return TextButton.icon(
          onPressed: () => Navigator.push(
            context,
            MaterialPageRoute<void>(
              builder: (_) => AttachmentPage(
                client: widget.client,
                session: widget.session,
                href: widget.href,
              ),
            ),
          ),
          icon: const Icon(Icons.download_outlined),
          label: Text(context.tr('下载原文件')),
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
                  child: LoadingImage(
                    image: MemoryImage(snap.data!),
                    errorColor: Colors.white,
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
              child: LoadingImage(
                image: ResizeImage(
                  MemoryImage(snap.data!),
                  width:
                      (MediaQuery.sizeOf(context).width *
                              MediaQuery.devicePixelRatioOf(context))
                          .ceil()
                          .clamp(64, 2048),
                  height: (320 * MediaQuery.devicePixelRatioOf(context))
                      .ceil()
                      .clamp(64, 2048),
                  policy: ResizeImagePolicy.fit,
                ),
                fit: BoxFit.contain,
                onRetry: retry,
              ),
            ),
          ),
        ),
      );
    },
  );
}
