import 'dart:convert';
import 'package:flutter/material.dart';
import 'core/client.dart';
import 'core/usage_resets.dart';
import 'language.dart';

class CodexResetPanel extends StatefulWidget {
  final AgentClient client;
  final VoidCallback onChanged;
  final int refreshToken;
  const CodexResetPanel({
    super.key,
    required this.client,
    required this.onChanged,
    this.refreshToken = 0,
  });
  @override
  State<CodexResetPanel> createState() => _CodexResetPanelState();
}

class _CodexResetPanelState extends State<CodexResetPanel> {
  static const pendingKey = 'agentdeck.codex.reset.pending';
  CodexResetState? data;
  final List<Map<String, dynamic>> history = [];
  String? error, historyError;
  String next = '', windowStart = '', message = '';
  bool loading = true, historyLoading = true, busy = false, resetReady = false;
  Map<String, dynamic>? pending;
  AgentClient get c => widget.client;
  String get binding =>
      '${c.device?['url'] ?? ''}|${c.device?['device'] ?? ''}';
  @override
  void initState() {
    super.initState();
    load();
    restore();
  }

  @override
  void didUpdateWidget(CodexResetPanel old) {
    super.didUpdateWidget(old);
    if (old.refreshToken != widget.refreshToken) {
      load();
      restore();
    }
  }

  Future<void> restore() async {
    try {
      final raw = await c.vault.storage.read(key: pendingKey);
      if (raw != null) {
        final v = Map<String, dynamic>.from(jsonDecode(raw) as Map);
        if (v['binding'] == binding && mounted) setState(() => pending = v);
      }
      if (mounted) setState(() => resetReady = true);
    } catch (_) {
      if (mounted) {
        setState(() => message = context.tr('上次重置记录暂不可读，请先核对电脑使用记录'));
      }
    }
  }

  Future<void> load() async {
    if (!mounted) return;
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final v = CodexResetState(await c.codexResetState());
      if (mounted) setState(() => data = v);
    } catch (e) {
      if (mounted) setState(() => error = context.trError(e));
    } finally {
      if (mounted) setState(() => loading = false);
    }
    await loadHistory(reset: true);
  }

  Future<void> loadHistory({bool reset = false}) async {
    if (!mounted) return;
    setState(() {
      historyLoading = true;
      historyError = null;
    });
    try {
      final v = await c.codexResetHistory(reset ? '' : next);
      if (mounted) {
        setState(() {
          if (reset) history.clear();
          for (final row in v['events'] as List? ?? []) {
            final x = Map<String, dynamic>.from(row as Map);
            if (!history.any((e) => e['id'] == x['id'])) history.add(x);
          }
          next = v['next'] as String? ?? '';
          windowStart = v['window_start'] as String? ?? '';
        });
      }
    } catch (e) {
      if (mounted) setState(() => historyError = context.trError(e));
    } finally {
      if (mounted) setState(() => historyLoading = false);
    }
  }

  String date(DateTime? value) {
    if (value == null) return context.tr('未提供');
    final d = value.toLocal(), f = MaterialLocalizations.of(context);
    return '${f.formatCompactDate(d)} ${f.formatTimeOfDay(TimeOfDay.fromDateTime(d), alwaysUse24HourFormat: true)}';
  }

  Future<bool> confirm(String title, String detail) async =>
      await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: Text(context.tr(title)),
          content: SingleChildScrollView(child: Text(context.tr(detail))),
          actions: [
            TextButton(
              key: const ValueKey('reset-cancel'),
              onPressed: () => Navigator.pop(ctx, false),
              child: Text(context.tr('取消')),
            ),
            FilledButton(
              key: const ValueKey('reset-confirm'),
              onPressed: () => Navigator.pop(ctx, true),
              child: Text(context.tr('确认使用一张')),
            ),
          ],
        ),
      ) ??
      false;
  Future<void> redeem([CodexResetCard? card]) async {
    if (busy || data == null || !resetReady || pending != null) return;
    setState(() => busy = true);
    try {
      final quote = await c.prepareCodexReset(card?.id ?? '', data!.account);
      if (!mounted) return;
      final selected = card?.title.isNotEmpty == true
          ? card!.title
          : context.tr('下一张可用重置卡');
      final yes = await confirm(
        '使用 Codex 重置卡？',
        '$selected\n\n${context.tr('将消耗一张重置卡，重置当前账号符合条件的五小时和每周额度，影响该账号所有项目。周额度的下一次重置时间可能变化。此操作不能撤销。')}',
      );
      if (!yes || !mounted) return;
      // Persist the exact attempt before sending. No background/automatic retry.
      final attempt = <String, dynamic>{
        'intent': quote['intent'],
        'id': AgentClient.requestID(),
        'binding': binding,
        'expires': quote['expires_at'],
      };
      await c.vault.storage.write(key: pendingKey, value: jsonEncode(attempt));
      if (!mounted) return;
      setState(() => pending = attempt);
      await submit(attempt);
    } catch (e) {
      if (mounted) setState(() => message = context.trError(e));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> submit(Map<String, dynamic> attempt) async {
    try {
      final v = await c.consumeCodexReset(
        attempt['intent'] as String,
        attempt['id'] as String,
      );
      final outcome = v['outcome'];
      if (outcome != 'unknown') {
        await c.vault.storage.delete(key: pendingKey);
        if (mounted) setState(() => pending = null);
      }
      if (mounted) {
        setState(
          () => message = context.tr(switch (outcome) {
            'reset' => '已使用一张重置卡，额度已重置',
            'alreadyRedeemed' => '本次重置已完成，没有重复消耗重置卡',
            'noCredit' => '当前没有可用重置卡，本次未重置',
            'nothingToReset' => '当前额度无需重置，本次未消耗重置卡',
            _ => '结果尚未确认，请刷新记录核对。再次操作只会使用原请求编号。',
          }),
        );
      }
      if (mounted) {
        widget.onChanged();
        await load();
      }
    } catch (e) {
      if (mounted) {
        setState(
          () => message =
              '${context.trError(e)}\n${context.tr('请先查看使用记录，勿新建重置请求')}',
        );
      }
    }
  }

  Future<void> retryPending() async {
    final attempt = pending;
    if (attempt == null || busy) return;
    final yes = await confirm(
      '核对原重置请求？',
      '复用上次的请求编号。若上次尚未执行，将完成原先确认的一次重置；已完成则不会再次消耗。',
    );
    if (!yes || !mounted) return;
    setState(() => busy = true);
    try {
      await submit(attempt);
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> dismissPending() async {
    if (busy) return;
    final yes =
        await showDialog<bool>(
          context: context,
          builder: (ctx) => AlertDialog(
            title: Text(context.tr('结束本次核对？')),
            content: Text(
              context.tr('请先核对使用记录和最新额度。清除本机待核对标记不会撤回已发生的重置，也不会再使用任何重置卡。'),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(ctx, false),
                child: Text(context.tr('取消')),
              ),
              TextButton(
                onPressed: () => Navigator.pop(ctx, true),
                child: Text(context.tr('已核对，清除标记')),
              ),
            ],
          ),
        ) ??
        false;
    if (yes) {
      await c.vault.storage.delete(key: pendingKey);
      if (mounted) setState(() => pending = null);
    }
  }

  @override
  Widget build(BuildContext context) {
    final s = data;
    final canUse =
        !busy &&
        resetReady &&
        !loading &&
        pending == null &&
        s != null &&
        s.available != null &&
        s.available! > 0 &&
        s.eligible &&
        s.eligibilityKnown;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Divider(height: 36),
        Row(
          children: [
            Expanded(
              child: Text(
                context.tr('重置卡'),
                style: const TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
            IconButton(
              tooltip: context.tr('刷新重置卡'),
              onPressed: loading || busy
                  ? null
                  : () {
                      load();
                      restore();
                    },
              icon: const Icon(Icons.refresh),
            ),
          ],
        ),
        if (loading)
          const Padding(
            padding: EdgeInsets.all(14),
            child: CircularProgressIndicator(),
          ),
        if (error != null) Text(error!),
        if (s != null) ...[
          Text(
            s.available == null
                ? context.tr('可用数量暂不可读')
                : context.tr('可用 {0} 张', [s.available.toString()]),
          ),
          if (!s.detailsKnown) Text(context.tr('电脑接口仅提供数量，未提供卡片详情')),
          if (!loading && !s.eligible)
            Text(
              context.tr(
                s.eligibilityKnown
                    ? '五小时或每周核心额度剩余不超过 10% 时可使用'
                    : '当前核心额度暂不可读，暂不能使用重置卡',
              ),
              style: const TextStyle(fontSize: 12),
            ),
          for (final card in s.cards)
            Card(
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      card.title.isEmpty ? context.tr('Codex 重置卡') : card.title,
                      style: const TextStyle(fontWeight: FontWeight.w600),
                    ),
                    if (card.description.isNotEmpty) Text(card.description),
                    Text(context.tr('获得时间：{0}', [date(card.granted)])),
                    Text(context.tr('到期时间：{0}', [date(card.expires)])),
                    Text(
                      context.tr(switch (card.status) {
                        'available' => '可用',
                        'redeeming' => '处理中',
                        'redeemed' => '已使用',
                        _ => '状态未知',
                      }),
                    ),
                    Align(
                      alignment: Alignment.centerRight,
                      child: OutlinedButton(
                        key: ValueKey('reset-use-${card.id}'),
                        onPressed: canUse && card.available
                            ? () => redeem(card)
                            : null,
                        child: Text(context.tr('使用这张')),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          if (s.available != null && s.available! > s.cards.length)
            OutlinedButton(
              key: const ValueKey('reset-use-next'),
              onPressed: canUse ? redeem : null,
              child: Text(context.tr('使用下一张可用重置卡')),
            ),
        ],
        if (busy) const LinearProgressIndicator(),
        if (message.isNotEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 12),
            child: Text(message, key: const ValueKey('reset-result')),
          ),
        if (pending != null) ...[
          Text(context.tr('有一笔重置结果待核对')),
          TextButton(
            key: const ValueKey('reset-retry'),
            onPressed: busy ? null : retryPending,
            child: Text(context.tr('核对原请求')),
          ),
          TextButton(
            onPressed: busy ? null : dismissPending,
            child: Text(context.tr('查看记录后结束核对')),
          ),
        ],
        const SizedBox(height: 14),
        Text(
          context.tr('重置卡记录'),
          style: const TextStyle(fontWeight: FontWeight.w600),
        ),
        if (windowStart.isNotEmpty)
          Text(
            context.tr('记录起始：{0}', [date(DateTime.tryParse(windowStart))]),
            style: const TextStyle(fontSize: 12),
          ),
        for (final event in history)
          ListTile(
            contentPadding: EdgeInsets.zero,
            title: Text(
              context.tr(switch (event['kind']) {
                'granted' => '获得重置卡',
                'redeemed' => '已使用重置卡',
                _ => '未知重置卡事件',
              }),
            ),
            subtitle: Text(
              date(DateTime.tryParse(event['occurred_at'] as String? ?? '')),
            ),
          ),
        if (historyLoading)
          const Padding(
            padding: EdgeInsets.all(14),
            child: CircularProgressIndicator(),
          ),
        if (historyError != null) ...[
          Text(historyError!),
          TextButton(
            onPressed: () => loadHistory(reset: history.isEmpty),
            child: Text(context.tr('重试读取记录')),
          ),
        ] else if (!historyLoading && history.isEmpty)
          Text(context.tr('当前返回范围内暂无记录')),
        if (next.isNotEmpty && !historyLoading)
          TextButton(onPressed: loadHistory, child: Text(context.tr('更多记录'))),
      ],
    );
  }
}
