import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'core/client.dart';
import 'language.dart';
import 'design.dart';
import 'running_indicator.dart';

Future<void> _act(BuildContext context, Future<void> Function() action) async {
  try {
    await action();
  } catch (e) {
    if (context.mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(context.trError(e))));
    }
  }
}

class OutboxSummary extends StatelessWidget {
  final AgentClient client;
  const OutboxSummary({super.key, required this.client});
  @override
  Widget build(BuildContext context) {
    final n = client.outbox.outstanding.length;
    if (n == 0 && client.outbox.restoreError.isEmpty) {
      return const SizedBox.shrink();
    }
    return ListTile(
      dense: true,
      leading: const Icon(Icons.outbox_outlined, size: 20),
      title: Text(
        client.outbox.restoreError.isNotEmpty
            ? context.tr(client.outbox.restoreError)
            : context.tr('{0} 条消息待处理', [n]),
      ),
      trailing: const Icon(Icons.chevron_right, size: 18),
      onTap: () => Navigator.push(
        context,
        MaterialPageRoute<void>(builder: (_) => OutboxPage(client: client)),
      ),
    );
  }
}

class ConversationOutbox extends StatelessWidget {
  final AgentClient client;
  final String session;
  const ConversationOutbox({
    super.key,
    required this.client,
    required this.session,
  });
  @override
  Widget build(BuildContext context) {
    final tasks = client.outbox
        .forSession(session)
        .where((t) => t.pending || t.needsAttention)
        .toList();
    if (tasks.isEmpty) return const SizedBox.shrink();
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        for (final task in tasks)
          Padding(
            padding: const EdgeInsets.only(bottom: 4),
            child: Row(
              children: [
                if (task.running)
                  const RunningIndicator()
                else
                  const Icon(
                    Icons.outbox_outlined,
                    size: 16,
                    color: DeckColors.muted,
                  ),
                const SizedBox(width: 8),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        context.tr(task.stage),
                        maxLines: 2,
                        style: const TextStyle(
                          fontSize: 11,
                          color: DeckColors.muted,
                        ),
                      ),
                      if (task.needsAttention && task.error.isNotEmpty)
                        Text(
                          context.trError(task.error),
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 11,
                            color: DeckColors.muted,
                          ),
                        ),
                    ],
                  ),
                ),
                TextButton(
                  onPressed: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) => OutboxPage(client: client),
                    ),
                  ),
                  child: Text(context.tr('查看')),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

class OutboxPage extends StatelessWidget {
  final AgentClient client;
  const OutboxPage({super.key, required this.client});
  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(context.tr('发件箱'))),
    body: ListenableBuilder(
      listenable: client,
      builder: (context, _) {
        final tasks = client.outbox.tasks
            .where(
              (t) =>
                  t.binding == client.outbox.binding &&
                  t.state != 'done' &&
                  t.state != 'queued',
            )
            .toList()
            .reversed
            .toList();
        return ListView(
          padding: const EdgeInsets.all(16),
          children: [
            Text(
              context.tr('离开会话后继续发送；断网恢复后自动重试。'),
              style: const TextStyle(fontSize: 12, color: DeckColors.muted),
            ),
            if (client.outbox.restoreError.isNotEmpty)
              TextButton(
                onPressed: () => client.outbox.restore(),
                child: Text(context.tr(client.outbox.restoreError)),
              ),
            if (tasks.isEmpty)
              Padding(
                padding: const EdgeInsets.all(32),
                child: Center(child: Text(context.tr('没有待发送消息'))),
              ),
            for (final task in tasks)
              Card(
                key: ValueKey('outgoing-${task.id}'),
                margin: const EdgeInsets.only(top: 12),
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        task.title,
                        style: const TextStyle(fontWeight: FontWeight.w600),
                      ),
                      const SizedBox(height: 8),
                      SelectableText(task.receipt.text),
                      const SizedBox(height: 8),
                      Text(
                        context.tr(task.stage),
                        style: const TextStyle(
                          fontSize: 12,
                          color: DeckColors.muted,
                        ),
                      ),
                      if (task.error.isNotEmpty)
                        Padding(
                          padding: const EdgeInsets.only(top: 8),
                          child: Text(
                            context.trError(task.error),
                            style: const TextStyle(fontSize: 12),
                          ),
                        ),
                      if (task.state == 'uncertain')
                        Padding(
                          padding: const EdgeInsets.only(top: 8),
                          child: Text(
                            context.tr('核对使用原请求，不会重复执行；仍无法确认时请查看原会话。'),
                            style: const TextStyle(fontSize: 12),
                          ),
                        ),
                      Wrap(
                        spacing: 8,
                        children: [
                          TextButton.icon(
                            onPressed: () => Clipboard.setData(
                              ClipboardData(text: task.text),
                            ),
                            icon: const Icon(Icons.copy, size: 16),
                            label: Text(context.tr('复制')),
                          ),
                          if (task.needsAttention || task.state == 'retrying')
                            TextButton(
                              onPressed: () => _act(
                                context,
                                () => client.outbox.retry(task),
                              ),
                              child: Text(
                                context.tr(
                                  task.locked && !task.definitelyNotSent
                                      ? '核对发送结果'
                                      : '重试',
                                ),
                              ),
                            ),
                          if (!task.running)
                            TextButton(
                              onPressed: () async {
                                final accepted =
                                    task.state == 'accepted' ||
                                    task.state == 'submitted' ||
                                    task.state == 'uncertain' ||
                                    task.locked;
                                final ok = await showDialog<bool>(
                                  context: context,
                                  builder: (ctx) => AlertDialog(
                                    title: Text(
                                      context.tr(
                                        accepted ? '移除本机发送记录？' : '取消待发送消息？',
                                      ),
                                    ),
                                    content: Text(
                                      context.tr(
                                        accepted
                                            ? '移除记录不会撤回可能已送达的消息。'
                                            : '取消后将停止自动重试。',
                                      ),
                                    ),
                                    actions: [
                                      TextButton(
                                        onPressed: () =>
                                            Navigator.pop(ctx, false),
                                        child: Text(context.tr('返回')),
                                      ),
                                      TextButton(
                                        onPressed: () =>
                                            Navigator.pop(ctx, true),
                                        child: Text(context.tr('确认')),
                                      ),
                                    ],
                                  ),
                                );
                                if (ok == true && context.mounted) {
                                  await _act(
                                    context,
                                    () => client.outbox.remove(task),
                                  );
                                }
                              },
                              child: Text(context.tr('移除')),
                            ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
          ],
        );
      },
    ),
  );
}
