import 'package:agentdeck/language.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class LifecycleClient extends AgentClient {
  int paused = 0, resumed = 0;
  @override
  void suspend() {
    paused++;
  }

  @override
  Future<void> resume() async {
    resumed++;
  }
}

void main() {
  testWidgets(
    'App closes on background and requests fresh calibration on resume',
    (tester) async {
      final c = LifecycleClient();
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      await tester.pumpWidget(
        AgentDeckApp(
          client: c,
          languages: LanguageController(
            c.vault,
            initial: AppLanguage.simplifiedChinese,
          ),
        ),
      );
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      expect(c.paused, 0);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      expect(c.paused, 1);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      expect(c.resumed, 1);
      await tester.pumpWidget(const SizedBox());
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      expect(c.paused, 1);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      c.dispose();
    },
  );
}
