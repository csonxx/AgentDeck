import 'dart:convert';
import 'dart:io';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:path_provider/path_provider.dart';
import 'crypto.dart';

// Device root and the cache wrapping key live in platform secure storage.
// Cache files contain only AES-GCM ciphertext; media is kept in memory.
class DeviceVault {
  final FlutterSecureStorage storage;
  final Directory? directory;
  DeviceVault({FlutterSecureStorage? storage, this.directory})
    : storage =
          storage ??
          const FlutterSecureStorage(
            aOptions: AndroidOptions(encryptedSharedPreferences: true),
            iOptions: IOSOptions(
              accessibility: KeychainAccessibility.first_unlock_this_device,
            ),
          );
  Future<Map<String, dynamic>?> loadAppLock() async {
    final value = await storage.read(key: 'agentdeck.app.lock');
    return value == null
        ? null
        : Map<String, dynamic>.from(jsonDecode(value) as Map);
  }

  Future<void> saveAppLock(Map<String, dynamic> value) =>
      storage.write(key: 'agentdeck.app.lock', value: jsonEncode(value));
  Future<void> removeAppLock() => storage.delete(key: 'agentdeck.app.lock');
  Future<String?> loadLanguage() => storage.read(key: 'agentdeck.language');
  Future<String?> loadDisplaySize() =>
      storage.read(key: 'agentdeck.display.size');
  Future<void> saveDisplaySize(String value) =>
      storage.write(key: 'agentdeck.display.size', value: value);
  Future<Map<String, dynamic>?> loadDeviceName() async {
    final value = await storage.read(key: 'agentdeck.device.name');
    return value == null ? null : Map<String, dynamic>.from(jsonDecode(value));
  }

  Future<void> saveDeviceName(Map<String, dynamic> value) =>
      storage.write(key: 'agentdeck.device.name', value: jsonEncode(value));
  Future<Map<String, dynamic>?> loadAppUpdate() async {
    final value = await storage.read(key: 'agentdeck.app.update');
    return value == null ? null : Map<String, dynamic>.from(jsonDecode(value));
  }

  Future<void> saveAppUpdate(Map<String, dynamic> value) =>
      storage.write(key: 'agentdeck.app.update', value: jsonEncode(value));
  Future<void> saveLanguage(String code) =>
      storage.write(key: 'agentdeck.language', value: code);

  Future<Map<String, dynamic>?> loadDevice() async {
    final v = await storage.read(key: 'agentdeck.device');
    return v == null ? null : Map<String, dynamic>.from(jsonDecode(v) as Map);
  }

  Future<void> saveDevice(Map<String, dynamic> value) =>
      storage.write(key: 'agentdeck.device', value: jsonEncode(value));
  Future<Map<String, dynamic>?> loadGatewayAccount() async {
    final v = await storage.read(key: 'agentdeck.gateway.account');
    return v == null ? null : Map<String, dynamic>.from(jsonDecode(v) as Map);
  }

  Future<void> saveGatewayAccount(Map<String, dynamic> value) =>
      storage.write(key: 'agentdeck.gateway.account', value: jsonEncode(value));
  Future<Map<String, dynamic>?> loadProjectLayout() async {
    final v = await storage.read(key: 'agentdeck.project.layout');
    return v == null ? null : Map<String, dynamic>.from(jsonDecode(v) as Map);
  }

  Future<void> saveProjectLayout(Map<String, dynamic> value) =>
      storage.write(key: 'agentdeck.project.layout', value: jsonEncode(value));

  Future<Map<String, dynamic>?> loadAgentVisibility() async {
    final value = await storage.read(key: 'agentdeck.agents.visibility');
    return value == null
        ? null
        : Map<String, dynamic>.from(jsonDecode(value) as Map);
  }

  Future<void> saveAgentVisibility(Map<String, dynamic> value) => storage.write(
    key: 'agentdeck.agents.visibility',
    value: jsonEncode(value),
  );

  Future<List<dynamic>> loadDownloads() async {
    final value = await storage.read(key: 'agentdeck.downloads');
    return value == null ? [] : jsonDecode(value) as List;
  }

  Future<void> saveDownloads(List<dynamic> value) =>
      storage.write(key: 'agentdeck.downloads', value: jsonEncode(value));

  Future<Map<String, dynamic>> loadGeneration() async {
    final v = await storage.read(key: 'agentdeck.generation');
    return v == null ? {} : Map<String, dynamic>.from(jsonDecode(v) as Map);
  }

  Future<void> saveGeneration(Map<String, dynamic> v) =>
      storage.write(key: 'agentdeck.generation', value: jsonEncode(v));
  Future<Directory> _directory() async =>
      directory ?? await getApplicationSupportDirectory();
  Future<File> _cache() async =>
      File('${(await _directory()).path}/transcripts.vault');
  Future<List<int>>? _keyFuture;
  Future<List<int>> _key() => _keyFuture ??= _loadKey().catchError((Object e) {
    _keyFuture = null;
    throw e;
  });
  Future<List<int>> _loadKey() async {
    var s = await storage.read(key: 'agentdeck.cache.key');
    if (s == null) {
      s = base64Encode(WireCrypto.random(32));
      await storage.write(key: 'agentdeck.cache.key', value: s);
    }
    return base64Decode(s);
  }

  Future<File> _outgoing(String id, String suffix) async {
    if (!RegExp(r'^[a-f0-9]{32}$').hasMatch(id)) {
      throw const FormatException('无效发件记录');
    }
    return File('${(await _directory()).path}/outbox/$id.$suffix.vault');
  }

  Future<void> _writeOutgoing(
    String id,
    String suffix,
    Map<String, dynamic> value,
  ) async {
    final file = await _outgoing(id, suffix);
    await file.parent.create(recursive: true);
    final sealed = await WireCrypto.seal(
      await _key(),
      'outbox/$id/$suffix',
      0,
      0,
      value,
    );
    final tmp = File('${file.path}.tmp');
    await tmp.writeAsString(jsonEncode(sealed), flush: true);
    await tmp.rename(file.path);
  }

  Future<Map<String, dynamic>> _readOutgoing(String id, String suffix) async {
    final file = await _outgoing(id, suffix);
    return WireCrypto.open(
      await _key(),
      'outbox/$id/$suffix',
      Map<String, dynamic>.from(jsonDecode(await file.readAsString())),
    );
  }

  Future<List<Map<String, dynamic>>> loadOutgoing() async {
    final dir = Directory('${(await _directory()).path}/outbox');
    if (!await dir.exists()) return [];
    final rows = <Map<String, dynamic>>[];
    await for (final file in dir.list()) {
      final match = RegExp(
        r'/([a-f0-9]{32})\.task\.vault$',
      ).firstMatch(file.path);
      if (file is File && match != null) {
        rows.add(await _readOutgoing(match[1]!, 'task'));
      }
    }
    return rows;
  }

  Future<void> saveOutgoing(String id, Map<String, dynamic> value) =>
      _writeOutgoing(id, 'task', value);
  Future<void> saveOutgoingFiles(String id, List<Map<String, dynamic>> files) =>
      _writeOutgoing(id, 'files', {'files': files});
  Future<List<Map<String, dynamic>>> loadOutgoingFiles(String id) async =>
      ((await _readOutgoing(id, 'files'))['files'] as List)
          .map((v) => Map<String, dynamic>.from(v as Map))
          .toList();
  Future<void> removeOutgoingFiles(String id) async {
    final file = await _outgoing(id, 'files');
    for (final path in [file.path, '${file.path}.tmp']) {
      final f = File(path);
      if (await f.exists()) await f.delete();
    }
  }

  Future<void> removeOutgoing(String id) async {
    final file = await _outgoing(id, 'task');
    for (final path in [file.path, '${file.path}.tmp']) {
      final f = File(path);
      if (await f.exists()) await f.delete();
    }
    await removeOutgoingFiles(id);
  }

  Future<Map<String, dynamic>> readCache() async {
    final f = await _cache();
    if (!await f.exists()) return {};
    return WireCrypto.open(
      await _key(),
      'cache',
      Map<String, dynamic>.from(jsonDecode(await f.readAsString()) as Map),
    );
  }

  Future<void> writeCache(Map<String, dynamic> value) async {
    final f = await _cache();
    await f.parent.create(recursive: true);
    final b = await WireCrypto.seal(await _key(), 'cache', 0, 0, value);
    final tmp = File('${f.path}.tmp');
    await tmp.writeAsString(jsonEncode(b), flush: true);
    await tmp.rename(f.path);
  }

  Future<void> clear({bool keepGateway = false}) async {
    // Remove caches before forgetting their key, including any interrupted write.
    final f = await _cache();
    for (final p in [f.path, '${f.path}.tmp']) {
      final file = File(p);
      if (await file.exists()) await file.delete();
    }
    final outbox = Directory('${(await _directory()).path}/outbox');
    if (await outbox.exists()) await outbox.delete(recursive: true);
    await storage.delete(key: 'agentdeck.device');
    await storage.delete(key: 'agentdeck.cache.key');
    _keyFuture = null;
    await storage.delete(key: 'agentdeck.project.layout');
    await storage.delete(key: 'agentdeck.generation');
    await storage.delete(key: 'agentdeck.downloads');
    if (!keepGateway) {
      await storage.delete(key: 'agentdeck.gateway.account');
    }
  }
}
