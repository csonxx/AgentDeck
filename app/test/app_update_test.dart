import 'dart:convert';
import 'package:agentdeck/core/app_update.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:agentdeck/core/client.dart';
import 'package:flutter_test/flutter_test.dart';

class UpdateVault extends DeviceVault {
  Map<String, dynamic>? update, name;
  @override
  Future<Map<String, dynamic>?> loadAppUpdate() async =>
      update == null ? null : jsonDecode(jsonEncode(update));
  @override
  Future<void> saveAppUpdate(Map<String, dynamic> value) async {
    update = jsonDecode(jsonEncode(value));
  }

  @override
  Future<Map<String, dynamic>?> loadDeviceName() async => name;
  @override
  Future<void> saveDeviceName(Map<String, dynamic> value) async {
    name = Map.of(value);
  }
}

AndroidRelease fixtureRelease() => AndroidRelease(
  version: '0.4.34',
  code: 7051,
  bytes: 123456,
  sha256: List.filled(64, 'a').join(),
  url: 'https://cdn.example/update.apk',
  notes: 'Fixture notes',
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  var installed = 7050,
      received = 12345,
      queries = 0,
      starts = 0,
      cancels = 0,
      fetches = 0;
  var state = 'downloading';
  String installResult = 'installer';
  setUp(() {
    installed = 7050;
    received = 12345;
    queries = 0;
    starts = 0;
    cancels = 0;
    fetches = 0;
    state = 'downloading';
    installResult = 'installer';
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(AppUpdater.channel, (call) async {
          switch (call.method) {
            case 'info':
              return {
                'version': '0.4.33',
                'version_code': installed,
                'sdk': 30,
                'abis': ['arm64-v8a'],
              };
            case 'start':
              starts++;
              return 42;
            case 'query':
              queries++;
              return {'status': state, 'received': received, 'total': 123456};
            case 'cancel':
              cancels++;
              return null;
            case 'install':
              expect(call.arguments['sha256'], fixtureRelease().sha256);
              expect(call.arguments['version_code'], 7051);
              return installResult;
          }
          return null;
        });
  });
  test(
    'release rejects insecure URLs and wrong identity; native ABI code is used',
    () {
      final v = fixtureRelease().toJson();
      expect(AndroidRelease.fromJson(v).code, 7051);
      for (final patch in [
        {'url': 'http://cdn.example/a.apk'},
        {'url': 'https://cdn.example/a.apk?token=secret'},
        {'package': 'other.app'},
        {'sha256': 'wrong'},
        {'bytes': 0},
      ]) {
        expect(
          () => AndroidRelease.fromJson({...v, ...patch}),
          throwsA(isA<FormatException>()),
        );
      }
    },
  );
  test(
    'system download restores after process restart and installed update clears it',
    () async {
      final vault = UpdateVault();
      AppUpdater make() => AppUpdater(
        vault,
        supported: true,
        gateway: () async => 'https://gateway.example/agentdeck',
        fetchRelease: (_) async {
          fetches++;
          return fixtureRelease();
        },
      );
      var u = make();
      await u.initialize();
      expect(u.available, true);
      expect(u.installedCode, 7050);
      await u.download();
      expect(starts, 1);
      expect(u.received, 12345);
      expect(vault.update?['download_id'], 42);
      u.background();
      u.dispose();
      state = 'complete';
      received = 123456;
      u = make();
      await u.initialize();
      expect(u.ready, true);
      expect(starts, 1);
      expect(fetches, 1);
      expect(queries, greaterThan(1));
      installResult = 'permission';
      await u.install();
      expect(u.error, contains('允许安装'));
      expect(u.ready, true);
      installResult = 'installer';
      await u.install();
      expect(u.error, isEmpty);
      u.dispose();
      installed = 7051;
      u = make();
      await u.initialize();
      expect(u.downloadId, isNull);
      expect(u.available, false);
      expect(cancels, 1);
      u.dispose();
    },
  );
  test('automatic disabled stays disabled; manual check still works', () async {
    final v = UpdateVault()..update = {'automatic': false};
    final u = AppUpdater(
      v,
      supported: true,
      gateway: () async => 'https://gateway.example',
      fetchRelease: (_) async {
        fetches++;
        return fixtureRelease();
      },
    );
    addTearDown(u.dispose);
    await u.initialize();
    expect(fetches, 0);
    await u.check(force: true);
    expect(fetches, 1);
    await u.setAutomatic(false);
    expect(v.update?['automatic'], false);
  });
  test(
    'failed checks do not claim latest or overwrite a known release',
    () async {
      final u = AppUpdater(
        UpdateVault(),
        supported: true,
        gateway: () async => 'https://gateway.example',
        fetchRelease: (_) async => throw const FormatException('网关尚未启用应用更新'),
      );
      addTearDown(u.dispose);
      await u.initialize();
      expect(u.checkedAt, isNull);
      expect(u.error, contains('尚未启用'));
      expect(u.available, false);
    },
  );
  test(
    'offline device name persists for encrypted sync without changing pairing',
    () async {
      final v = UpdateVault(), c = AgentClient(vault: UpdateVault());
      addTearDown(c.dispose);
      final own = AgentClient(vault: v);
      addTearDown(own.dispose);
      await own.renameDevice(' 我的 OnePlus 7 ');
      expect(own.deviceName, '我的 OnePlus 7');
      expect(own.deviceNamePending, true);
      expect(v.name?['name'], '我的 OnePlus 7');
      expect(
        () => own.renameDevice('bad\nname'),
        throwsA(isA<FormatException>()),
      );
    },
  );
}
