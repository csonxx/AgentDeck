import 'package:flutter/material.dart';
import 'core/display.dart';
import 'language.dart';

class DisplayScope extends InheritedNotifier<DisplayController> {
  const DisplayScope({
    super.key,
    required DisplayController controller,
    required super.child,
  }) : super(notifier: controller);
  static DisplayController? of(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<DisplayScope>()?.notifier;
}

/// Scale the entire Navigator (including overlays), not just its text. Layout
/// and hit testing use the same transform. Hardware/keyboard insets remain at
/// their physical size and the system accessibility text scaler is preserved.
class DisplayScale extends StatelessWidget {
  final DisplayController controller;
  final Widget child;
  const DisplayScale({
    super.key,
    required this.controller,
    required this.child,
  });
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: controller,
    builder: (context, _) => LayoutBuilder(
      builder: (context, constraints) {
        final scale = controller.size.scale;
        final size = Size(
          constraints.maxWidth / scale,
          constraints.maxHeight / scale,
        );
        final media = MediaQuery.of(context);
        // Keep this widget path stable even at 1.0 so toggling sizes preserves
        // routes, drafts, focus and scroll controllers in the existing child.
        return SizedBox.expand(
          child: FittedBox(
            fit: BoxFit.fill,
            alignment: Alignment.topLeft,
            child: SizedBox(
              width: size.width,
              height: size.height,
              child: MediaQuery(
                data: media.copyWith(
                  size: size,
                  padding: media.padding / scale,
                  viewPadding: media.viewPadding / scale,
                  viewInsets: media.viewInsets / scale,
                  systemGestureInsets: media.systemGestureInsets / scale,
                ),
                child: child,
              ),
            ),
          ),
        );
      },
    ),
  );
}

class DisplaySizeSetting extends StatefulWidget {
  const DisplaySizeSetting({super.key});
  @override
  State<DisplaySizeSetting> createState() => _DisplaySizeSettingState();
}

class _DisplaySizeSettingState extends State<DisplaySizeSetting> {
  bool saving = false;
  Future<void> choose(DisplayController controller) async {
    if (saving) return;
    final choice = await showModalBottomSheet<InterfaceSize>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (context) => SafeArea(
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              ListTile(
                title: Text(context.tr('界面大小')),
                subtitle: Text(context.tr('文字、图标、间距和卡片同步调整')),
              ),
              for (final value in InterfaceSize.values)
                ListTile(
                  key: ValueKey('display-size-${value.code}'),
                  title: Text(context.tr(value.label)),
                  trailing: controller.size == value
                      ? const Icon(Icons.check_rounded)
                      : null,
                  onTap: () => Navigator.pop(context, value),
                ),
              const SizedBox(height: 12),
            ],
          ),
        ),
      ),
    );
    if (!mounted || choice == null || choice == controller.size) return;
    setState(() => saving = true);
    try {
      await controller.select(choice);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.tr('界面大小保存失败，请重试'))));
      }
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final controller = DisplayScope.of(context);
    if (controller == null) return const SizedBox.shrink();
    return ListTile(
      key: const ValueKey('display-size-setting'),
      contentPadding: EdgeInsets.zero,
      leading: const Icon(Icons.format_size_rounded),
      title: Text(context.tr('界面大小')),
      subtitle: Text(context.tr(controller.size.label)),
      trailing: saving
          ? const SizedBox(
              width: 18,
              height: 18,
              child: CircularProgressIndicator(strokeWidth: 1.5),
            )
          : const Icon(Icons.chevron_right_rounded),
      onTap: saving ? null : () => choose(controller),
    );
  }
}
