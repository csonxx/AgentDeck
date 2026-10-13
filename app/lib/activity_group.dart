import 'package:flutter/material.dart';
import 'core/model.dart';
import 'design.dart';
import 'language.dart';

class ActivityGroup extends StatelessWidget {
  final List<TranscriptBlock> blocks;
  final bool expanded, showChildren;
  final VoidCallback onToggle;
  final Widget Function(TranscriptBlock) tile;
  const ActivityGroup({
    super.key,
    required this.blocks,
    required this.expanded,
    this.showChildren = true,
    required this.onToggle,
    required this.tile,
  });

  @override
  Widget build(BuildContext context) => Container(
    margin: const EdgeInsets.only(bottom: 12),
    decoration: BoxDecoration(
      border: Border.all(color: DeckColors.line, width: .6),
      borderRadius: BorderRadius.circular(14),
    ),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        InkWell(
          key: ValueKey('activity-toggle-${blocks.first.key}'),
          onTap: onToggle,
          borderRadius: BorderRadius.circular(14),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 12),
            child: Row(
              children: [
                const Icon(Icons.terminal, size: 17, color: DeckColors.muted),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    context.tr('执行过程 · {0} 项', [blocks.length]),
                    style: const TextStyle(
                      fontSize: 12,
                      color: DeckColors.muted,
                    ),
                  ),
                ),
                Icon(
                  expanded ? Icons.expand_less : Icons.expand_more,
                  size: 19,
                  color: DeckColors.muted,
                ),
              ],
            ),
          ),
        ),
        if (expanded && showChildren)
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 0, 8, 2),
            child: Column(children: blocks.map(tile).toList()),
          ),
      ],
    ),
  );
}
