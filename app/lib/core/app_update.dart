import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'vault.dart';

const defaultUpdateGateway = String.fromEnvironment(
  'AGENTDECK_GATEWAY_URL',
  defaultValue: 'https://web-test-omnivibe.ttyuyin.com/agentdeck',
);

class AndroidRelease {
  final String version, sha256, url, notes, abi;
  final int code, bytes, minSdk;
  const AndroidRelease({
    required this.version,
    required this.code,
    required this.bytes,
    required this.sha256,
    required this.url,
    this.notes = '',
    this.abi = 'arm64-v8a',
    this.minSdk = 24,
  });
  factory AndroidRelease.fromJson(Map<String, dynamic> v) {
    final uri = Uri.parse(v['url'] as String);
    final code = (v['version_code'] as num).toInt();
    final size = (v['bytes'] as num).toInt();
    final minSdk = (v['min_sdk'] as num).toInt();
    final version = v['version'] as String;
    final hash = v['sha256'] as String;
    final notes = v['notes'] as String? ?? '';
    if (v['package'] != 'dev.agentdeck.agentdeck' ||
        v['abi'] != 'arm64-v8a' ||
        uri.scheme != 'https' ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty ||
        uri.hasQuery ||
        uri.hasFragment ||
        !RegExp(r'^[a-f0-9]{64}$').hasMatch(hash) ||
        !RegExp(r'^\d+\.\d+\.\d+$').hasMatch(version) ||
        code < 1 ||
        size < 1 ||
        size > 2 << 30 ||
        minSdk < 21 ||
        notes.length > 12000) {
      throw const FormatException('更新信息无效');
    }
    return AndroidRelease(
      version: version,
      code: code,
      bytes: size,
      sha256: hash,
      url: uri.toString(),
      notes: notes,
      minSdk: minSdk,
    );
  }
  Map<String, dynamic> toJson() => {
    'version': version,
    'version_code': code,
    'package': 'dev.agentdeck.agentdeck',
    'abi': abi,
    'min_sdk': minSdk,
    'bytes': bytes,
    'sha256': sha256,
    'url': url,
    'notes': notes,
  };
}

class AppUpdater extends ChangeNotifier {
  static const channel = MethodChannel('dev.agentdeck/update');
  final DeviceVault vault;
  final Future<String> Function() gateway;
  final HttpClient Function() createHttp;
  final Future<AndroidRelease?> Function(String)? fetchRelease;
  final bool supported;
  AppUpdater(
    this.vault, {
    required this.gateway,
    HttpClient Function()? createHttp,
    this.fetchRelease,
    bool? supported,
  }) : createHttp = createHttp ?? HttpClient.new,
       supported = supported ?? Platform.isAndroid;
  bool automatic = true, checking = false, installing = false, starting = false;
  String installedVersion = '', error = '', downloadState = '', gatewayUrl = '';
  int installedCode = 0, sdk = 0, received = 0, total = 0;
  List<String> abis = [];
  AndroidRelease? latest, downloading;
  int? downloadId;
  DateTime? checkedAt, _attemptedAt;
  Timer? _poll;
  bool _disposed = false, _background = false, _querying = false;
  Future<void>? _initializing;
  bool get available => latest != null && latest!.code > installedCode;
  bool get ready => downloadId != null && downloadState == 'complete';
  bool get active =>
      ['pending', 'downloading', 'paused'].contains(downloadState);
  void _changed() {
    if (!_disposed) notifyListeners();
  }

  Future<void> initialize() => _initializing ??= _initialize();
  Future<void> _initialize() async {
    if (!supported) return;
    try {
      final info = Map<String, dynamic>.from(
        await channel.invokeMethod('info'),
      );
      installedVersion = info['version'] as String;
      installedCode = (info['version_code'] as num).toInt();
      sdk = (info['sdk'] as num).toInt();
      abis = List<String>.from(info['abis'] as List);
      final saved = await vault.loadAppUpdate();
      if (saved != null) {
        automatic = saved['automatic'] != false;
        gatewayUrl = saved['gateway'] as String? ?? '';
        if (saved['latest'] is Map) {
          latest = AndroidRelease.fromJson(
            Map<String, dynamic>.from(saved['latest']),
          );
        }
        if (saved['checked_at'] is int) {
          checkedAt = DateTime.fromMillisecondsSinceEpoch(
            saved['checked_at'] as int,
          );
        }
        if (saved['download_id'] is int && saved['release'] is Map) {
          downloadId = saved['download_id'] as int;
          downloading = AndroidRelease.fromJson(
            Map<String, dynamic>.from(saved['release']),
          );
          if (downloading!.code <= installedCode) {
            await channel.invokeMethod('cancel', {'id': downloadId});
            downloadId = null;
            downloading = null;
            await _save();
          } else {
            await refreshDownload();
          }
        }
      }
      await check();
    } catch (_) {
      error = '无法读取更新状态，请重试';
    }
    _changed();
  }

  Future<void> _save() => vault.saveAppUpdate({
    'automatic': automatic,
    'gateway': gatewayUrl,
    'checked_at': checkedAt?.millisecondsSinceEpoch,
    'latest': latest?.toJson(),
    'download_id': downloadId,
    'release': downloading?.toJson(),
  });
  Future<void> setAutomatic(bool value) async {
    automatic = value;
    try {
      await _save();
      if (value) await check(force: true);
    } catch (_) {
      error = '无法保存更新设置';
    }
    _changed();
  }

  Future<AndroidRelease?> _fetch(String origin) async {
    final base = Uri.parse(origin);
    if (base.scheme != 'https' ||
        base.host.isEmpty ||
        base.userInfo.isNotEmpty ||
        base.hasQuery ||
        base.hasFragment) {
      throw const FormatException('更新信息无效');
    }
    final http = createHttp()..connectionTimeout = const Duration(seconds: 15);
    try {
      final req = await http.getUrl(
        Uri.parse(
          '${origin.replaceFirst(RegExp(r'/$'), '')}/v2/app/releases/android?abi=arm64-v8a',
        ),
      );
      req.followRedirects = false;
      final res = await req.close().timeout(const Duration(seconds: 20));
      if (res.statusCode == 404) throw const FormatException('网关尚未启用应用更新');
      if (res.statusCode == 204) {
        await res.drain<void>();
        return null;
      }
      if (res.statusCode != 200) throw const HttpException('暂时无法检查更新，请稍后重试');
      final raw = <int>[];
      await for (final part in res.timeout(const Duration(seconds: 15))) {
        raw.addAll(part);
        if (raw.length > 16 << 10) throw const FormatException('更新信息无效');
      }
      return AndroidRelease.fromJson(
        Map<String, dynamic>.from(jsonDecode(utf8.decode(raw))),
      );
    } finally {
      http.close(force: true);
    }
  }

  Future<void> check({bool force = false}) async {
    if (!supported || checking || _disposed || !force && !automatic) return;
    final origin = await gateway();
    final now = DateTime.now();
    if (!force &&
        origin == gatewayUrl &&
        (checkedAt != null &&
                now.difference(checkedAt!) < const Duration(hours: 6) ||
            _attemptedAt != null &&
                now.difference(_attemptedAt!) < const Duration(minutes: 10))) {
      return;
    }
    checking = true;
    error = '';
    _attemptedAt = now;
    _changed();
    try {
      final release = await (fetchRelease?.call(origin) ?? _fetch(origin));
      if (release != null &&
          (!abis.contains(release.abi) || sdk < release.minSdk)) {
        throw const FormatException('新版本暂不支持此设备');
      }
      latest = release;
      gatewayUrl = origin;
      checkedAt = now;
      await _save();
    } catch (e) {
      error =
          e is FormatException &&
              ['网关尚未启用应用更新', '新版本暂不支持此设备'].contains(e.message)
          ? e.message
          : '暂时无法检查更新，请稍后重试';
    } finally {
      checking = false;
      _changed();
    }
  }

  Future<void> download() async {
    if (!available || starting || active || ready) return;
    starting = true;
    error = '';
    _changed();
    try {
      if (downloadId != null) {
        await channel.invokeMethod('cancel', {'id': downloadId});
      }
      downloading = latest;
      downloadId = await channel.invokeMethod<int>(
        'start',
        downloading!.toJson(),
      );
      if (downloadId == null) throw const FormatException('无法开始更新下载，请重试');
      downloadState = 'pending';
      received = 0;
      total = downloading!.bytes;
      try {
        await _save();
      } catch (_) {
        await channel.invokeMethod('cancel', {'id': downloadId});
        downloadId = null;
        downloading = null;
        rethrow;
      }
      await refreshDownload();
    } catch (_) {
      error = '无法开始更新下载，请重试';
    } finally {
      starting = false;
      _changed();
    }
  }

  Future<void> refreshDownload() async {
    if (downloadId == null || _querying || _disposed) return;
    _querying = true;
    try {
      final row = Map<String, dynamic>.from(
        await channel.invokeMethod('query', {'id': downloadId}),
      );
      downloadState = row['status'] as String;
      received = (row['received'] as num).toInt();
      total = downloading?.bytes ?? 0;
      if (downloadState == 'failed' || downloadState == 'missing') {
        error = '更新下载失败，请重新下载';
      }
      _poll?.cancel();
      if (active && !_background) {
        _poll = Timer.periodic(
          const Duration(seconds: 2),
          (_) => unawaited(refreshDownload()),
        );
      }
    } catch (_) {
      error = '无法读取下载进度';
    } finally {
      _querying = false;
      _changed();
    }
  }

  Future<void> cancelDownload() async {
    if (downloadId == null || installing) return;
    try {
      await channel.invokeMethod('cancel', {'id': downloadId});
      _poll?.cancel();
      downloadId = null;
      downloading = null;
      downloadState = '';
      received = 0;
      total = 0;
      await _save();
      error = '';
    } catch (_) {
      error = '无法取消更新下载';
    }
    _changed();
  }

  Future<void> install() async {
    if (!ready || installing || downloading == null) return;
    installing = true;
    error = '';
    _changed();
    try {
      final result = await channel.invokeMethod<String>('install', {
        ...downloading!.toJson(),
        'id': downloadId,
      });
      if (result == 'permission') error = '请允许安装此来源的应用，返回后点击安装';
    } catch (_) {
      error = '更新包校验或安装失败，请重新下载';
    } finally {
      installing = false;
      _changed();
    }
  }

  void background() {
    _background = true;
    _poll?.cancel();
  }

  Future<void> foreground() async {
    _background = false;
    await initialize();
    await refreshDownload();
    await check();
  }

  @override
  void dispose() {
    _disposed = true;
    _poll?.cancel();
    super.dispose();
  }
}
