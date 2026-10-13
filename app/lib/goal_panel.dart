import 'package:flutter/material.dart';
import 'core/client.dart';
import 'core/goals.dart';
import 'design.dart';
import 'language.dart';

class GoalPanel extends StatefulWidget {
  final AgentClient client;
  final AgentGoal goal;
  final VoidCallback? open;
  const GoalPanel({
    super.key,
    required this.client,
    required this.goal,
    this.open,
  });
  @override
  State<GoalPanel> createState() => _GoalPanelState();
}

class _GoalPanelState extends State<GoalPanel> {
  bool busy = false;
  Future<void> change(String action) async {
    if (action == 'delete') {
      final yes = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text(context.tr('删除目标？')),
          content: Text(context.tr('将停止并清除这个目标，保留会话和已有记录。')),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: Text(context.tr('取消')),
            ),
            TextButton(
              onPressed: () => Navigator.pop(context, true),
              child: Text(context.tr('删除')),
            ),
          ],
        ),
      );
      if (yes != true) return;
    }
    setState(() => busy = true);
    try {
      await widget.client.changeGoal(widget.goal, action);
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
    final g = widget.goal;
    return Container(
      key: ValueKey('goal-${g.session}'),
      margin: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
      decoration: BoxDecoration(
        color: const Color(0xFFF5F6F7),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              if (g.status == 'active')
                const SizedBox(
                  width: 14,
                  height: 14,
                  child: CircularProgressIndicator(strokeWidth: 1.5),
                )
              else
                const Icon(
                  Icons.flag_outlined,
                  size: 16,
                  color: DeckColors.muted,
                ),
              const SizedBox(width: 7),
              Expanded(
                child: Text(
                  context.tr(g.label),
                  style: const TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              if (busy)
                const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
              if (widget.open != null)
                IconButton(
                  tooltip: context.tr('打开会话'),
                  onPressed: widget.open,
                  icon: const Icon(Icons.open_in_new, size: 18),
                ),
              if (g.actions.contains('pause') && g.status == 'active')
                IconButton(
                  key: ValueKey('goal-pause-${g.session}'),
                  tooltip: context.tr('暂停目标'),
                  onPressed: busy || !widget.client.connected
                      ? null
                      : () => change('pause'),
                  icon: const Icon(Icons.pause_rounded, size: 20),
                ),
              if (g.actions.contains('resume') &&
                  g.status != 'active' &&
                  g.status != 'complete' &&
                  g.status != 'failed')
                IconButton(
                  key: ValueKey('goal-resume-${g.session}'),
                  tooltip: context.tr('恢复目标'),
                  onPressed: busy || !widget.client.connected
                      ? null
                      : () => change('resume'),
                  icon: const Icon(Icons.play_arrow_rounded, size: 20),
                ),
              if (g.actions.contains('delete'))
                IconButton(
                  key: ValueKey('goal-delete-${g.session}'),
                  tooltip: context.tr('删除目标'),
                  onPressed: busy || !widget.client.connected
                      ? null
                      : () => change('delete'),
                  icon: const Icon(Icons.delete_outline, size: 19),
                ),
            ],
          ),
          Text(
            g.objective,
            maxLines: 3,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(fontSize: 12, height: 1.5),
          ),
          if (g.tokens > 0 || g.seconds > 0)
            Padding(
              padding: const EdgeInsets.only(top: 5),
              child: Text(
                '${g.tokens}${g.budget == null ? "" : " / ${g.budget}"} tokens · ${g.seconds ~/ 60} min',
                style: const TextStyle(fontSize: 10, color: DeckColors.muted),
              ),
            ),
          if (g.reason.isNotEmpty)
            Text(
              g.reason,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontSize: 11, color: DeckColors.muted),
            ),
          if (g.note.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 5),
              child: Text(
                context.tr(g.note),
                style: const TextStyle(fontSize: 10, color: DeckColors.muted),
              ),
            ),
        ],
      ),
    );
  }
}
