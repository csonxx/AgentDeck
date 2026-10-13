import 'package:flutter/material.dart';
import 'core/client.dart';
import 'language.dart';

class DeviceNameSetting extends StatelessWidget {
  final AgentClient client;
  const DeviceNameSetting({super.key, required this.client});
  Future<void> change(BuildContext context) async {
    final input = TextEditingController(text: client.deviceName);
    final name = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(context.tr('设备显示名称')),
        content: TextField(
          controller: input,
          autofocus: true,
          maxLength: 80,
          decoration: InputDecoration(hintText: context.tr('例如：我的 OnePlus 7')),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(context.tr('取消')),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, input.text),
            child: Text(context.tr('保存')),
          ),
        ],
      ),
    );
    input.dispose();
    if (name == null || !context.mounted) return;
    try {
      await client.renameDevice(name);
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              context.tr(client.deviceNamePending ? '已保存，连接工作站后同步' : '设备名称已更新'),
            ),
          ),
        );
      }
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.trError(e))));
      }
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: client,
    builder: (context, _) => ListTile(
      key: const ValueKey('device-name-setting'),
      contentPadding: EdgeInsets.zero,
      leading: const Icon(Icons.phone_android_outlined),
      title: Text(context.tr('设备显示名称')),
      subtitle: Text(
        client.deviceName +
            (client.deviceNamePending ? ' · ${context.tr('连接后同步')}' : ''),
      ),
      trailing: const Icon(Icons.edit_outlined),
      onTap: () => change(context),
    ),
  );
}
