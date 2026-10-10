// Opt-in acceptance against a deployed gateway, using an owner-enrolled,
// temporary S-LOCAL. Credentials and synthetic inputs stay in a private temp dir.
import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:flutter_test/flutter_test.dart';
import 'package:agentdeck/core/client.dart';
import 'gateway_e2e_test.dart' show GatewayVault, until;

void main() {
  final dir = Platform.environment['AGENTDECK_DEPLOYED_TEST_DIR'];
  test(
    'deployed HTTPS prefix relays native encrypted sessions and images',
    () async {
      final c = AgentClient(vault: GatewayVault());
      addTearDown(c.dispose);
      await c.pair(await File('$dir/pair.json').readAsString());
      expect(c.connected, isTrue);
      await until(() => c.sessions.any((s) => s.id == 'deployment-acceptance'));
      await c.watch('deployment-acceptance');
      await until(
        () =>
            c.transcripts['deployment-acceptance']?.blocks.any(
              (b) => b.text.contains('Deployment acceptance'),
            ) ??
            false,
      );
      final image = c.transcripts['deployment-acceptance']!.blocks.firstWhere(
        (b) => b.kind == 'image',
      );
      final bytes = await c.image(image.media);
      final codec = await ui.instantiateImageCodec(bytes);
      final frame = await codec.getNextFrame();
      expect(frame.image.width, 1);
      frame.image.dispose();
      codec.dispose();
      final linked = await c.attachment('deployment-acceptance', 'notes.txt');
      expect(utf8.decode(linked.bytes), 'Deployment attachment');
      await File('$dir/source/live.jsonl').writeAsString(
        '${jsonEncode({
          'method': 'item/agentMessage/delta',
          'params': {'threadId': 'deployment-acceptance', 'itemId': 'live', 'delta': 'Live public gateway update'},
        })}\n',
        mode: FileMode.append,
      );
      await until(
        () =>
            c.transcripts['deployment-acceptance']?.blocks.any(
              (b) => b.text.contains('Live public gateway update'),
            ) ??
            false,
      );
      expect(c.projects, isNotEmpty);
      final session = await c.createSession('codex', c.projects.first.id);
      await c.sendPrompt(session.id, 'deployment acceptance');
      await until(
        () =>
            c.transcripts[session.id]?.blocks.any(
              (b) => b.text == 'RUNTIME_OK',
            ) ??
            false,
      );
      c.suspend();
      await c.resume();
      expect(c.connected, isTrue);
      await c.send({'method': 'ping'});
    },
    skip: dir == null,
    timeout: const Timeout(Duration(minutes: 2)),
  );
}
