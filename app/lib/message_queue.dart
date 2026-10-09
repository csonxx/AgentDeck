import 'package:flutter/material.dart';
import 'core/client.dart';
import 'core/model.dart';
import 'design.dart';
import 'language.dart';
import 'running_indicator.dart';

/// Queue snapshots arrive over the existing encrypted stream, independently of
/// paginated conversation history. No polling or queue scheduler runs on phone.
class MessageQueue extends StatefulWidget {
  final AgentClient client;
  final String session, agent;
  final bool running;
  const MessageQueue({
    super.key,
    required this.client,
    required this.session,
    required this.agent,
    required this.running,
  });
  @override
  State<MessageQueue> createState() => _MessageQueueState();
}

class _MessageQueueState extends State<MessageQueue> {
  String? busy;
  Future<String?> edit(QueuedMessage message) async {
    final controller = TextEditingController(text: message.text);
    try {
      return await showModalBottomSheet<String>(
        context: context,
        isScrollControlled: true,
        useSafeArea: true,
        builder: (context) => Padding(
          padding: EdgeInsets.fromLTRB(
            20,
            20,
            20,
            MediaQuery.viewInsetsOf(context).bottom + 20,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                context.tr('编辑消息'),
                style: const TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                key: const ValueKey('queue-edit-field'),
                controller: controller,
                autofocus: true,
                minLines: 3,
                maxLines: 8,
                maxLength: 65536,
                decoration: InputDecoration(hintText: context.tr('消息内容')),
              ),
              const SizedBox(height: 12),
              Align(
                alignment: Alignment.centerRight,
                child: FilledButton(
                  onPressed: () => Navigator.pop(context, controller.text),
                  child: Text(context.tr('保存')),
                ),
              ),
            ],
          ),
        ),
      );
    } finally {
      // Modal exit animation may still reference the field's controller.
      Future<void>.delayed(
        const Duration(milliseconds: 400),
        controller.dispose,
      );
    }
  }

  Future<void> action(QueuedMessage message, String action) async {
    if (busy != null) return;
    String text = '';
    if (action == 'edit') {
      final value = await edit(message);
      if (value == null || value.trim().isEmpty || !mounted) return;
      text = value;
    }
    if (!mounted) return;
    setState(() => busy = message.id);
    try {
      await widget.client.changeQueue(
        widget.session,
        message,
        action,
        text: text,
      );
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.trError(e))));
      }
    } finally {
      if (mounted) {
        setState(() => busy = null);
      }
    }
  }

  Future<void> refresh() async {
    try {
      final messages = await widget.client.queue(widget.session);
      if (mounted) {
        widget.client.queues[widget.session] = messages;
        widget.client.queueErrors.remove(widget.session);
        setState(() {});
      }
    } catch (e) {
      if (mounted) {
        widget.client.queueErrors[widget.session] = context.trError(e);
        setState(() {});
      }
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.client,
    builder: (context, _) {
      final messages =
          widget.client.queues[widget.session] ?? <QueuedMessage>[];
      final failure = widget.client.queueErrors[widget.session];
      if (messages.isEmpty) {
        return failure == null
            ? const SizedBox.shrink()
            : Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Row(
                  children: [
                    const Icon(
                      Icons.sync_problem,
                      size: 14,
                      color: DeckColors.muted,
                    ),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text(
                        failure,
                        maxLines: 2,
                        style: const TextStyle(
                          fontSize: 11,
                          color: DeckColors.muted,
                        ),
                      ),
                    ),
                    IconButton(
                      tooltip: context.tr('刷新队列'),
                      onPressed: refresh,
                      icon: const Icon(Icons.refresh, size: 16),
                    ),
                  ],
                ),
              );
      }
      return Container(
        key: const ValueKey('message-queue'),
        margin: const EdgeInsets.only(bottom: 10),
        decoration: BoxDecoration(
          color: DeckColors.paper,
          borderRadius: BorderRadius.circular(14),
          border: Border.all(color: DeckColors.line),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 9, 12, 3),
              child: Row(
                children: [
                  const Icon(
                    Icons.playlist_play_rounded,
                    size: 17,
                    color: DeckColors.muted,
                  ),
                  const SizedBox(width: 6),
                  Text(
                    context.tr('队列 · {0}', [messages.length]),
                    style: const TextStyle(
                      fontSize: 11,
                      color: DeckColors.muted,
                    ),
                  ),
                  const Spacer(),
                  Text(
                    context.tr(
                      messages.every((m) => m.origin == 'codex')
                          ? 'Codex 原生队列'
                          : '工作站队列',
                    ),
                    style: const TextStyle(
                      fontSize: 10,
                      color: DeckColors.muted,
                    ),
                  ),
                ],
              ),
            ),
            ConstrainedBox(
              constraints: BoxConstraints(
                maxHeight: MediaQuery.sizeOf(context).height * .22,
              ),
              child: ListView.builder(
                shrinkWrap: true,
                padding: const EdgeInsets.only(bottom: 5),
                itemCount: messages.length,
                itemBuilder: (context, i) {
                  final m = messages[i];
                  return Padding(
                    key: ValueKey('queued-${m.id}'),
                    padding: const EdgeInsets.only(left: 12),
                    child: Row(
                      children: [
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                m.text.isEmpty ? context.tr('附件消息') : m.text,
                                maxLines: 2,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(
                                  fontSize: 13,
                                  height: 1.4,
                                ),
                              ),
                              if (m.attachments > 0)
                                Text(
                                  context.tr('{0} 个附件', [m.attachments]),
                                  style: const TextStyle(
                                    fontSize: 10,
                                    color: DeckColors.muted,
                                  ),
                                ),
                              if (m.state != 'queued')
                                Text(
                                  context.tr(
                                    m.state == 'paused'
                                        ? '队列已暂停'
                                        : m.state == 'dispatching'
                                        ? '正在提交…'
                                        : '结果待确认，请检查电脑会话',
                                  ),
                                  style: const TextStyle(
                                    fontSize: 10,
                                    color: DeckColors.muted,
                                  ),
                                ),
                            ],
                          ),
                        ),
                        if (busy == m.id)
                          const Padding(
                            padding: EdgeInsets.all(14),
                            child: RunningIndicator(),
                          )
                        else
                          PopupMenuButton<String>(
                            key: ValueKey('queue-menu-${m.id}'),
                            tooltip: context.tr('消息操作'),
                            icon: const Icon(
                              Icons.more_horiz,
                              size: 20,
                              color: DeckColors.muted,
                            ),
                            enabled: busy == null && widget.client.connected,
                            onSelected: (value) => action(m, value),
                            itemBuilder: (context) => [
                              PopupMenuItem(
                                value: 'steer',
                                enabled: widget.running && m.editable,
                                child: Text(
                                  context.tr(
                                    widget.agent == 'claude' ? '中断并继续' : '引导',
                                  ),
                                ),
                              ),
                              PopupMenuItem(
                                value: 'edit',
                                enabled: m.editable,
                                child: Text(context.tr('编辑消息')),
                              ),
                              PopupMenuItem(
                                value: 'delete',
                                enabled: m.cancellable,
                                child: Text(context.tr('删除消息')),
                              ),
                            ],
                          ),
                      ],
                    ),
                  );
                },
              ),
            ),
          ],
        ),
      );
    },
  );
}
