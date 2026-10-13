import 'dart:convert';
import 'package:cryptography/cryptography.dart';
import 'package:flutter/foundation.dart';
import 'crypto.dart';
import 'vault.dart';

// Secure storage keeps the salted verifier and the fixed cooldown together.
// No plaintext PIN is persisted or sent to the workstation/gateway.
class AppLockController extends ChangeNotifier {
  final DeviceVault vault;
  final DateTime Function() now;
  final int iterations;
  AppLockController(
    this.vault, {
    DateTime Function()? now,
    this.iterations = 120000,
  }) : now = now ?? DateTime.now;
  Map<String, dynamic>? _record;
  int _lockGeneration = 0;
  bool ready = false, unlocked = false, busy = false, failed = false;
  bool get enabled => _record != null;
  int get attempts => (_record?['attempts'] as num?)?.toInt() ?? 0;
  Duration get remaining {
    final until = (_record?['until'] as num?)?.toInt() ?? 0;
    final ms = until - now().millisecondsSinceEpoch;
    return Duration(milliseconds: ms > 0 ? ms : 0);
  }

  bool get locked => !ready || failed || (enabled && !unlocked);
  Future<void> load() async {
    try {
      final v = await vault.loadAppLock();
      if (v != null &&
          (v['version'] != 1 ||
              v['salt'] is! String ||
              v['hash'] is! String ||
              v['attempts'] is! num ||
              v['until'] is! num ||
              v['iterations'] is! int)) {
        throw const FormatException();
      }
      _record = v;
      ready = true;
      failed = false;
      unlocked = !enabled;
    } catch (_) {
      ready = true;
      failed = true;
      unlocked = false;
    }
    notifyListeners();
  }

  void lock() {
    _lockGeneration++;
    if (enabled) {
      unlocked = false;
      notifyListeners();
    }
  }

  Future<Uint8List> _derive(String pin, List<int> salt, int rounds) async {
    final key = await Pbkdf2(
      macAlgorithm: Hmac.sha256(),
      iterations: rounds,
      bits: 256,
    ).deriveKey(secretKey: SecretKey(utf8.encode(pin)), nonce: salt);
    return Uint8List.fromList(await key.extractBytes());
  }

  Future<bool> verify(String pin) async {
    if (busy || failed || !ready || !enabled || remaining > Duration.zero) {
      return false;
    }
    busy = true;
    final generation = _lockGeneration;
    notifyListeners();
    try {
      final v = Map<String, dynamic>.from(_record!);
      if ((v['until'] as num) > 0) {
        v['attempts'] = 0;
        v['until'] = 0;
      }
      final hash = await _derive(
        pin,
        base64Decode(v['salt'] as String),
        v['iterations'] as int,
      );
      final expected = base64Decode(v['hash'] as String);
      var diff = hash.length ^ expected.length;
      for (var i = 0; i < hash.length && i < expected.length; i++) {
        diff |= hash[i] ^ expected[i];
      }
      final ok = RegExp(r'^\d{4}$').hasMatch(pin) && diff == 0;
      if (ok) {
        v['attempts'] = 0;
        v['until'] = 0;
      } else {
        v['attempts'] = (v['attempts'] as num).toInt() + 1;
        if (v['attempts'] >= 5) {
          v['until'] = now()
              .add(const Duration(minutes: 10))
              .millisecondsSinceEpoch;
        }
      }
      await vault.saveAppLock(v); // Commit BEFORE revealing success or retry.
      _record = v;
      if (ok && generation == _lockGeneration) unlocked = true;
      return ok;
    } catch (_) {
      failed = true;
      unlocked = false;
      return false;
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  Future<void> setPin(String pin) async {
    if (!RegExp(r'^\d{4}$').hasMatch(pin) ||
        failed ||
        !ready ||
        (enabled && !unlocked) ||
        remaining > Duration.zero) {
      throw FormatException('启动密码设置失败');
    }
    final salt = WireCrypto.random(32);
    final v = <String, dynamic>{
      'version': 1,
      'salt': base64Encode(salt),
      'hash': base64Encode(await _derive(pin, salt, iterations)),
      'iterations': iterations,
      'attempts': 0,
      'until': 0,
    };
    await vault.saveAppLock(v);
    _record = v;
    unlocked = true;
    notifyListeners();
  }

  Future<void> disable() async {
    if (!unlocked || failed || remaining > Duration.zero) {
      throw FormatException('启动密码设置失败');
    }
    await vault.removeAppLock();
    _record = null;
    notifyListeners();
  }
}
