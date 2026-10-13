import 'package:agentdeck/main.dart';
import 'package:agentdeck/core/model.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'conversation_outline_test.dart' show DirectoryClient;

void main() {
  for (final role in ['user', 'assistant']) {
    testWidgets('$role message copies complete raw content and latest stream', (
      t,
    ) async {
      String? copied;
      t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async {
          if (call.method == 'Clipboard.setData') {
            copied = call.arguments['text'] as String;
          }
          return null;
        },
      );
      addTearDown(
        () => t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          null,
        ),
      );
      final c = DirectoryClient();
      addTearDown(c.dispose);
      final b = TranscriptBlock(
        key: 'copy-fixture',
        kind: 'message',
        role: role,
        text: '**Complete text**\n[link](https://example.com)\n`code`',
      );
      Future<void> mount() => t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: TranscriptTile(block: b, session: 's', client: c),
          ),
        ),
      );
      await mount();
      await t.tap(find.byKey(const ValueKey('copy-message-copy-fixture')));
      await t.pump();
      expect(copied, b.text);
      b.text += '\nstreamed ending';
      await mount();
      await t.tap(find.byKey(const ValueKey('copy-message-copy-fixture')));
      await t.pump();
      expect(copied, b.text);
      expect(t.takeException(), isNull);
    });
  }
}
