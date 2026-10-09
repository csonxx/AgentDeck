import 'dart:convert';
import 'dart:io';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:path_provider/path_provider.dart';
import 'crypto.dart';

// Device root and the cache wrapping key live in platform secure storage.
// Cache files contain only AES-GCM ciphertext; media is kept in memory.
class DeviceVault {
  final FlutterSecureStorage storage;
  DeviceVault({FlutterSecureStorage? storage})
    : storage =
          storage ??
          const FlutterSecureStorage(
            aOptions: AndroidOptions(encryptedSharedPreferences: true),
            iOptions: IOSOptions(
              accessibility: KeychainAccessibility.first_unlock_this_device,
            ),
          );
  Future<String?> loadLanguage() => storage.read(key: 'agentdeck.language');
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

  Future<Map<String, dynamic>> loadGeneration() async {
    final v = await storage.read(key: 'agentdeck.generation');
    return v == null ? {} : Map<String, dynamic>.from(jsonDecode(v) as Map);
  }

  Future<void> saveGeneration(Map<String, dynamic> v) =>
      storage.write(key: 'agentdeck.generation', value: jsonEncode(v));
  Future<File> _cache() async => File(
    '${(await getApplicationSupportDirectory()).path}/transcripts.vault',
  );
  Future<List<int>> _key() async {
    var s = await storage.read(key: 'agentdeck.cache.key');
    if (s == null) {
      s = base64Encode(WireCrypto.random(32));
      await storage.write(key: 'agentdeck.cache.key', value: s);
    }
    return base64Decode(s);
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
    await storage.delete(key: 'agentdeck.device');
    await storage.delete(key: 'agentdeck.cache.key');
    await storage.delete(key: 'agentdeck.project.layout');
    await storage.delete(key: 'agentdeck.generation');
    if (!keepGateway) {
      await storage.delete(key: 'agentdeck.gateway.account');
    }
  }
}
