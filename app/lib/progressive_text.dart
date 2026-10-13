import 'package:flutter/material.dart';
import 'language.dart';

// Bound the initial Markdown/selection layout for an individual huge record.
// The full original stays available; expansion is explicit and incremental.
class ProgressiveText extends StatefulWidget {
  final String text;
  final Widget Function(String) builder;
  final int step;
  const ProgressiveText({
    super.key,
    required this.text,
    required this.builder,
    this.step = 8000,
  });
  @override
  State<ProgressiveText> createState() => _ProgressiveTextState();
}

class _ProgressiveTextState extends State<ProgressiveText> {
  late int limit = widget.step;
  @override
  Widget build(BuildContext context) {
    var end = limit.clamp(0, widget.text.length);
    if (end < widget.text.length) {
      final newline = widget.text.lastIndexOf('\n', end);
      if (newline > end - 1000) end = newline + 1;
      if (end > 0 &&
          widget.text.codeUnitAt(end - 1) >= 0xD800 &&
          widget.text.codeUnitAt(end - 1) <= 0xDBFF) {
        end--;
      }
    }
    final remaining = widget.text.length - end;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        widget.builder(widget.text.substring(0, end)),
        if (remaining > 0)
          TextButton.icon(
            key: const ValueKey('continue-long-text'),
            onPressed: () => setState(() => limit += widget.step),
            icon: const Icon(Icons.expand_more, size: 18),
            label: Text(context.tr('继续显示 · 剩余 {0} 字', [remaining])),
          ),
        if (limit > widget.step)
          TextButton(
            key: const ValueKey('collapse-long-text'),
            onPressed: () => setState(() => limit = widget.step),
            child: Text(context.tr('收起长消息')),
          ),
      ],
    );
  }
}
