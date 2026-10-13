import 'package:agentdeck/core/app_lock.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:agentdeck/app_lock.dart';
import 'package:agentdeck/language.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class LockVault extends DeviceVault {
  Map<String, dynamic>? value;
  bool fail = false;
  @override
  Future<Map<String, dynamic>?> loadAppLock() async =>
      value == null ? null : Map.of(value!);
  @override
  Future<void> saveAppLock(Map<String, dynamic> v) async {
    if (fail) throw StateError('unavailable');
    value = Map.of(v);
  }

  @override
  Future<void> removeAppLock() async {
    value = null;
  }
}

void main() {
  test(
    'salted verifier, restart persists five-attempt fixed ten-minute cooldown',
    () async {
      final v = LockVault();
      var now = DateTime.utc(2026, 10, 10, 12);
      AppLockController make() =>
          AppLockController(v, now: () => now, iterations: 10);
      var c = make();
      await c.load();
      await c.setPin('8361');
      expect(v.value.toString(), isNot(contains('8361')));
      c.lock();
      for (var i = 0; i < 4; i++) {
        expect(await c.verify('0000'), false);
        expect(c.remaining, Duration.zero);
      }
      expect(await c.verify('0000'), false);
      expect(c.remaining, const Duration(minutes: 10));
      final deadline = v.value!['until'];
      c.dispose();
      c = make();
      await c.load();
      expect(c.locked, true);
      expect(await c.verify('8361'), false);
      expect(v.value!['until'], deadline);
      now = now.add(const Duration(minutes: 9));
      expect(await c.verify('0000'), false);
      expect(c.remaining, const Duration(minutes: 1));
      expect(v.value!['until'], deadline);
      now = now.add(const Duration(minutes: 1));
      expect(await c.verify('0000'), false);
      expect(c.attempts, 1);
      expect(c.remaining, Duration.zero);
      for (var i = 0; i < 4; i++) {
        expect(await c.verify('0000'), false);
      }
      expect(c.remaining, const Duration(minutes: 10));
      now = now.add(const Duration(minutes: 10));
      expect(await c.verify('8361'), true);
      expect(c.attempts, 0);
      expect(c.locked, false);
      c.dispose();
    },
  );
  test(
    'invalid lengths, failed persistence, and locked password changes fail closed',
    () async {
      final v = LockVault();
      final c = AppLockController(v, iterations: 10);
      await c.load();
      await expectLater(c.setPin('12'), throwsFormatException);
      await c.setPin('4829');
      c.lock();
      await expectLater(c.setPin('1111'), throwsFormatException);
      await expectLater(c.disable(), throwsFormatException);
      v.fail = true;
      expect(await c.verify('4829'), false);
      expect(c.locked, true);
      expect(c.failed, true);
      c.dispose();
    },
  );
  testWidgets(
    'lock covers existing routes, PIN keypad unlocks and relocks on background',
    (tester) async {
      final v = LockVault();
      final c = AppLockController(v, iterations: 10);
      await c.load();
      await c.setPin('4829');
      c.lock();
      final languages = LanguageController(
        v,
        initial: AppLanguage.simplifiedChinese,
      );
      await tester.pumpWidget(
        LanguageScope(
          controller: languages,
          child: MaterialApp(
            locale: AppLanguage.simplifiedChinese.locale,
            supportedLocales: UiStrings.supportedLocales,
            localizationsDelegates: UiStrings.delegates,
            home: const Text('sensitive history'),
            builder: (context, child) =>
                AppLockGate(controller: c, child: child!),
          ),
        ),
      );
      await tester.pump();
      expect(find.text('sensitive history'), findsNothing);
      expect(find.text('输入启动密码'), findsOneWidget);
      for (final d in '4829'.split('')) {
        await tester.tap(find.byKey(ValueKey('pin-$d')));
        await tester.pump();
      }
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 50)),
      );
      await tester.pump();
      expect(find.text('sensitive history'), findsOneWidget);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      await tester.pump();
      expect(find.text('sensitive history'), findsNothing);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      await tester.pump();
      expect(find.text('输入启动密码'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      c.dispose();
      languages.dispose();
    },
  );
}
