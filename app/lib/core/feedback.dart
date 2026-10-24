import 'dart:convert';
import 'dart:io';
import 'package:flutter/foundation.dart';
import 'crypto.dart';
import 'diagnostics.dart';
import 'vault.dart';

class FeedbackManager extends ChangeNotifier {
  final DeviceVault vault;
  final DiagnosticLog log;
  final Future<dynamic> Function(Map<String, dynamic>) exchange;
  final String Function() binding;
  final bool Function() connected, supported;
  final Future<String> Function() version;
  FeedbackManager({
    required this.vault,
    required this.log,
    required this.exchange,
    required this.binding,
    required this.connected,
    required this.supported,
    required this.version,
  });
  Map<String, dynamic>? pending;
  String pendingBinding = '', lastID = '', error = '';
  bool busy = false, _disposed = false, _loaded = false;
  Future<void>? _loading;
  Future<void> _writes = Future.value();
  int _generation = 0;
  void _notify() {
    if (!_disposed) notifyListeners();
  }

  Future<void> initialize() => _loading ??= () async {
    await log.initialize();
    try {
      final saved = await vault.loadFeedback();
      pending = saved['pending'] is Map
          ? Map<String, dynamic>.from(saved['pending'])
          : null;
      pendingBinding = saved['binding'] as String? ?? '';
      lastID = saved['last_id'] as String? ?? '';
      _loaded = true;
    } catch (_) {
      error = '反馈记录读取失败';
      _loading = null;
    }
    _notify();
  }();

  Future<void> _save() {
    final value = Map<String, dynamic>.from(
      jsonDecode(
        jsonEncode({
          'pending': pending,
          'binding': pendingBinding,
          'last_id': lastID,
        }),
      ),
    );
    final write = _writes.then((_) => vault.saveFeedback(value));
    _writes = write.catchError((Object _) {});
    return write;
  }

  Future<Map<String, dynamic>> snapshot(
    String message,
    bool includeDiagnostics,
  ) async {
    message = message.trim();
    if (message.isEmpty || utf8.encode(message).length > 16384) {
      throw const FormatException('请填写问题说明，最多 16 KiB');
    }
    await log.initialize();
    final id = WireCrypto.random(
      16,
    ).map((v) => v.toRadixString(16).padLeft(2, '0')).join();
    return {
      'id': 'AD-FB-$id',
      'message': message,
      'app_version': await version(),
      'platform': Platform.operatingSystem,
      if (includeDiagnostics) 'diagnostics': log.records,
    };
  }

  Future<bool> submit(String message, bool includeDiagnostics) async {
    if (busy) return false;
    final generation = _generation;
    busy = true;
    error = '';
    _notify();
    try {
      await initialize();
      if (generation != _generation) return false;
      if (!_loaded) throw const FormatException('反馈记录读取失败');
      if (pending == null) {
        // Freeze identity, report and destination BEFORE transmission. Retrying
        // an uncertain response must not collect new records under the same ID.
        final target = binding();
        final report = await snapshot(message, includeDiagnostics);
        if (generation != _generation) return false;
        if (target != binding()) throw const FormatException('工作站已切换，请重新提交反馈');
        pending = report;
        pendingBinding = target;
        await _save();
        if (generation != _generation) return false;
      }
      if (pendingBinding != binding()) {
        throw const FormatException('请连接提交反馈时的工作站');
      }
      if (!connected()) throw const FormatException('工作站未连接，反馈已保留，可稍后重试');
      if (!supported()) throw const FormatException('请更新本机后端后提交，或复制诊断记录');
      final response = await exchange({
        'method': 'feedback_submit',
        'feedback': pending,
      });
      if (generation != _generation) return false;
      if (response is! Map ||
          response['id'] != pending!['id'] ||
          response['stored'] != 'workstation') {
        throw const FormatException('反馈回执校验失败，记录已保留');
      }
      final previous = pending;
      lastID = pending!['id'];
      pending = null;
      try {
        await _save();
      } catch (_) {
        pending = previous;
        rethrow;
      }
      return true;
    } catch (e) {
      if (generation != _generation) return false;
      // Do not expose raw network exceptions, URLs or credentials in reports.
      error = e is FormatException ? e.message : '反馈尚未确认，已保留，可重试';
      return false;
    } finally {
      busy = false;
      _notify();
    }
  }

  Future<void> reset() async {
    ++_generation;
    await _loading;
    await _writes;
    pending = null;
    pendingBinding = '';
    lastID = '';
    error = '';
    _loaded = true;
    _notify();
  }

  Future<void> discard() async {
    if (busy) return;
    await initialize();
    if (!_loaded) throw const FormatException('反馈记录读取失败');
    final old = pending, oldBinding = pendingBinding;
    pending = null;
    pendingBinding = '';
    try {
      await _save();
    } catch (_) {
      pending = old;
      pendingBinding = oldBinding;
      rethrow;
    }
    error = '';
    _notify();
  }

  @override
  void dispose() {
    ++_generation;
    _disposed = true;
    super.dispose();
  }
}
