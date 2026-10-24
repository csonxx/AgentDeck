import 'dart:async';
import 'package:flutter/foundation.dart';
import 'clock.dart';
import 'vault.dart';

/// Bounded, encrypted metadata. Never accept arbitrary exception text, URLs,
/// messages, project names, native payloads, credentials or attachments.
class DiagnosticLog extends ChangeNotifier {
  static const capacity = 500;
  static const codes = {
    'send_started',
    'send_ack',
    'send_error',
    'send_prepared',
    'send_round_trip',
    'send_total',
    'send_running',
    'history_presented',
    'request_timeout',
    'request_error',
    'connected',
    'disconnected',
  };
  final DeviceVault vault;
  final ProtocolClock clock;
  DiagnosticLog(this.vault, this.clock);
  final _records = <Map<String, dynamic>>[];
  Future<void>? _loading;
  Future<void> _writes = Future.value();
  bool _scheduled = false;
  bool storageFailed = false, _disposed = false, enabled = true;
  List<Map<String, dynamic>> get records =>
      _records.map(Map<String, dynamic>.of).toList();

  static Map<String, dynamic>? _safe(Map<dynamic, dynamic> v) {
    if (!codes.contains(v['code']) || v['component'] != 'app') return null;
    final date = DateTime.tryParse('${v['time']}');
    final id = v['request_id'] ?? '';
    final ms = v['duration_ms'] ?? 0, status = v['status'] ?? 0;
    if (date == null ||
        id is! String ||
        id.isNotEmpty && !RegExp(r'^[a-f0-9]{32}$').hasMatch(id) ||
        ms is! int ||
        ms < 0 ||
        ms > 86400000 ||
        status is! int ||
        status < 0 ||
        status > 599) {
      return null;
    }
    return {
      'time': date.toUtc().toIso8601String(),
      'component': 'app',
      'code': v['code'],
      if (id.isNotEmpty) 'request_id': id,
      if (ms > 0) 'duration_ms': ms,
      if (status > 0) 'status': status,
    };
  }

  Future<void> initialize() => _loading ??= () async {
    try {
      final saved = await vault.loadDiagnostics();
      final old = (saved['records'] as List? ?? [])
          .whereType<Map>()
          .map(_safe)
          .whereType<Map<String, dynamic>>()
          .toList();
      _records.insertAll(0, old);
      _trim();
    } catch (_) {
      storageFailed = true;
    }
  }();

  void _trim() {
    final cutoff = clock.millis - const Duration(days: 7).inMilliseconds;
    _records.removeWhere(
      (v) => DateTime.parse(v['time']).millisecondsSinceEpoch < cutoff,
    );
    if (_records.length > capacity) {
      _records.removeRange(0, _records.length - capacity);
    }
  }

  void record(
    String code, {
    String request = '',
    int duration = 0,
    int status = 0,
  }) {
    if (_disposed || !enabled) return;
    final v = _safe({
      'time': DateTime.fromMillisecondsSinceEpoch(
        clock.millis,
        isUtc: true,
      ).toIso8601String(),
      'component': 'app',
      'code': code,
      'request_id': request,
      'duration_ms': duration,
      'status': status,
    });
    if (v == null) return;
    unawaited(initialize());
    _records.add(v);
    _trim();
    if (!_scheduled) {
      _scheduled = true;
      scheduleMicrotask(() {
        if (!_scheduled) return;
        _scheduled = false;
        unawaited(flush());
      });
    }
    notifyListeners();
  }

  Future<void> flush() {
    _scheduled = false;
    final write = _writes.then((_) async {
      await initialize();
      _trim();
      await vault.saveDiagnostics({'records': records});
      storageFailed = false;
    });
    _writes = write.catchError((Object _) {
      storageFailed = true;
    });
    return _writes;
  }

  Future<void> clear() async {
    await initialize();
    _records.clear();
    await flush();
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    unawaited(flush());
    super.dispose();
  }
}
