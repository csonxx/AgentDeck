import 'dart:convert';
import 'package:flutter/material.dart';
import 'core/client.dart';
import 'core/model.dart';
import 'language.dart';

/// A question is an explicit user response, never a tool permission grant.
/// Native IDs and arrays are preserved all the way to the workstation.
class QuestionCard extends StatefulWidget {
  final TranscriptBlock block;
  final AgentClient client;
  final String session;
  final Map<String, dynamic> params;
  const QuestionCard({
    super.key,
    required this.block,
    required this.client,
    required this.session,
    required this.params,
  });
  @override
  State<QuestionCard> createState() => _QuestionCardState();
}

class _QuestionDraft {
  final selected = <String, List<String>>{};
  final custom = <String, String>{};
}

final _drafts = Expando<Map<String, _QuestionDraft>>();

class _QuestionCardState extends State<QuestionCard> {
  late Map<String, List<String>> selected;
  late Map<String, String> custom;
  String get draftKey =>
      '${widget.session}/${widget.block.key}/${jsonEncode(questions).hashCode}';
  void bindDraft() {
    final drafts = _drafts[widget.client] ??= <String, _QuestionDraft>{};
    if (!drafts.containsKey(draftKey) && drafts.length >= 32) {
      drafts.remove(drafts.keys.first);
    }
    final draft = drafts.putIfAbsent(draftKey, _QuestionDraft.new);
    selected = draft.selected;
    custom = draft.custom;
  }

  @override
  void initState() {
    super.initState();
    bindDraft();
  }

  final Map<String, TextEditingController> fields = {};
  @override
  void dispose() {
    for (final controller in fields.values) {
      controller.dispose();
    }
    super.dispose();
  }

  bool busy = false, submitted = false;
  String? error;
  List<Map<String, dynamic>> get questions {
    final raw =
        widget.params['questions'] ??
        (widget.params['input'] is Map
            ? widget.params['input']['questions']
            : null);
    return raw is List
        ? raw.whereType<Map>().map((q) => Map<String, dynamic>.from(q)).toList()
        : [];
  }

  List<Map> options(Map<String, dynamic> q) => q['options'] is List
      ? (q['options'] as List).whereType<Map>().toList()
      : [];
  String id(Map<String, dynamic> q, int i) =>
      (widget.params['question_kind'] == 'claude'
              ? q['question'] ?? '$i'
              : q['id'] ?? q['question'] ?? '$i')
          .toString();
  bool multiple(Map<String, dynamic> q) =>
      q['multiSelect'] == true || q['multiple'] == true;
  bool other(Map<String, dynamic> q) =>
      options(q).isEmpty ||
      q['isOther'] == true ||
      (widget.params['question_kind'] != 'codex' &&
          q['custom'] != false &&
          q['isOther'] != false);
  List<String> values(String key) => [
    ...selected[key] ?? [],
    if ((custom[key] ?? '').trim().isNotEmpty) custom[key]!.trim(),
  ];
  bool get ready => questions.asMap().entries.every(
    (e) => values(id(e.value, e.key)).isNotEmpty,
  );
  @override
  void didUpdateWidget(QuestionCard oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.block.key != widget.block.key ||
        oldWidget.session != widget.session ||
        oldWidget.client != widget.client ||
        jsonEncode(
              oldWidget.params['questions'] ?? oldWidget.params['input'],
            ) !=
            jsonEncode(widget.params['questions'] ?? widget.params['input'])) {
      bindDraft();
      for (final controller in fields.values) {
        controller.dispose();
      }
      fields.clear();
      submitted = false;
      error = null;
    }
  }

  Future<void> submit(bool cancel) async {
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await widget.client.approve(
        widget.session,
        widget.block.key.replaceFirst(RegExp(r':approval$'), ''),
        cancel ? 'decline' : 'accept',
        answer: cancel
            ? '{}'
            : jsonEncode({
                for (var i = 0; i < questions.length; i++)
                  id(questions[i], i): multiple(questions[i])
                      ? values(id(questions[i], i))
                      : values(id(questions[i], i)).single,
              }),
      );
      _drafts[widget.client]?.remove(draftKey);
      if (mounted) setState(() => submitted = true);
    } catch (e) {
      if (mounted) setState(() => error = context.trError(e));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final done = submitted || widget.block.status != 'pending';
    if (done) _drafts[widget.client]?.remove(draftKey);
    return Card(
      margin: const EdgeInsets.symmetric(vertical: 10),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  done ? Icons.check_circle_outline : Icons.chat_bubble_outline,
                  size: 19,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    context.tr(done ? '提问已处理' : '需要你的回答'),
                    style: const TextStyle(fontWeight: FontWeight.w600),
                  ),
                ),
                if (busy)
                  const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
              ],
            ),
            for (var i = 0; i < questions.length; i++) ...[
              const SizedBox(height: 16),
              if ((questions[i]['header']?.toString() ?? '').isNotEmpty)
                Text(
                  '${i + 1} · ${questions[i]['header']}',
                  style: Theme.of(context).textTheme.labelMedium,
                ),
              const SizedBox(height: 5),
              SelectableText(
                questions[i]['question']?.toString() ?? '',
                style: const TextStyle(
                  fontSize: 15,
                  fontWeight: FontWeight.w500,
                ),
              ),
              if (!done) ...[
                if (multiple(questions[i]))
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: Text(
                      context.tr('可多选'),
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                  ),
                for (final option in options(questions[i]))
                  _option(questions[i], i, option),
                if (other(questions[i]))
                  Padding(
                    padding: const EdgeInsets.only(top: 10),
                    child: TextFormField(
                      key: ValueKey('question-answer-${id(questions[i], i)}'),
                      controller: fields.putIfAbsent(
                        id(questions[i], i),
                        () => TextEditingController(
                          text: custom[id(questions[i], i)] ?? '',
                        ),
                      ),
                      enabled: !busy,
                      obscureText: questions[i]['isSecret'] == true,
                      maxLines: questions[i]['isSecret'] == true ? 1 : 4,
                      minLines: 1,
                      decoration: InputDecoration(
                        labelText: context.tr(
                          options(questions[i]).isEmpty ? '你的回答' : '其他回答',
                        ),
                      ),
                      onChanged: (v) => setState(() {
                        final key = id(questions[i], i);
                        custom[key] = v;
                        if (!multiple(questions[i]) && v.trim().isNotEmpty) {
                          selected[key] = [];
                        }
                        error = null;
                      }),
                    ),
                  ),
              ],
            ],
            if (!done) ...[
              if (error != null)
                Padding(
                  padding: const EdgeInsets.only(top: 12),
                  child: Text(
                    error!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ),
              const SizedBox(height: 14),
              Row(
                children: [
                  TextButton(
                    onPressed: busy ? null : () => submit(true),
                    child: Text(context.tr('跳过回答')),
                  ),
                  const Spacer(),
                  FilledButton(
                    onPressed: busy || !ready ? null : () => submit(false),
                    child: Text(context.tr(busy ? '提交中…' : '提交回答')),
                  ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _option(Map<String, dynamic> q, int index, Map option) {
    final key = id(q, index), label = option['label']?.toString() ?? '';
    final chosen = (selected[key] ?? []).contains(label);
    return Padding(
      padding: const EdgeInsets.only(top: 8),
      child: Semantics(
        button: true,
        selected: chosen,
        child: Material(
          color: chosen
              ? Theme.of(context).colorScheme.primaryContainer
              : Colors.transparent,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(10),
            side: BorderSide(
              color: chosen
                  ? Theme.of(context).colorScheme.primary
                  : Theme.of(context).dividerColor,
            ),
          ),
          child: InkWell(
            borderRadius: BorderRadius.circular(10),
            onTap: busy
                ? null
                : () => setState(() {
                    if (multiple(q)) {
                      final next = [...selected[key] ?? <String>[]];
                      chosen ? next.remove(label) : next.add(label);
                      selected[key] = next;
                    } else {
                      selected[key] = [label];
                      custom[key] = '';
                      fields[key]?.clear();
                      // Preserve each question's draft while switching choices.
                      FocusScope.of(context).unfocus();
                    }
                    error = null;
                  }),
            child: Padding(
              padding: const EdgeInsets.all(12),
              child: Row(
                children: [
                  Icon(
                    multiple(q)
                        ? (chosen
                              ? Icons.check_box_outlined
                              : Icons.check_box_outline_blank)
                        : (chosen
                              ? Icons.radio_button_checked
                              : Icons.radio_button_off),
                    size: 21,
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          label,
                          style: const TextStyle(fontWeight: FontWeight.w500),
                        ),
                        if ((option['description']?.toString() ?? '')
                            .isNotEmpty) ...[
                          const SizedBox(height: 4),
                          Text(
                            option['description'].toString(),
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                        ],
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
