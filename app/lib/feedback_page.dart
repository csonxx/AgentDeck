import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'core/client.dart';
import 'design.dart';
import 'language.dart';

class FeedbackSetting extends StatelessWidget {
  final AgentClient client;
  const FeedbackSetting({super.key, required this.client});
  @override
  Widget build(BuildContext context) => ListTile(
    key: const ValueKey('feedback-setting'),
    contentPadding: EdgeInsets.zero,
    leading: const Icon(Icons.feedback_outlined),
    title: Text(context.tr('问题反馈')),
    subtitle: Text(context.tr('附带发送耗时与连接诊断')),
    trailing: const Icon(Icons.chevron_right),
    onTap: () => Navigator.push(
      context,
      MaterialPageRoute<void>(builder: (_) => FeedbackPage(client: client)),
    ),
  );
}

class FeedbackPage extends StatefulWidget {
  final AgentClient client;
  const FeedbackPage({super.key, required this.client});
  @override
  State<FeedbackPage> createState() => _FeedbackPageState();
}

class _FeedbackPageState extends State<FeedbackPage> {
  final message = TextEditingController();
  bool include = true;
  AgentClient get c => widget.client;
  @override
  void initState() {
    super.initState();
    unawaited(
      c.feedback.initialize().then((_) {
        if (!mounted) return;
        message.text = c.feedback.pending?['message'] as String? ?? '';
        setState(() {});
      }),
    );
  }

  @override
  void dispose() {
    message.dispose();
    super.dispose();
  }

  void notice(String text) {
    if (mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(context.tr(text))));
    }
  }

  Future<void> copy() async {
    try {
      final report =
          c.feedback.pending ??
          await c.feedback.snapshot(message.text, include);
      await Clipboard.setData(
        ClipboardData(text: const JsonEncoder.withIndent('  ').convert(report)),
      );
      notice('诊断记录已复制，可粘贴到反馈消息');
    } catch (e) {
      notice(e is FormatException ? e.message : '诊断复制失败');
    }
  }

  Future<void> send() async {
    if (await c.feedback.submit(message.text, include)) {
      if (!mounted) return;
      message.clear();
      notice('反馈已保存到工作站');
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: Listenable.merge([c.feedback, c.diagnosticLog, c]),
    builder: (context, _) {
      final f = c.feedback;
      final frozen = f.pending != null;
      return Scaffold(
        appBar: AppBar(title: Text(context.tr('问题反馈'))),
        body: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            Text(
              context.tr('反馈保存到当前配对的工作站，返回反馈编号后即可查询。'),
              style: const TextStyle(fontSize: 15),
            ),
            const SizedBox(height: 12),
            Text(
              context.tr('诊断仅包含时间、请求编号、连接状态和耗时，不包含对话、工具内容、附件或密钥。'),
              style: const TextStyle(color: DeckColors.muted, fontSize: 13),
            ),
            const SizedBox(height: 20),
            TextField(
              key: const ValueKey('feedback-message'),
              controller: message,
              enabled: !frozen && !f.busy,
              minLines: 4,
              maxLines: 8,
              maxLength: 4000,
              decoration: InputDecoration(
                labelText: context.tr('问题说明'),
                hintText: context.tr('例如：今天 07:19，Codex 发送等待超过 20 秒'),
              ),
            ),
            CheckboxListTile(
              contentPadding: EdgeInsets.zero,
              value: include,
              onChanged: frozen || f.busy
                  ? null
                  : (v) => setState(() => include = v ?? true),
              title: Text(context.tr('附带诊断记录')),
              subtitle: Text(context.tr('最近 7 天，最多 500 条；加密保存在手机上')),
            ),
            if (c.diagnosticLog.storageFailed)
              Text(
                context.tr('部分本机诊断未能保存，当前记录仍可复制'),
                style: const TextStyle(color: DeckColors.accent),
              ),
            if (frozen) ...[
              SelectableText('${f.pending!['id']}'),
              Text(context.tr('待确认反馈已保留，重试会使用相同编号与记录。')),
            ],
            if (f.error.isNotEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Text(
                  context.tr(f.error),
                  key: const ValueKey('feedback-error'),
                ),
              ),
            const SizedBox(height: 16),
            FilledButton.icon(
              key: const ValueKey('feedback-submit'),
              onPressed: f.busy ? null : send,
              icon: f.busy
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.send_outlined),
              label: Text(context.tr(frozen ? '重试反馈' : '发送反馈')),
            ),
            TextButton.icon(
              onPressed: f.busy ? null : copy,
              icon: const Icon(Icons.copy_outlined),
              label: Text(context.tr('复制诊断记录')),
            ),
            if (frozen)
              TextButton(
                onPressed: f.busy
                    ? null
                    : () async {
                        try {
                          await f.discard();
                          if (mounted) setState(() {});
                        } catch (_) {
                          notice('反馈记录保存失败');
                        }
                      },
                child: Text(context.tr('放弃待确认反馈')),
              ),
            if (f.lastID.isNotEmpty) ...[
              const Divider(height: 32),
              Text(context.tr('最近反馈编号')),
              SelectableText(f.lastID, key: const ValueKey('feedback-receipt')),
              Text(
                context.tr('查询位置：本机后端；网关只透传加密内容。'),
                style: const TextStyle(color: DeckColors.muted, fontSize: 12),
              ),
            ],
          ],
        ),
      );
    },
  );
}
