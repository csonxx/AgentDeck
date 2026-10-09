import 'language.dart';
import 'package:flutter/material.dart';
import 'core/client.dart';
import 'core/model.dart';
import 'design.dart';

class ConversationOutline extends StatefulWidget {
  final AgentClient client;
  final String session;
  const ConversationOutline({
    super.key,
    required this.client,
    required this.session,
  });
  @override
  State<ConversationOutline> createState() => _ConversationOutlineState();
}

class _ConversationOutlineState extends State<ConversationOutline> {
  final search = TextEditingController();
  final entries = <ConversationEntry>[];
  bool loading = true;
  String error = '';
  int generation = 0;
  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final current = ++generation;
    setState(() {
      entries.clear();
      loading = true;
      error = '';
    });
    try {
      var before = 0;
      while (mounted && current == generation) {
        final page = await widget.client.outlinePage(
          widget.session,
          before: before,
        );
        if (!mounted || current != generation) return;
        final values = (page['entries'] as List)
            .map(
              (e) => ConversationEntry.fromJson(
                Map<String, dynamic>.from(e as Map),
              ),
            )
            .toList();
        setState(() => entries.addAll(values));
        if (page['has_more'] != true) break;
        final next = (page['before'] as num).toInt();
        if (next == 0 || next == before) throw const FormatException('目录分页无效');
        before = next;
      }
    } catch (_) {
      if (mounted && current == generation) {
        setState(() => error = '目录加载失败，请重试');
      }
    } finally {
      if (mounted && current == generation) setState(() => loading = false);
    }
  }

  @override
  void dispose() {
    generation++;
    search.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final query = search.text.trim().toLowerCase();
    final visible = entries
        .where((e) => e.preview.toLowerCase().contains(query))
        .toList();
    return SafeArea(
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 12, 8, 4),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    context.tr('对话目录'),
                    style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600),
                  ),
                ),
                IconButton(
                  tooltip: context.tr('刷新目录'),
                  onPressed: loading ? null : _load,
                  icon: const Icon(Icons.refresh),
                ),
                IconButton(
                  tooltip: context.tr('关闭目录'),
                  onPressed: () => Navigator.pop(context),
                  icon: const Icon(Icons.close),
                ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    if (loading) ...[
                      const SizedBox(
                        width: 12,
                        height: 12,
                        child: CircularProgressIndicator(strokeWidth: 1.5),
                      ),
                      const SizedBox(width: 8),
                    ],
                    Expanded(
                      child: Text(
                        context.tr('{0} 条对话记录 · 含未加载的历史，点击即可跳转', [
                          entries.length,
                        ]),
                        style: const TextStyle(
                          fontSize: 12,
                          color: DeckColors.muted,
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: search,
                  onChanged: (_) => setState(() {}),
                  decoration: InputDecoration(
                    hintText: context.tr('搜索目录摘要'),
                    prefixIcon: Icon(Icons.search),
                    isDense: true,
                    border: InputBorder.none,
                  ),
                ),
              ],
            ),
          ),
          if (error.isNotEmpty)
            TextButton(onPressed: _load, child: Text(context.trError(error))),
          Expanded(
            child: visible.isEmpty
                ? Center(
                    child: Text(
                      loading
                          ? context.tr('正在读取对话目录…')
                          : query.isEmpty
                          ? context.tr('暂无对话记录')
                          : context.tr('没有匹配的记录'),
                    ),
                  )
                : ListView.builder(
                    key: const ValueKey('conversation-outline-list'),
                    itemCount: visible.length,
                    itemBuilder: (_, i) {
                      final e = visible[i];
                      final role = e.kind == 'image'
                          ? context.tr('图片')
                          : e.role == 'user'
                          ? context.tr('你')
                          : 'Agent';
                      final time = DateTime.tryParse(e.time)?.toLocal();
                      final stamp = time == null
                          ? ''
                          : ' · ${time.month}/${time.day} ${time.hour.toString().padLeft(2, '0')}:${time.minute.toString().padLeft(2, '0')}';
                      return ListTile(
                        minVerticalPadding: 12,
                        key: ValueKey('outline-${e.key}'),
                        leading: Icon(
                          e.kind == 'image'
                              ? Icons.image_outlined
                              : e.role == 'user'
                              ? Icons.person_outline
                              : Icons.terminal_rounded,
                          size: 20,
                        ),
                        title: Text(
                          e.preview,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(fontSize: 14),
                        ),
                        subtitle: Text(
                          '$role$stamp',
                          style: const TextStyle(fontSize: 11),
                        ),
                        onTap: () => Navigator.pop(context, e),
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }
}
