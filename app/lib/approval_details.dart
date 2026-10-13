import 'package:flutter/material.dart';
import 'language.dart';

// Keep nested arguments and literal command newlines readable. This is the
// complete presentation payload from the workstation, after its redaction.
String approvalDetailsText(Map<String, dynamic> params, String fallback) {
  if (params.isEmpty) return fallback;
  final out = StringBuffer();
  void write(Object? value, String indent) {
    if (value is Map && value.isNotEmpty) {
      for (final entry in value.entries) {
        out.writeln('$indent${entry.key}:');
        write(entry.value, '$indent  ');
      }
    } else if (value is List && value.isNotEmpty) {
      for (var i = 0; i < value.length; i++) {
        out.writeln('$indent[$i]:');
        write(value[i], '$indent  ');
      }
    } else {
      final text = value is Map
          ? '{}'
          : value is List
          ? '[]'
          : value?.toString() ?? 'null';
      for (final line in text.split('\n')) {
        out.writeln('$indent$line');
      }
    }
  }

  write(params, '');
  return out.toString();
}

Future<void> showApprovalDetails(
  BuildContext context,
  Map<String, dynamic> params,
  String fallback,
) => showModalBottomSheet<void>(
  context: context,
  isScrollControlled: true,
  useSafeArea: true,
  builder: (context) => SizedBox(
    height: MediaQuery.sizeOf(context).height * .9,
    child: SafeArea(
      top: false,
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 12, 8, 8),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    context.tr('完整执行内容'),
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
                IconButton(
                  tooltip: MaterialLocalizations.of(context).closeButtonTooltip,
                  onPressed: () => Navigator.of(context).pop(),
                  icon: const Icon(Icons.close),
                ),
              ],
            ),
          ),
          const Divider(height: 1),
          Expanded(
            child: Scrollbar(
              thumbVisibility: true,
              child: SingleChildScrollView(
                padding: const EdgeInsets.all(20),
                child: SizedBox(
                  width: double.infinity,
                  child: SelectableText(
                    approvalDetailsText(params, fallback),
                    style: const TextStyle(
                      fontFamily: 'monospace',
                      fontSize: 13,
                      height: 1.5,
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    ),
  ),
);
