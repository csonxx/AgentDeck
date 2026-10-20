import 'dart:convert';
import 'package:flutter/material.dart';
import 'core/client.dart';
import 'core/model.dart';
import 'language.dart';
import 'approval_details.dart';
import 'question_card.dart';

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
  bool busy = false, submitted = false;
  Future<void> decide(String decision, {String answer = '{}'}) async {
    setState(() => busy = true);
    try {
      await widget.client.approve(
        widget.session,
        widget.block.key.replaceFirst(RegExp(r':approval$'), ''),
        decision,
        answer: answer,
      );
      if (mounted) setState(() => submitted = true);
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
  void didUpdateWidget(ApprovalTile oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.block.key != widget.block.key ||
        oldWidget.session != widget.session) {
      submitted = false;
    }
  }

  @override
  Widget build(BuildContext context) {
    final b = widget.block;
    Map<String, dynamic> params = {};
    try {
      params = Map<String, dynamic>.from(jsonDecode(b.text));
    } catch (_) {}
    final rawQuestions =
        params['questions'] ??
        (params['input'] is Map ? params['input']['questions'] : null);
    final questions = rawQuestions is List
        ? rawQuestions.whereType<Map>().toList()
        : [];
    if (questions.isNotEmpty) {
      return QuestionCard(
        block: b,
        client: widget.client,
        session: widget.session,
        params: params,
      );
    }
    if (b.status != 'pending' || submitted) return const SizedBox.shrink();
    final title =
        (params['tool_name'] ??
                (params['toolCall'] is Map
                    ? params['toolCall']['title']
                    : null) ??
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
            Text(
              title,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w500),
            ),
            if (description.isNotEmpty)
              Text(
                description,
                maxLines: 3,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(fontSize: 12),
              ),
            TextButton(
              onPressed: () => showApprovalDetails(context, params, b.text),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Icon(Icons.unfold_more, size: 18),
                  const SizedBox(width: 6),
                  Flexible(child: Text(context.tr('展开完整执行内容'))),
                ],
              ),
            ),
            const SizedBox(height: 10),
            if (params['permission_options'] is List)
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final choice
                      in (params['permission_options'] as List)
                          .whereType<Map>())
                    OutlinedButton(
                      onPressed: busy
                          ? null
                          : () => decide(
                              choice['kind'] == 'allow_once' ||
                                      choice['kind'] == 'allow_always'
                                  ? 'accept'
                                  : 'decline',
                              answer: jsonEncode({
                                'permission_option': choice['optionId'],
                              }),
                            ),
                      child: Text(choice['name']?.toString() ?? ''),
                    ),
                ],
              )
            else
              Row(
                children: [
                  TextButton(
                    onPressed: busy ? null : () => decide('decline'),
                    child: Text(context.tr('拒绝')),
                  ),
                  const Spacer(),
                  FilledButton(
                    onPressed: busy ? null : () => decide('accept'),
                    child: Text(context.tr(busy ? '提交中…' : '允许此次')),
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }
}
