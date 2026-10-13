import 'language.dart';
import 'app_update_page.dart';
import 'device_name_setting.dart';
import 'app_lock.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import 'core/client.dart';
import 'core/agents.dart';
import 'design.dart';

class AgentSettingsPage extends StatefulWidget {
  final AgentClient client;
  const AgentSettingsPage({super.key, required this.client});
  @override
  State<AgentSettingsPage> createState() => _AgentSettingsPageState();
}

class _AgentSettingsPageState extends State<AgentSettingsPage> {
  bool saving = false, authorizing = false, savingLanguage = false;
  Future<void> change(AgentInfo agent, bool visible) async {
    setState(() => saving = true);
    try {
      await widget.client.updateAgentVisibility(
        widget.client.agentVisibility.setVisible(agent.id, visible),
      );
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.trError(e))));
      }
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  Future<void> chooseLanguage() async {
    final controller = LanguageScope.of(context);
    if (controller == null || savingLanguage) return;
    final choice = await showModalBottomSheet<AppLanguage>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (context) => SafeArea(
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 0, 20, 12),
                child: Text(
                  context.tr('语言'),
                  style: const TextStyle(
                    fontSize: 18,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              for (final language in AppLanguage.values)
                ListTile(
                  key: ValueKey('language-${language.code}'),
                  title: Text(language.label),
                  trailing: controller.language == language
                      ? const Icon(Icons.check_rounded, size: 22)
                      : null,
                  onTap: () => Navigator.pop(context, language),
                ),
              const SizedBox(height: 8),
            ],
          ),
        ),
      ),
    );
    if (choice == null || !mounted || choice == controller.language) return;
    setState(() => savingLanguage = true);
    try {
      await controller.select(choice);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.tr('语言保存失败，请重试'))));
      }
    } finally {
      if (mounted) setState(() => savingLanguage = false);
    }
  }

  Future<void> login() async {
    setState(() => authorizing = true);
    try {
      final result = await widget.client.agentLogin('workbuddy');
      final uri = Uri.parse(result['url'] as String);
      if (uri.scheme != 'https' || uri.userInfo.isNotEmpty) {
        throw const FormatException('授权地址无效');
      }
      if (!await launchUrl(uri, mode: LaunchMode.externalApplication)) {
        throw const FormatException('无法打开授权页面');
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.trError(e))));
      }
    } finally {
      if (mounted) setState(() => authorizing = false);
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.client,
    builder: (context, _) => Scaffold(
      appBar: AppBar(title: Text(context.tr('设置'))),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          AppUpdateSetting(updater: widget.client.updates),
          DeviceNameSetting(client: widget.client),
          ListTile(
            key: const ValueKey('language-setting'),
            contentPadding: EdgeInsets.zero,
            leading: const Icon(Icons.language_rounded, size: 24),
            title: Text(context.tr('语言')),
            subtitle: Text(
              LanguageScope.of(context)?.language.label ?? 'English',
            ),
            trailing: savingLanguage
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 1.5),
                  )
                : const Icon(Icons.chevron_right_rounded),
            onTap: savingLanguage ? null : chooseLanguage,
          ),
          Text(
            context.tr('语言保存在此设备，立即生效。'),
            style: const TextStyle(color: DeckColors.muted, fontSize: 12),
          ),
          const Divider(height: 36),
          Text(
            context.tr('选择首页显示的 Agent'),
            style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 10),
          Text(
            context.tr('显示设置保存在手机上。隐藏后，会话和项目设置仍会保留。'),
            style: TextStyle(color: DeckColors.muted, fontSize: 13),
          ),
          const SizedBox(height: 18),
          for (final agent in widget.client.agents) ...[
            SwitchListTile.adaptive(
              contentPadding: EdgeInsets.zero,
              title: Text(agent.name),
              subtitle: Text(
                context.tr(agent.detail),
                style: const TextStyle(fontSize: 12),
              ),
              value: !widget.client.agentVisibility.hidden.contains(agent.id),
              onChanged: saving ? null : (v) => change(agent, v),
            ),
            if (agent.id == 'workbuddy')
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton.icon(
                  onPressed: widget.client.connected && !authorizing
                      ? login
                      : null,
                  icon: authorizing
                      ? const SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(strokeWidth: 1.5),
                        )
                      : const Icon(Icons.login, size: 18),
                  label: Text(context.tr('授权 WorkBuddy 本地助理')),
                ),
              ),
            const Divider(height: 16),
          ],
          const Divider(height: 24),
          AppLockSettings(controller: widget.client.appLock),
          const SizedBox(height: 12),
          Text(
            context.tr(
              'Qoder / TRAE 使用工作站 CLI 的登录状态。OpenCode 连接工作站服务。WorkBuddy 需要开放平台应用和用户授权。',
            ),
            style: TextStyle(color: DeckColors.muted, fontSize: 12),
          ),
        ],
      ),
    ),
  );
}
