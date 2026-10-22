import 'package:flutter/material.dart';
import 'core/client.dart';
import 'core/usage.dart';
import 'design.dart';
import 'language.dart';
import 'codex_reset_panel.dart';

class UsageSheet extends StatefulWidget {
  final AgentClient client;
  final String agent;
  const UsageSheet({super.key, required this.client, required this.agent});
  @override
  State<UsageSheet> createState() => _UsageSheetState();
}

class _UsageSheetState extends State<UsageSheet> {
  late Future<AccountUsage> usage;
  int refreshToken = 0;
  @override
  void initState() {
    super.initState();
    usage = widget.client.accountUsage(widget.agent);
  }

  String date(DateTime? value) {
    if (value == null) return context.tr('暂不可用');
    final local = value.toLocal(), format = MaterialLocalizations.of(context);
    return '${format.formatCompactDate(local)} ${format.formatTimeOfDay(TimeOfDay.fromDateTime(local), alwaysUse24HourFormat: true)}';
  }

  void refresh({bool cards = true}) => setState(() {
    usage = widget.client.accountUsage(widget.agent);
    if (cards) refreshToken++;
  });
  @override
  Widget build(BuildContext context) => SafeArea(
    child: Padding(
      padding: const EdgeInsets.fromLTRB(24, 8, 24, 24),
      child: FutureBuilder<AccountUsage>(
        future: usage,
        builder: (context, snapshot) {
          final loading = snapshot.connectionState != ConnectionState.done;
          final data = snapshot.data;
          return Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      context.tr('{0} 用量', [
                        widget.agent == 'codex' ? 'Codex' : 'Claude',
                      ]),
                      style: const TextStyle(
                        fontSize: 22,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                  IconButton(
                    tooltip: context.tr('刷新用量'),
                    onPressed: loading ? null : refresh,
                    icon: const Icon(Icons.refresh_rounded),
                  ),
                ],
              ),
              Text(
                context.tr('工作站账号额度，所有项目共享'),
                style: const TextStyle(color: DeckColors.muted, fontSize: 12),
              ),
              const SizedBox(height: 20),
              Expanded(
                child: ListView(
                  children: [
                    if (loading)
                      const Center(child: CircularProgressIndicator())
                    else if (snapshot.hasError)
                      Text(
                        context.trError(snapshot.error),
                        textAlign: TextAlign.center,
                      )
                    else if (data != null) ...[
                      if (data.stale)
                        Container(
                          padding: const EdgeInsets.all(14),
                          margin: const EdgeInsets.only(bottom: 20),
                          decoration: BoxDecoration(
                            color: const Color(0xFFFFF1D8),
                            borderRadius: BorderRadius.circular(12),
                          ),
                          child: Text(
                            context.tr(
                              '非实时 · Claude 桌面账号缓存。实时接口暂未返回额度；此数据可能已变化。',
                            ),
                            style: const TextStyle(fontSize: 13),
                          ),
                        ),
                      for (final w in data.windows)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 24),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                context.tr(w.label),
                                style: const TextStyle(
                                  fontWeight: FontWeight.w600,
                                  fontSize: 16,
                                ),
                              ),
                              if (w.minutes > 0)
                                Text(
                                  context.tr('额度周期：{0} 小时', [
                                    (w.minutes / 60).toStringAsFixed(
                                      w.minutes % 60 == 0 ? 0 : 1,
                                    ),
                                  ]),
                                  style: const TextStyle(
                                    color: DeckColors.muted,
                                    fontSize: 12,
                                  ),
                                ),
                              const SizedBox(height: 12),
                              ClipRRect(
                                borderRadius: BorderRadius.circular(8),
                                child: LinearProgressIndicator(
                                  value: (w.used / 100).clamp(0, 1),
                                  minHeight: 7,
                                  backgroundColor: DeckColors.panel,
                                  color: w.used >= 90
                                      ? const Color(0xFFBF653B)
                                      : DeckColors.ink,
                                ),
                              ),
                              const SizedBox(height: 8),
                              Text(
                                context.tr('已用 {0}% · 剩余 {1}%', [
                                  w.used.toStringAsFixed(0),
                                  (100 - w.used)
                                      .clamp(0, 100)
                                      .toStringAsFixed(0),
                                ]),
                              ),
                              const SizedBox(height: 4),
                              Text(
                                context.tr('重置时间：{0}', [date(w.resets)]),
                                style: const TextStyle(
                                  color: DeckColors.muted,
                                  fontSize: 12,
                                ),
                              ),
                            ],
                          ),
                        ),
                      Text(
                        context.tr('数据时间：{0}', [date(data.asOf)]),
                        style: const TextStyle(
                          color: DeckColors.muted,
                          fontSize: 12,
                        ),
                      ),
                      const SizedBox(height: 6),
                      Text(
                        context.tr(
                          data.stale
                              ? '来源：Claude 桌面缓存'
                              : '来源：工作站 CLI（短时缓存 30 秒）',
                        ),
                        style: const TextStyle(
                          color: DeckColors.muted,
                          fontSize: 12,
                        ),
                      ),
                    ],
                    if (widget.agent == 'codex')
                      CodexResetPanel(
                        key: const ValueKey('codex-reset-panel'),
                        client: widget.client,
                        refreshToken: refreshToken,
                        onChanged: () => refresh(cards: false),
                      ),
                  ],
                ),
              ),
            ],
          );
        },
      ),
    ),
  );
}
