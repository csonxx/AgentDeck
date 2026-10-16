import 'dart:async';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import 'core/app_update.dart';
import 'design.dart';
import 'language.dart';

void openAppUpdate(BuildContext context, AppUpdater updater) =>
    Navigator.of(context).push(
      MaterialPageRoute<void>(builder: (_) => AppUpdatePage(updater: updater)),
    );

class UpdateNotice extends StatelessWidget {
  final AppUpdater updater;
  const UpdateNotice({super.key, required this.updater});
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: updater,
    builder: (context, _) {
      if (!updater.available && updater.downloadId == null) {
        return const SizedBox.shrink();
      }
      return Padding(
        padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 6),
        child: Card(
          child: ListTile(
            dense: true,
            leading: const Icon(Icons.system_update_outlined),
            title: Text(
              updater.ready
                  ? context.tr('更新已下载，点击安装')
                  : context.tr('发现新版本 {0}', [
                      updater.latest?.version ??
                          updater.downloading?.version ??
                          '',
                    ]),
            ),
            trailing: const Icon(Icons.chevron_right),
            onTap: () => openAppUpdate(context, updater),
          ),
        ),
      );
    },
  );
}

class AppUpdateSetting extends StatelessWidget {
  final AppUpdater updater;
  const AppUpdateSetting({super.key, required this.updater});
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: updater,
    builder: (context, _) => ListTile(
      key: const ValueKey('app-update-setting'),
      contentPadding: EdgeInsets.zero,
      leading: const Icon(Icons.system_update_outlined),
      title: Text(context.tr('应用更新')),
      subtitle: Text(
        updater.available
            ? context.tr('发现新版本 {0}', [updater.latest!.version])
            : context.tr('自动检查与后台下载'),
      ),
      trailing: const Icon(Icons.chevron_right),
      onTap: () => openAppUpdate(context, updater),
    ),
  );
}

class AppUpdatePage extends StatefulWidget {
  final AppUpdater updater;
  const AppUpdatePage({super.key, required this.updater});
  @override
  State<AppUpdatePage> createState() => _AppUpdatePageState();
}

class _AppUpdatePageState extends State<AppUpdatePage> {
  AppUpdater get u => widget.updater;
  @override
  void initState() {
    super.initState();
    unawaited(u.initialize());
  }

  Future<void> browserDownload(AndroidRelease release) async {
    if (!await launchUrl(
          Uri.parse(release.url),
          mode: LaunchMode.externalApplication,
        ) &&
        mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(context.tr('无法打开浏览器'))));
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: u,
    builder: (context, _) {
      final release = u.downloading ?? u.latest;
      return Scaffold(
        appBar: AppBar(title: Text(context.tr('应用更新'))),
        body: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            Text(
              'AgentDeck',
              style: const TextStyle(fontSize: 26, fontWeight: FontWeight.w600),
            ),
            const SizedBox(height: 8),
            Text(
              context.tr('当前版本 {0}', [
                u.installedVersion.isEmpty ? '—' : u.installedVersion,
              ]),
              style: const TextStyle(color: DeckColors.muted),
            ),
            const SizedBox(height: 4),
            Text(
              context.tr(u.edition == 'lite' ? '轻量版 · 不含语音输入' : '完整版 · 离线语音输入'),
              style: const TextStyle(color: DeckColors.muted),
            ),
            const SizedBox(height: 24),
            SegmentedButton<String>(
              key: const ValueKey('update-edition-picker'),
              segments: [
                ButtonSegment(value: 'lite', label: Text(context.tr('轻量版'))),
                ButtonSegment(value: 'voice', label: Text(context.tr('带语音版'))),
              ],
              selected: {u.selectedEdition},
              onSelectionChanged: u.canSelectEdition
                  ? (values) => unawaited(u.setEdition(values.single))
                  : null,
            ),
            const SizedBox(height: 10),
            Text(
              context.tr(
                u.selectedEdition == 'lite' ? '轻量版 · 不含语音输入' : '完整版 · 离线语音输入',
              ),
              style: const TextStyle(color: DeckColors.muted),
            ),
            const SizedBox(height: 16),
            SwitchListTile.adaptive(
              contentPadding: EdgeInsets.zero,
              title: Text(context.tr('自动检查更新')),
              subtitle: Text(context.tr('打开应用时检查新版本；下载和安装由你确认。')),
              value: u.automatic,
              onChanged: u.supported ? u.setAutomatic : null,
            ),
            const SizedBox(height: 12),
            OutlinedButton.icon(
              onPressed: !u.supported || u.checking
                  ? null
                  : () => u.check(force: true),
              icon: u.checking
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 1.5),
                    )
                  : const Icon(Icons.refresh),
              label: Text(context.tr('检查更新')),
            ),
            if (!u.supported) Text(context.tr('自动更新目前支持 Android')),
            if (!u.checking &&
                u.checkedAt != null &&
                !u.available &&
                u.downloadId == null)
              Padding(
                padding: const EdgeInsets.only(top: 14),
                child: Text(
                  context.tr(
                    u.latest == null
                        ? '暂无可用更新'
                        : u.latest!.code < u.installedCode
                        ? '该版本较旧，暂不可切换'
                        : '已是最新版本',
                  ),
                ),
              ),
            if (release != null) ...[
              const Divider(height: 40),
              Text(
                context.tr('{0} · {1}', [
                  context.tr(release.edition == 'lite' ? '轻量版' : '带语音版'),
                  release.version,
                ]),
                style: const TextStyle(
                  fontSize: 21,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 8),
              Text(
                '${(release.bytes / 1000000).toStringAsFixed(1)} MB',
                style: const TextStyle(color: DeckColors.muted),
              ),
              if (release.notes.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 18),
                  child: SelectableText(release.notes),
                ),
              if (u.active || u.ready) ...[
                LinearProgressIndicator(
                  value: u.total > 0
                      ? (u.received / u.total).clamp(0.0, 1.0)
                      : null,
                ),
                const SizedBox(height: 10),
                Text(
                  u.ready
                      ? context.tr('下载完成')
                      : u.downloadState == 'paused'
                      ? context.tr('等待网络恢复，下载将自动继续')
                      : context.tr('正在下载 {0} / {1} MB', [
                          (u.received / 1000000).toStringAsFixed(1),
                          (u.total / 1000000).toStringAsFixed(1),
                        ]),
                ),
                const SizedBox(height: 8),
                Text(
                  context.tr('可以返回聊天或切到后台，系统会继续下载。'),
                  style: const TextStyle(color: DeckColors.muted, fontSize: 13),
                ),
              ],
              const SizedBox(height: 18),
              if (u.ready)
                FilledButton.icon(
                  onPressed: u.installing ? null : u.install,
                  icon: u.installing
                      ? const SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(strokeWidth: 1.5),
                        )
                      : const Icon(Icons.install_mobile_outlined),
                  label: Text(context.tr(u.installing ? '正在校验更新包' : '安装更新')),
                )
              else if (!u.active && u.available)
                FilledButton.icon(
                  onPressed: u.starting ? null : u.download,
                  icon: const Icon(Icons.download_outlined),
                  label: Text(context.tr('下载更新')),
                ),
              if (u.downloadId != null)
                TextButton(
                  onPressed: u.installing ? null : u.cancelDownload,
                  child: Text(context.tr('取消更新下载')),
                ),
              if (u.available || u.downloadId != null)
                TextButton.icon(
                  onPressed: () => browserDownload(release),
                  icon: const Icon(Icons.open_in_browser_outlined),
                  label: Text(context.tr('浏览器下载')),
                ),
              Text(
                context.tr('安装前校验文件完整性和应用签名，保留原有配对和设置。'),
                style: const TextStyle(color: DeckColors.muted, fontSize: 12),
              ),
            ],
            if (u.error.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 16),
                child: Text(
                  context.tr(u.error),
                  style: const TextStyle(color: Colors.red),
                ),
              ),
          ],
        ),
      );
    },
  );
}
