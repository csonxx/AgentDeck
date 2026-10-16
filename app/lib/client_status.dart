import 'package:flutter/material.dart';
import 'core/client.dart';
import 'design.dart';
import 'language.dart';

class ClientStatusButton extends StatelessWidget {
  final AgentClient client;
  final VoidCallback? onPair;
  final bool light;
  const ClientStatusButton({
    super.key,
    required this.client,
    this.onPair,
    this.light = false,
  });

  @override
  Widget build(BuildContext context) => TextButton(
    key: const ValueKey('client-status'),
    onPressed: () => showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (_) => ClientStatusSheet(client: client, onPair: onPair),
    ),
    style: TextButton.styleFrom(
      foregroundColor: light ? const Color(0xFFB8BDC6) : DeckColors.muted,
      padding: const EdgeInsets.symmetric(horizontal: 6),
      minimumSize: const Size(40, 40),
      textStyle: const TextStyle(fontSize: 10),
    ),
    child: Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (client.health.busy)
          const SizedBox(
            width: 10,
            height: 10,
            child: CircularProgressIndicator(strokeWidth: 1.2),
          )
        else
          ConnectionDot(connected: client.live),
        const SizedBox(width: 6),
        Flexible(
          child: Text(
            context.tr(client.state),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
        ),
        if (client.health.visibleIssues.isNotEmpty) ...[
          const SizedBox(width: 5),
          Icon(
            Icons.info_outline,
            key: const ValueKey('client-local-warning'),
            size: 13,
            color: DeckColors.accent,
            semanticLabel: context.tr('本机存储需要关注'),
          ),
        ],
      ],
    ),
  );
}

/// Only issues that block the whole connection deserve a persistent banner.
/// Recoverable local warnings remain accessible from the compact status entry.
class ClientBlockingNotice extends StatelessWidget {
  final AgentClient client;
  final VoidCallback onPair;
  const ClientBlockingNotice({
    super.key,
    required this.client,
    required this.onPair,
  });
  @override
  Widget build(BuildContext context) {
    final issue = client.health.blockingIssue;
    final revoked = client.health.needsPairing;
    if (issue == null && !revoked) return const SizedBox.shrink();
    return Container(
      key: const ValueKey('client-blocking-notice'),
      margin: const EdgeInsets.symmetric(vertical: 8),
      padding: const EdgeInsets.fromLTRB(12, 8, 4, 8),
      decoration: BoxDecoration(
        color: const Color(0xFFFFEEEB),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        children: [
          Expanded(
            child: Text(
              context.trError(revoked ? client.error : issue!.message),
              style: const TextStyle(color: Color(0xFFBA3F38), fontSize: 12),
            ),
          ),
          TextButton(
            onPressed: revoked
                ? onPair
                : client.retryingIssues.contains(issue!.kind)
                ? null
                : () => client.retryIssue(issue.kind),
            child: Text(context.tr(revoked ? '重新配对' : '重试')),
          ),
        ],
      ),
    );
  }
}

class ClientStatusSheet extends StatefulWidget {
  final AgentClient client;
  final VoidCallback? onPair;
  const ClientStatusSheet({super.key, required this.client, this.onPair});
  @override
  State<ClientStatusSheet> createState() => _ClientStatusSheetState();
}

class _ClientStatusSheetState extends State<ClientStatusSheet> {
  bool refreshing = false;
  String refreshError = '';

  Future<void> refresh() async {
    setState(() {
      refreshing = true;
      refreshError = '';
    });
    try {
      await widget.client.refreshSessions();
    } catch (e) {
      if (mounted) setState(() => refreshError = context.trError(e));
    } finally {
      if (mounted) setState(() => refreshing = false);
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.client,
    builder: (context, _) {
      final c = widget.client;
      return SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      context.tr('连接与同步'),
                      style: const TextStyle(
                        fontSize: 18,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                  IconButton(
                    tooltip: context.tr('关闭'),
                    onPressed: () => Navigator.pop(context),
                    icon: const Icon(Icons.close),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              Text(context.tr(c.state)),
              if (c.device?['url'] case final String endpoint?) ...[
                const SizedBox(height: 8),
                SelectableText(
                  endpoint,
                  key: const ValueKey('connection-endpoint'),
                  style: const TextStyle(fontSize: 12, color: DeckColors.muted),
                ),
              ],
              const SizedBox(height: 8),
              Text(
                context.tr(
                  c.live
                      ? '会话列表已同步，正在接收实时更新'
                      : c.sessions.isNotEmpty
                      ? '当前显示上次同步的内容，执行状态待确认'
                      : '连接并同步后显示工作站的会话',
                ),
                style: const TextStyle(fontSize: 12, color: DeckColors.muted),
              ),
              if (c.error.isNotEmpty) ...[
                const SizedBox(height: 10),
                Text(context.trError(c.error)),
              ],
              if (refreshError.isNotEmpty) ...[
                const SizedBox(height: 10),
                Text(
                  refreshError,
                  style: const TextStyle(color: Color(0xFFBA3F38)),
                ),
              ],
              const SizedBox(height: 12),
              if (c.health.needsPairing || c.device == null)
                if (widget.onPair != null)
                  FilledButton(
                    onPressed: () {
                      Navigator.pop(context);
                      widget.onPair!();
                    },
                    child: Text(context.tr('配对工作站')),
                  )
                else
                  const SizedBox.shrink()
              else
                OutlinedButton(
                  key: const ValueKey('client-status-retry'),
                  onPressed: refreshing || c.health.busy ? null : refresh,
                  child: Text(
                    context.tr(
                      refreshing
                          ? '正在刷新…'
                          : c.live
                          ? '刷新'
                          : '重试',
                    ),
                  ),
                ),
              for (final issue in c.health.issues) ...[
                const Divider(height: 28),
                Text(context.trError(issue.message)),
                Wrap(
                  spacing: 8,
                  children: [
                    TextButton(
                      key: ValueKey('retry-${issue.kind.name}'),
                      onPressed: c.retryingIssues.contains(issue.kind)
                          ? null
                          : () => c.retryIssue(issue.kind),
                      child: Text(context.tr('重试')),
                    ),
                    if (!issue.blocking && !issue.dismissed)
                      TextButton(
                        key: ValueKey('dismiss-${issue.kind.name}'),
                        onPressed: () => c.dismissIssue(issue.kind),
                        child: Text(context.tr('暂不提醒')),
                      ),
                  ],
                ),
              ],
            ],
          ),
        ),
      );
    },
  );
}
