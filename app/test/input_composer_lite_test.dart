import 'package:agentdeck/core/edition.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'input_composer_test.dart' show InputClient, FakeLocalInputs, mount;

class LiteInputs extends FakeLocalInputs {
  int prepares = 0;
  @override
  Future<void> prepareLocalVoice() async {
    prepares++;
  }
}

void main() {
  testWidgets(
    'lite has no microphone and never prepares offline recognition',
    (t) async {
      final client = InputClient(),
          inputs = LiteInputs(),
          controller = TextEditingController();
      addTearDown(client.dispose);
      addTearDown(controller.dispose);
      await mount(t, client, inputs, controller);
      expect(find.byTooltip('语音输入'), findsNothing);
      expect(find.byKey(const ValueKey('voice-hold')), findsNothing);
      expect(find.byTooltip('添加图片或文件'), findsOneWidget);
      await t.enterText(find.byType(TextField), 'Lite text input');
      expect(controller.text, 'Lite text input');
      expect(inputs.localStarts, 0);
      expect(inputs.prepares, 0);
      expect(t.takeException(), isNull);
    },
    skip: hasVoiceInput,
  );
}
