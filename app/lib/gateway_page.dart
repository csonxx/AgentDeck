import 'language.dart';
import 'dart:async';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import 'core/client.dart';
import 'core/gateway.dart';
import 'core/relay.dart';

class GatewayPage extends StatefulWidget {
  final AgentClient client;
  const GatewayPage({super.key, required this.client});
  @override
  State<GatewayPage> createState() => _GatewayPageState();
}

class _GatewayPageState extends State<GatewayPage> {
  final url = TextEditingController();
  GatewayLogin? login;
  GatewayPair? pairing;
  Timer? timer;
  List<Map<String, dynamic>> nodes = [];
  bool busy = false, polling = false, confirmed = false;
  String message = '', error = '';
  @override
  void initState() {
    super.initState();
    _restore();
  }

  Future<void> _restore() async {
    try {
      final saved = await widget.client.vault.loadGatewayAccount();
      if (!mounted || saved == null || login != null) return;
      url.text = saved['url'] as String;
      login = GatewayLogin(url.text)
        ..account = Map<String, dynamic>.from(saved['account'] as Map);
      final current = login!;
      final list = await current.locals();
      if (mounted && identical(login, current)) setState(() => nodes = list);
    } catch (_) {
      if (mounted) setState(() => message = '请重新用飞书登录网关');
    }
  }

  @override
  void dispose() {
    timer?.cancel();
    login?.close();
    url.dispose();
    super.dispose();
  }

  Future<void> _begin() async {
    setState(() {
      busy = true;
      error = '';
      message = '';
    });
    try {
      timer?.cancel();
      login?.close();
      login = GatewayLogin(url.text);
      await login!.begin();
      if (!mounted) return;
      final launched = await launchUrl(
        Uri.parse(login!.loginUrl),
        mode: LaunchMode.externalApplication,
      );
      if (!launched) throw const FormatException('无法打开飞书授权页面');
      setState(() => message = '在授权页用飞书扫码或授权，完成后返回这里');
      timer = Timer.periodic(const Duration(seconds: 2), (_) => _poll());
      await _poll();
    } on FormatException catch (e) {
      if (mounted) setState(() => error = e.message);
    } catch (_) {
      if (mounted) setState(() => error = '网关登录失败，请检查域名和飞书配置');
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> _poll() async {
    if (polling || !mounted || login == null) return;
    polling = true;
    final current = login!;
    try {
      if (await current.poll()) {
        if (!mounted || !identical(login, current)) return;
        timer?.cancel();
        await widget.client.vault.saveGatewayAccount({
          'url': current.origin,
          'account': current.account,
        });
        final list = await current.locals();
        if (mounted && identical(login, current)) {
          setState(() {
            nodes = list;
            message = list.isEmpty ? '这个飞书账号还没有关联工作站' : '选择你已关联的工作站';
          });
        }
      }
    } on FormatException catch (e) {
      timer?.cancel();
      if (mounted) setState(() => error = e.message);
    } catch (_) {
      if (mounted) setState(() => error = '暂时无法查询登录结果，请检查网络');
    } finally {
      polling = false;
    }
  }

  Future<void> _offer(String local) async {
    setState(() {
      busy = true;
      error = '';
    });
    try {
      final p = await login!.offer(local);
      if (mounted) {
        setState(() {
          pairing = p;
          confirmed = false;
          message = '';
        });
      }
    } catch (_) {
      if (mounted) setState(() => error = '工作站未在线或配对请求失败，请稍后重试');
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> _finish() async {
    if (polling || !mounted || !confirmed || pairing == null) return;
    polling = true;
    try {
      await pairing!.complete(widget.client);
      timer?.cancel();
      if (mounted) Navigator.pop(context);
    } on ApprovalPending {
      if (mounted) setState(() => message = '等待工作站确认。仅在两边校验码一致时批准。');
    } on FormatException catch (e) {
      timer?.cancel();
      if (mounted) setState(() => error = e.message);
    } catch (_) {
      timer?.cancel();
      if (mounted) setState(() => error = '配对失败或请求已过期，请返回重新选择工作站');
    } finally {
      polling = false;
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(context.tr('通过网关连接'))),
    body: SafeArea(
      child: ListView(
        padding: const EdgeInsets.all(24),
        children: [
          Text(
            context.tr('飞书确认。\n私密会话。'),
            style: TextStyle(fontSize: 24, height: 1.3),
          ),
          const SizedBox(height: 20),
          if (pairing == null) ...[
            Text(context.tr('用同一飞书账号关联工作站与手机。网关匹配你的设备，会话内容由手机和工作站加密。')),
            const SizedBox(height: 24),
            TextField(
              controller: url,
              keyboardType: TextInputType.url,
              autocorrect: false,
              enableSuggestions: false,
              decoration: InputDecoration(
                labelText: context.tr('网关 HTTPS 域名'),
                hintText: 'https://agent.example.com/agentdeck',
              ),
            ),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: busy ? null : _begin,
              child: Text(busy ? context.tr('正在连接') : context.tr('用飞书登录')),
            ),
            if (login?.account != null) ...[
              const SizedBox(height: 24),
              Text(
                context.tr('已登录：{0}', [
                  (login!.account!['identity'] as Map)['name'],
                ]),
              ),
              const SizedBox(height: 12),
              for (final n in nodes)
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: const Icon(Icons.computer),
                  title: Text(n['name'] as String),
                  subtitle: Text(
                    n['online'] == true
                        ? context.tr('在线 · 可以绑定')
                        : context.tr('离线 · 等待工作站连接'),
                  ),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: n['online'] == true && !busy
                      ? () => _offer(n['id'] as String)
                      : null,
                ),
            ],
          ] else ...[
            Text(context.tr('核对工作站和手机上显示的校验码，完全一致后，在工作站确认这次配对。')),
            const SizedBox(height: 30),
            Semantics(
              label: context.tr('配对校验码 {0}', [pairing!.displayCode]),
              child: SelectableText(
                pairing!.displayCode,
                style: const TextStyle(
                  fontSize: 20,
                  fontFamily: 'monospace',
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
            const SizedBox(height: 30),
            FilledButton(
              onPressed: confirmed
                  ? null
                  : () {
                      setState(() => confirmed = true);
                      timer = Timer.periodic(
                        const Duration(seconds: 2),
                        (_) => _finish(),
                      );
                      _finish();
                    },
              child: Text(context.tr('两边一致，等待工作站确认')),
            ),
            TextButton(
              onPressed: () {
                timer?.cancel();
                setState(() {
                  pairing = null;
                  confirmed = false;
                  message = '';
                });
              },
              child: Text(context.tr('返回工作站列表')),
            ),
          ],
          if (message.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 20),
              child: Text(context.tr(message)),
            ),
          if (error.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 20),
              child: Text(
                context.trError(error),
                style: const TextStyle(color: Colors.red),
              ),
            ),
        ],
      ),
    ),
  );
}
