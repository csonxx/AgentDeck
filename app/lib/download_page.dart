import 'package:flutter/material.dart';
import 'core/client.dart';
import 'core/download.dart';
import 'core/download_manager.dart';
import 'language.dart';
import 'attachment_page.dart' show attachmentSize;

class DownloadPage extends StatelessWidget {
  final AgentClient client;
  const DownloadPage({super.key, required this.client});
  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(context.tr('下载'))),
    body: ListenableBuilder(
      listenable: client.downloads,
      builder: (context, _) {
        final tasks = client.downloads.tasks.reversed.toList();
        if (client.downloads.restoreError.isNotEmpty) {
          return Center(child: Text(context.tr(client.downloads.restoreError)));
        }
        if (tasks.isEmpty) return Center(child: Text(context.tr('暂无下载任务')));
        return ListView.builder(
          itemCount: tasks.length,
          itemBuilder: (context, i) => Padding(
            padding: const EdgeInsets.all(16),
            child: DownloadTaskView(client: client, task: tasks[i]),
          ),
        );
      },
    ),
  );
}

class DownloadTaskView extends StatelessWidget {
  final AgentClient client;
  final DownloadTask task;
  const DownloadTaskView({super.key, required this.client, required this.task});
  @override
  Widget build(BuildContext context) => Column(
    mainAxisSize: MainAxisSize.min,
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      Text(task.name, maxLines: 2, overflow: TextOverflow.ellipsis),
      const SizedBox(height: 8),
      if (!task.complete)
        LinearProgressIndicator(
          value: task.total > 0
              ? (task.received / task.total).clamp(0, 1)
              : null,
        ),
      const SizedBox(height: 8),
      Text(
        '${attachmentSize(task.received)} / ${attachmentSize(task.total)} · ${context.tr(task.complete
            ? '已保存到所选位置'
            : task.status == 'pausing'
            ? '正在暂停…'
            : task.status == 'waiting'
            ? '等待连接'
            : task.running
            ? '下载中，可返回或切换应用'
            : '已暂停')}',
      ),
      if (task.error.isNotEmpty) Text(context.tr(task.error)),
      Wrap(
        spacing: 8,
        children: [
          if (task.complete)
            TextButton(
              onPressed: () async {
                try {
                  await DownloadDestination(task.id).open();
                } catch (_) {
                  if (context.mounted) {
                    ScaffoldMessenger.of(context).showSnackBar(
                      SnackBar(content: Text(context.tr('请使用手机文件管理器打开已保存的附件'))),
                    );
                  }
                }
              },
              child: Text(context.tr('其他应用打开')),
            )
          else if (task.running)
            TextButton(
              onPressed: () => client.downloads.pause(task),
              child: Text(context.tr('暂停')),
            )
          else ...[
            TextButton(
              onPressed: task.control != null
                  ? null
                  : () => client.downloads.resume(task),
              child: Text(context.tr('继续下载')),
            ),
            TextButton(
              onPressed: task.control != null
                  ? null
                  : () => client.downloads.restart(task),
              child: Text(context.tr('重新下载')),
            ),
          ],
          TextButton(
            onPressed: () async {
              final confirmed = await showDialog<bool>(
                context: context,
                builder: (context) => AlertDialog(
                  title: Text(
                    context.tr(task.complete ? '移除下载记录？' : '取消下载并删除未完成文件？'),
                  ),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.pop(context, false),
                      child: Text(context.tr('返回')),
                    ),
                    TextButton(
                      onPressed: () => Navigator.pop(context, true),
                      child: Text(context.tr('确认')),
                    ),
                  ],
                ),
              );
              if (confirmed == true) {
                try {
                  await client.downloads.remove(task);
                } catch (_) {
                  if (context.mounted) {
                    ScaffoldMessenger.of(context).showSnackBar(
                      SnackBar(content: Text(context.tr('文件操作失败，请检查保存位置'))),
                    );
                  }
                }
              }
            },
            child: Text(context.tr(task.complete ? '移除记录' : '取消下载')),
          ),
        ],
      ),
    ],
  );
}
