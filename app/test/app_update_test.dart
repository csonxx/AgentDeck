import 'dart:convert';
import 'package:agentdeck/core/app_update.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/app_update_page.dart';
import 'package:flutter/material.dart';
import 'package:agentdeck/language.dart';
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
  test('editions retain identity and reject unknown flavors', () {
    final voice = fixtureRelease().toJson();
    expect(AndroidRelease.fromJson(voice).edition, 'voice');
    expect(
      AndroidRelease.fromJson({...voice, 'edition': 'lite'}).edition,
      'lite',
    );
    expect(
      () => AndroidRelease.fromJson({...voice, 'edition': 'other'}),
      throwsFormatException,
    );
  });
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
    'lite ignores voice releases and cancels old edition downloads',
    () async {
      final vault = UpdateVault()
        ..update = {
          'automatic': true,
          'download_id': 42,
          'release': fixtureRelease().toJson(),
          'latest': fixtureRelease().toJson(),
        };
      final updater = AppUpdater(
        vault,
        gateway: () async => 'https://gateway.example/agentdeck',
        edition: 'lite',
        supported: true,
        fetchRelease: (_) async => fixtureRelease(),
      );
      addTearDown(updater.dispose);
      await updater.initialize();
      expect(updater.latest, isNull);
      expect(updater.available, isFalse);
      expect(updater.downloadId, isNull);
      expect(cancels, 1);
      expect(vault.update!['latest'], isNull);
      expect(starts, 0);
    },
  );
  test(
    'explicit same-version edition switch persists and resumes download',
    () async {
      installed = 7051;
      final vault = UpdateVault();
      late AppUpdater u;
      AppUpdater make(String edition) {
        u = AppUpdater(
          vault,
          edition: edition,
          supported: true,
          gateway: () async => 'https://gateway.example',
          fetchRelease: (_) async => AndroidRelease.fromJson({
            ...fixtureRelease().toJson(),
            'edition': u.selectedEdition,
          }),
        );
        return u;
      }

      make('lite');
      await u.initialize();
      expect(u.selectedEdition, 'lite');
      expect(u.available, false);
      await u.setEdition('voice');
      expect(u.available, true);
      expect(vault.update!['selected_edition'], 'voice');
      await u.download();
      expect(starts, 1);
      expect(u.canSelectEdition, false);
      await u.setEdition('lite');
      expect(u.selectedEdition, 'voice');
      u.dispose();
      make('lite');
      await u.initialize();
      expect(u.downloadId, 42);
      expect(cancels, 0);
      state = 'complete';
      await u.refreshDownload();
      await u.install();
      expect(u.error, isEmpty);
      u.dispose();
      // The same installed target clears its old download, preserving other settings.
      make('voice');
      await u.initialize();
      expect(u.downloadId, isNull);
      expect(u.available, false);
      expect(cancels, 1);
      u.dispose();
    },
  );
  test('edition switch rejects downgrade and wrong gateway edition', () async {
    installed = 7052;
    final u = AppUpdater(
      UpdateVault(),
      edition: 'lite',
      supported: true,
      gateway: () async => 'https://gateway.example',
      fetchRelease: (_) async => fixtureRelease(),
    );
    addTearDown(u.dispose);
    await u.initialize();
    expect(u.latest, isNull);
    await u.setEdition('voice');
    expect(u.latest!.edition, 'voice');
    expect(u.available, false);
    await u.download();
    expect(starts, 0);
  });
  testWidgets(
    'update picker shows selected edition size and same-version download',
    (tester) async {
      installed = 7051;
      late AppUpdater u;
      u = AppUpdater(
        UpdateVault(),
        edition: 'lite',
        supported: true,
        gateway: () async => 'https://gateway.example',
        fetchRelease: (_) async => AndroidRelease.fromJson({
          ...fixtureRelease().toJson(),
          'edition': u.selectedEdition,
          'bytes': u.selectedEdition == 'lite' ? 33000000 : 273000000,
        }),
      );
      addTearDown(u.dispose);
      await u.initialize();
      await tester.pumpWidget(
        LanguageScope(
          controller: LanguageController(
            UpdateVault(),
            initial: AppLanguage.simplifiedChinese,
          ),
          child: MaterialApp(
            locale: AppLanguage.simplifiedChinese.locale,
            localizationsDelegates: UiStrings.delegates,
            supportedLocales: UiStrings.supportedLocales,
            home: AppUpdatePage(updater: u),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('33.0 MB'), findsOneWidget);
      expect(find.text('下载更新'), findsNothing);
      await tester.tap(find.text('带语音版'));
      await tester.pumpAndSettle();
      expect(u.selectedEdition, 'voice');
      expect(find.text('273.0 MB'), findsOneWidget);
      await tester.scrollUntilVisible(
        find.text('下载更新'),
        200,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.ensureVisible(find.text('下载更新'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('下载更新'));
      for (var i = 0; i < 5; i++) {
        await tester.pump(const Duration(milliseconds: 50));
      }
      expect(starts, 1);
      expect(u.canSelectEdition, false);
      u.background();
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(seconds: 1));
    },
  );
  test('lite discovers only its own edition update', () async {
    final updater = AppUpdater(
      UpdateVault(),
      gateway: () async => 'https://gateway.example',
      edition: 'lite',
      supported: true,
      fetchRelease: (_) async => AndroidRelease.fromJson({
        ...fixtureRelease().toJson(),
        'edition': 'lite',
      }),
    );
    addTearDown(updater.dispose);
    await updater.initialize();
    expect(updater.available, isTrue);
    expect(updater.latest!.edition, 'lite');
    await updater.download();
    expect(starts, 1);
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
