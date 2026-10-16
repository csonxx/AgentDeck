import 'package:flutter/foundation.dart';
import 'vault.dart';

enum InterfaceSize {
  standard('standard', '标准', 1),
  small('small', '小一号', .9);

  final String code, label;
  final double scale;
  const InterfaceSize(this.code, this.label, this.scale);
  static InterfaceSize fromCode(String? code) =>
      values.firstWhere((value) => value.code == code, orElse: () => standard);
}

/// Local secure preference; independent of pairing and agent settings.
class DisplayController extends ChangeNotifier {
  final DeviceVault vault;
  InterfaceSize _size;
  bool _changed = false, _disposed = false;
  Future<void> _queue = Future.value();
  DisplayController(
    this.vault, {
    InterfaceSize initial = InterfaceSize.standard,
  }) : _size = initial;
  InterfaceSize get size => _size;
  Future<void> load() async {
    try {
      final saved = await vault.loadDisplaySize();
      if (_changed || _disposed) return;
      _size = InterfaceSize.fromCode(saved);
      notifyListeners();
    } catch (_) {
      // Keep the current size when secure storage is unavailable.
    }
  }

  Future<void> select(InterfaceSize value) {
    _changed = true;
    final save = _queue.then((_) async {
      await vault.saveDisplaySize(value.code);
      if (!_disposed) {
        _size = value;
        notifyListeners();
      }
    });
    _queue = save.catchError((Object _) {});
    return save;
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}
