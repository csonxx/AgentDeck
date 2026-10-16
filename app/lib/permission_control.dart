import 'dart:async';
import 'package:flutter/material.dart';
import 'core/client.dart';
import 'core/permissions.dart';
import 'design.dart';
import 'language.dart';

class PermissionControl extends StatefulWidget {
  final AgentClient client;
  final String session;
  const PermissionControl({
    super.key,
    required this.client,
    required this.session,
  });
  @override
  State<PermissionControl> createState() => _PermissionControlState();
}

class _PermissionControlState extends State<PermissionControl>
    with WidgetsBindingObserver {
  SessionPermissions? current;
  Object? readFailure;
  bool loading = false;
  Future<SessionPermissions?>? reading;
  bool opening = false;
  bool wasConnected = false;
  Timer? poll;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    widget.client.addListener(_connected);
    wasConnected = widget.client.connected;
    unawaited(_read());
    poll = Timer.periodic(const Duration(seconds: 15), (_) {
      if (mounted && TickerMode.of(context)) {
        unawaited(_read());
      }
    });
  }

  void _connected() {
    if (widget.client.connected && !wasConnected) {
      unawaited(_read());
    }
    wasConnected = widget.client.connected;
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) unawaited(_read());
  }

  @override
  void didUpdateWidget(covariant PermissionControl old) {
    super.didUpdateWidget(old);
    if (old.client != widget.client) {
      old.client.removeListener(_connected);
      widget.client.addListener(_connected);
    }
    if (old.session != widget.session) {
      current = null;
      unawaited(_read());
    }
  }

  Future<SessionPermissions?> _read() {
    if (reading != null) return reading!;
    if (!widget.client.connected) return Future.value(null);
    final task = _load();
    reading = task;
    unawaited(task.whenComplete(() => reading = null));
    return task;
  }

  Future<SessionPermissions?> _load() async {
    loading = true;
    final id = widget.session;
    try {
      final v = await widget.client.sessionPermissions(id);
      if (mounted && id == widget.session) {
        setState(() {
          current = v;
          readFailure = null;
        });
      }
      return v;
    } catch (e) {
      if (mounted && id == widget.session) {
        setState(() {
          readFailure = e;
          current = null;
        });
      }
      return null;
    } finally {
      loading = false;
    }
  }

  Future<void> _open() async {
    setState(() => opening = true);
    final fresh = await _read();
    if (!mounted) return;
    setState(() => opening = false);
    SessionPermissions? value = fresh ?? current;
    String? failure = fresh == null
        ? (readFailure == null
              ? context.tr('无法读取权限，请重试')
              : context.trError(readFailure!))
        : null;
    bool saving = false;
    bool loaded = fresh != null;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (sheet) => StatefulBuilder(
        builder: (context, set) => SafeArea(
          child: SingleChildScrollView(
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          context.tr('会话权限'),
                          style: const TextStyle(
                            fontSize: 20,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                      IconButton(
                        tooltip: context.tr('刷新'),
                        onPressed: saving
                            ? null
                            : () async {
                                set(() => saving = true);
                                final next = await _read();
                                if (!context.mounted) return;
                                set(() {
                                  saving = false;
                                  if (next != null) {
                                    value = next;
                                    loaded = true;
                                    failure = null;
                                  } else {
                                    failure = readFailure == null
                                        ? context.tr('无法读取权限，请重试')
                                        : context.trError(readFailure!);
                                  }
                                });
                              },
                        icon: const Icon(Icons.refresh),
                      ),
                    ],
                  ),
                  Text(
                    value == null
                        ? context.tr('权限未提供')
                        : context.tr(value!.label),
                    key: const ValueKey('permission-current'),
                    style: const TextStyle(fontWeight: FontWeight.w600),
                  ),
                  if (value?.detail.isNotEmpty == true)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 8),
                      child: SelectableText(context.tr(value!.detail)),
                    ),
                  if (failure != null)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 8),
                      child: Text(
                        failure!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                    ),
                  if (saving) const LinearProgressIndicator(),
                  for (final choice in value?.choices ?? <PermissionChoice>[])
                    ListTile(
                      key: ValueKey('permission-choice-${choice.id}'),
                      contentPadding: EdgeInsets.zero,
                      title: Text(choice.label),
                      subtitle: Text(
                        context.tr(
                          choice.detail.isEmpty ? choice.id : choice.detail,
                        ),
                      ),
                      trailing: value!.mode == choice.id
                          ? const Icon(Icons.check)
                          : null,
                      enabled: !saving && value!.editable && loaded,
                      onTap: choice.id == value!.mode
                          ? null
                          : () async {
                              set(() => saving = true);
                              try {
                                final next = await widget.client
                                    .changePermissions(
                                      widget.session,
                                      value!.version,
                                      choice.id,
                                    );
                                if (!context.mounted) return;
                                set(() {
                                  value = next;
                                  failure = null;
                                });
                                if (mounted) setState(() => current = next);
                              } catch (e) {
                                if (!context.mounted) return;
                                set(() => failure = context.trError(e));
                                final next = await _read();
                                if (context.mounted && next != null) {
                                  set(() => value = next);
                                }
                              } finally {
                                if (context.mounted) set(() => saving = false);
                              }
                            },
                    ),
                  const SizedBox(height: 8),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  @override
  void dispose() {
    poll?.cancel();
    WidgetsBinding.instance.removeObserver(this);
    widget.client.removeListener(_connected);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => TextButton.icon(
    key: const ValueKey('session-permission-button'),
    onPressed: widget.client.connected && !opening ? _open : null,
    icon: opening
        ? const SizedBox(
            width: 14,
            height: 14,
            child: CircularProgressIndicator(strokeWidth: 1.5),
          )
        : const Icon(Icons.shield_outlined, size: 14),
    label: Text(
      current == null ? context.tr('权限未提供') : context.tr(current!.label),
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
      style: const TextStyle(fontSize: 11),
    ),
    style: TextButton.styleFrom(
      foregroundColor: DeckColors.muted,
      padding: const EdgeInsets.symmetric(horizontal: 5),
      minimumSize: const Size(0, 30),
      tapTargetSize: MaterialTapTargetSize.shrinkWrap,
    ),
  );
}
