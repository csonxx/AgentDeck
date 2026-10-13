import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'core/app_lock.dart';
import 'design.dart';
import 'language.dart';

class AppLockScope extends InheritedNotifier<AppLockController> {
  const AppLockScope({
    super.key,
    required AppLockController controller,
    required super.child,
  }) : super(notifier: controller);
  static AppLockController of(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppLockScope>()!.notifier!;
}

class AppLockGate extends StatefulWidget {
  final AppLockController controller;
  final Widget child;
  const AppLockGate({super.key, required this.controller, required this.child});
  @override
  State<AppLockGate> createState() => _AppLockGateState();
}

class _AppLockGateState extends State<AppLockGate> with WidgetsBindingObserver {
  bool obscured = false;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.paused) widget.controller.lock();
    if (mounted) setState(() => obscured = state != AppLifecycleState.resumed);
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.controller,
    builder: (context, _) => Stack(
      fit: StackFit.expand,
      children: [
        Offstage(
          offstage: obscured || widget.controller.locked,
          child: widget.child,
        ),
        if (obscured)
          const ColoredBox(color: Colors.white)
        else if (widget.controller.locked)
          Positioned.fill(child: PinUnlockPage(controller: widget.controller)),
      ],
    ),
  );
}

class PinUnlockPage extends StatefulWidget {
  final AppLockController controller;
  const PinUnlockPage({super.key, required this.controller});
  @override
  State<PinUnlockPage> createState() => _PinUnlockPageState();
}

class _PinUnlockPageState extends State<PinUnlockPage> {
  String pin = '', message = '';
  Timer? timer;
  @override
  void initState() {
    super.initState();
    timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) {
        setState(() {});
      }
    });
  }

  @override
  void dispose() {
    timer?.cancel();
    super.dispose();
  }

  Future<void> digit(String digit) async {
    final c = widget.controller;
    if (c.busy || c.remaining > Duration.zero || c.failed || pin.length >= 4) {
      return;
    }
    setState(() => pin += digit);
    if (pin.length != 4) return;
    final ok = await c.verify(pin);
    if (!mounted) return;
    setState(() {
      pin = '';
      message = ok ? '' : '密码错误，请重试';
    });
  }

  @override
  Widget build(BuildContext context) {
    final c = widget.controller;
    final seconds = c.remaining.inSeconds;
    final waiting = c.remaining > Duration.zero;
    return PopScope(
      canPop: false,
      child: Scaffold(
        backgroundColor: Colors.white,
        body: SafeArea(
          child: Center(
            child: SingleChildScrollView(
              child: SizedBox(
                width: 300,
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Icon(
                      Icons.lock_outline_rounded,
                      size: 32,
                      color: DeckColors.ink,
                    ),
                    const SizedBox(height: 24),
                    Text(
                      context.tr('输入启动密码'),
                      style: const TextStyle(
                        fontSize: 22,
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                    const SizedBox(height: 14),
                    if (!c.ready)
                      const CircularProgressIndicator()
                    else if (c.failed) ...[
                      Text(context.tr('安全存储读取失败，请重试')),
                      TextButton(
                        onPressed: c.load,
                        child: Text(context.tr('重试')),
                      ),
                    ] else ...[
                      Semantics(
                        label: context.tr('已输入 {0} 位', [pin.length]),
                        child: ExcludeSemantics(
                          child: Row(
                            mainAxisAlignment: MainAxisAlignment.center,
                            children: List.generate(
                              4,
                              (i) => Padding(
                                padding: const EdgeInsets.all(9),
                                child: Icon(
                                  i < pin.length
                                      ? Icons.circle
                                      : Icons.circle_outlined,
                                  size: 14,
                                  color: DeckColors.ink,
                                ),
                              ),
                            ),
                          ),
                        ),
                      ),
                      SizedBox(
                        height: 50,
                        child: Center(
                          child: Text(
                            waiting
                                ? context.tr('输错 5 次，请 {0} 后重试', [
                                    '${seconds ~/ 60}:${(seconds % 60).toString().padLeft(2, '0')}',
                                  ])
                                : context.tr(message),
                            textAlign: TextAlign.center,
                            style: const TextStyle(
                              fontSize: 12,
                              color: DeckColors.muted,
                            ),
                          ),
                        ),
                      ),
                      for (final row in [
                        ['1', '2', '3'],
                        ['4', '5', '6'],
                        ['7', '8', '9'],
                        ['', '0', '⌫'],
                      ])
                        Row(
                          children: [
                            for (final d in row)
                              Expanded(
                                child: SizedBox(
                                  height: 72,
                                  child: d.isEmpty
                                      ? const SizedBox()
                                      : TextButton(
                                          key: ValueKey('pin-$d'),
                                          onPressed: waiting || c.busy
                                              ? null
                                              : () => d == '⌫'
                                                    ? setState(
                                                        () => pin = pin.isEmpty
                                                            ? ''
                                                            : pin.substring(
                                                                0,
                                                                pin.length - 1,
                                                              ),
                                                      )
                                                    : unawaited(digit(d)),
                                          child: Text(
                                            d,
                                            style: const TextStyle(
                                              fontSize: 25,
                                              color: DeckColors.ink,
                                            ),
                                          ),
                                        ),
                                ),
                              ),
                          ],
                        ),
                      if (c.busy)
                        const SizedBox(
                          height: 18,
                          width: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        ),
                    ],
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

Future<String?> askPin(BuildContext context, String title) async {
  final input = TextEditingController();
  try {
    return await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(ctx.tr(title)),
        content: TextField(
          key: const ValueKey('pin-input'),
          controller: input,
          autofocus: true,
          obscureText: true,
          enableSuggestions: false,
          autocorrect: false,
          keyboardType: TextInputType.number,
          inputFormatters: [
            FilteringTextInputFormatter.digitsOnly,
            LengthLimitingTextInputFormatter(4),
          ],
          decoration: InputDecoration(hintText: ctx.tr('4 位数字')),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(ctx.tr('取消')),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, input.text),
            child: Text(ctx.tr('确认')),
          ),
        ],
      ),
    );
  } finally {
    // Dialog exit animations can still reference the field for one frame.
    await Future<void>.delayed(const Duration(milliseconds: 300));
    input.dispose();
  }
}

class AppLockSettings extends StatelessWidget {
  final AppLockController controller;
  const AppLockSettings({super.key, required this.controller});
  Future<void> edit(BuildContext context, bool disable) async {
    final c = controller;
    try {
      if (c.remaining > Duration.zero) {
        throw FormatException('连续输错 5 次，请等待 10 分钟后重试');
      }
      if (c.enabled) {
        final old = await askPin(context, '输入当前密码');
        if (old == null || !context.mounted) return;
        if (!await c.verify(old)) {
          throw FormatException(
            c.remaining > Duration.zero ? '连续输错 5 次，请等待 10 分钟后重试' : '密码错误，请重试',
          );
        }
      }
      if (disable) {
        await c.disable();
        return;
      }
      if (!context.mounted) return;
      final pin = await askPin(context, '设置启动密码');
      if (pin == null || !context.mounted) return;
      if (!RegExp(r'^\d{4}$').hasMatch(pin)) throw FormatException('请输入 4 位数字');
      final again = await askPin(context, '再次输入密码');
      if (again == null) return;
      if (again != pin) throw FormatException('两次密码不一致');
      await c.setPin(pin);
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
    listenable: controller,
    builder: (context, _) => Column(
      children: [
        ListTile(
          key: const ValueKey('app-lock-settings'),
          leading: const Icon(Icons.lock_outline),
          title: Text(context.tr('启动密码')),
          subtitle: Text(
            context.tr(controller.enabled ? '已开启 · 4 位数字' : '未开启 · 4 位数字'),
          ),
          onTap: controller.ready && !controller.failed
              ? () => edit(context, false)
              : null,
          trailing: const Icon(Icons.chevron_right),
        ),
        if (controller.enabled)
          ListTile(
            title: Text(context.tr('关闭启动密码')),
            onTap: () => edit(context, true),
          ),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 4),
          child: Text(
            context.tr('连续输错 5 次，固定冷却 10 分钟；关闭或修改需验证当前密码。'),
            style: const TextStyle(fontSize: 11, color: DeckColors.muted),
          ),
        ),
      ],
    ),
  );
}
