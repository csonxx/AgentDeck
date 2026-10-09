// Runs against direct Go HTTPS and a separate TLS-terminating proxy. Test roots use a memory vault;
// production uses FlutterSecureStorage, covered separately by native builds.
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'package:agentdeck/core/input.dart';
import 'dart:ui' as ui;
import 'package:flutter_test/flutter_test.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/clock.dart';
import 'package:agentdeck/core/vault.dart';

class MemoryVault extends DeviceVault {
  Map<String, dynamic>? value;
  Map<String, dynamic> cache = {};
  Map<String, dynamic>? layout;
  @override
  Future<Map<String, dynamic>?> loadProjectLayout() async => layout;
  @override
  Future<void> saveProjectLayout(Map<String, dynamic> value) async {
    layout = value;
  }

  @override
  Future<Map<String, dynamic>?> loadDevice() async => value;
  @override
  Future<void> saveDevice(Map<String, dynamic> v) async {
    value = v;
  }

  @override
  Future<Map<String, dynamic>> readCache() async => cache;
  @override
  Future<void> writeCache(Map<String, dynamic> v) async {
    cache = v;
  }

  @override
  Future<void> clear({bool keepGateway = false}) async {
    value = null;
    cache = {};
    layout = null;
  }
}

Future<void> until(bool Function() f) async {
  final end = DateTime.now().add(const Duration(seconds: 15));
  while (!f()) {
    if (DateTime.now().isAfter(end)) fail('Condition timed out');
    await Future<void>.delayed(const Duration(milliseconds: 50));
  }
}

void main() {
  final dir = Platform.environment['AGENTDECK_E2E_DIR'];
  for (final proxy in [false, true]) {
    test(
      'Flutter ${proxy ? 'TLS proxy → HTTP backend' : 'direct HTTPS'} pairs, streams images, rotates and reconnects',
      () async {
        final vault = MemoryVault();
        final pairDir = proxy ? '$dir/proxy' : dir;
        final roots = proxy
            ? (SecurityContext(withTrustedRoots: false)
                ..setTrustedCertificates('$pairDir/ca.pem'))
            : null;
        var wallOffset = proxy ? -86400000 : 86400000;
        final clock = ProtocolClock(
          wallMillis: () => DateTime.now().millisecondsSinceEpoch + wallOffset,
        );
        final c = AgentClient(vault: vault, trustedRoots: roots, clock: clock);
        addTearDown(c.dispose);
        final bundle = await File('$pairDir/pair.json').readAsString();
        if (proxy) {
          final untrusted = AgentClient(vault: MemoryVault());
          addTearDown(untrusted.dispose);
          await expectLater(
            untrusted.pair(bundle),
            throwsA(isA<HandshakeException>()),
          );
          final wrongHost = Map<String, dynamic>.from(
            jsonDecode(bundle) as Map,
          );
          wrongHost['url'] = (wrongHost['url'] as String).replaceFirst(
            'localhost',
            '127.0.0.1',
          );
          final mismatch = AgentClient(
            vault: MemoryVault(),
            trustedRoots: roots,
          );
          addTearDown(mismatch.dispose);
          await expectLater(
            mismatch.pair(jsonEncode(wrongHost)),
            throwsA(isA<HandshakeException>()),
          );
        }
        try {
          await c.pair(bundle);
        } catch (_) {
          // Only synthetic clock diagnostics: no bundle, keys or session data.
          // Useful when a busy host exceeds the fixture's one-second epoch.
          printOnFailure(
            jsonEncode({
              'fixture': proxy ? 'proxy' : 'direct',
              'clock_calibrated': clock.calibrated,
              'clock_delta_ms': clock.calibrated
                  ? clock.millis - DateTime.now().millisecondsSinceEpoch
                  : null,
            }),
          );
          rethrow;
        }
        expect(c.connected, isTrue);
        expect(clock.calibrated, isTrue);
        expect(
          (clock.millis - DateTime.now().millisecondsSinceEpoch).abs(),
          lessThan(10000),
        );
        await until(() => c.sessions.any((s) => s.id == 'e2e-session'));
        await until(
          () =>
              c.sessions.any((s) => s.id == 'bridge-codex') &&
              c.sessions.any((s) => s.id == 'bridge-claude'),
        );
        for (final id in ['bridge-codex', 'bridge-claude']) {
          await c.watch(id);
          await until(() => c.transcripts[id]?.blocks.isNotEmpty ?? false);
          expect(
            c.transcripts[id]!.blocks.where((b) => b.kind == 'message').length,
            1,
          );
          expect(
            c.transcripts[id]!.blocks.single.text,
            contains('bridge works'),
          );
        }
        expect(c.projects, isNotEmpty);
        for (final agent in ['codex', 'claude']) {
          final session = await c.createSession(agent, c.projects.first.id);
          expect(session.managed, isTrue);
          await c.sendPrompt(session.id, 'runtime wire test');
          await until(
            () =>
                c.transcripts[session.id]?.blocks.any(
                  (b) => b.text == 'RUNTIME_OK',
                ) ??
                false,
          );
          await until(
            () => c.sessions.any(
              (s) => s.id == session.id && s.status == 'completed',
            ),
          );
          final catalog = await c.inputCatalog(agent, session.id);
          expect(catalog.models.single.id, 'fixture-model');
          expect(catalog.voice, isTrue);
          final file = DraftFile(
            'report.txt',
            'text/plain',
            Uint8List.fromList(utf8.encode('encrypted uploaded report')),
          );
          final image = DraftFile(
            'pixel.png',
            'image/png',
            base64Decode(
              'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg==',
            ),
          );
          final fileId = await c.uploadFile(session.id, file),
              imageId = await c.uploadFile(session.id, image);
          await c.sendInput(
            session.id,
            'generation-check',
            const TurnOptions(model: 'fixture-model', effort: 'high'),
            [fileId, imageId],
          );
          await until(
            () =>
                c.transcripts[session.id]?.blocks.any(
                  (b) => b.text.contains('model=fixture-model effort=high'),
                ) ??
                false,
          );
          await until(
            () => c.sessions.any(
              (s) => s.id == session.id && s.status == 'completed',
            ),
          );
          final uploaded = await c.attachment(
            session.id,
            'agentdeck-file:$fileId',
          );
          expect(utf8.decode(uploaded.bytes), 'encrypted uploaded report');
          expect(
            (await c.attachment(session.id, 'agentdeck-file:$imageId')).bytes,
            image.bytes,
          );
          final legacyBytes = await File(
            '../backend/internal/document/testdata/book97.xls',
          ).readAsBytes();
          final legacyDraft = DraftFile(
            'old.xls',
            'application/vnd.ms-excel',
            legacyBytes,
          );
          final legacyId = await c.uploadFile(session.id, legacyDraft);
          final legacyPreview = await c.attachment(
            session.id,
            'agentdeck-file:$legacyId',
          );
          expect(legacyPreview.mime, 'application/vnd.agentdeck.office+json');
          expect(jsonDecode(utf8.decode(legacyPreview.bytes))['kind'], 'excel');
          await c.removeDraftFile(legacyDraft);
          final binary = File('../artifacts/agentdeck').absolute.path;
          final state = proxy ? '$dir/proxy/state' : '$dir/state';
          final read = await Process.run(binary, [
            'input-file',
            '--data',
            state,
            '--session',
            session.id,
            '--upload',
            fileId,
          ]);
          expect(read.exitCode, 0);
          expect(read.stdout, 'encrypted uploaded report');
          final transcript = await c.transcribeRecording(
            session.id,
            recordingWav(Uint8List(32000)),
          );
          expect(transcript, 'VOICE_FIXTURE_OK');
          expect(
            await c.transcribeRecording(
              session.id,
              recordingWav(Uint8List(64000)),
              preview: true,
            ),
            'VOICE_FIXTURE_OK',
          );
          for (final decision in ['accept', 'decline']) {
            await c.sendPrompt(session.id, 'approval wire test');
            await until(
              () =>
                  c.transcripts[session.id]?.blocks.any(
                    (b) => b.kind == 'approval' && b.status == 'pending',
                  ) ??
                  false,
            );
            final block = c.transcripts[session.id]!.blocks.lastWhere(
              (b) => b.kind == 'approval' && b.status == 'pending',
            );
            await c.approve(
              session.id,
              block.key.replaceFirst(RegExp(r':approval$'), ''),
              decision,
            );
            await until(
              () =>
                  c.transcripts[session.id]?.blocks.any(
                    (b) =>
                        b.text ==
                        (decision == 'accept' ? 'APPROVED' : 'DECLINED'),
                  ) ??
                  false,
            );
            await until(
              () => c.sessions.any(
                (s) => s.id == session.id && s.status == 'completed',
              ),
            );
          }
          await until(
            () => c.sessions.any(
              (s) => s.id == session.id && s.status == 'completed',
            ),
          );
          final duplicate = {
            'method': 'send',
            'id': 'duplicate-$agent',
            'session': session.id,
            'text': 'idempotency probe',
          };
          await c.send(duplicate);
          await c.send(duplicate);
          await until(
            () =>
                c.transcripts[session.id]?.blocks.any(
                  (b) => b.role == 'user' && b.text == 'idempotency probe',
                ) ??
                false,
          );
          await until(
            () => c.sessions.any(
              (s) => s.id == session.id && s.status == 'completed',
            ),
          );
          expect(
            c.transcripts[session.id]!.blocks
                .where((b) => b.role == 'user' && b.text == 'idempotency probe')
                .length,
            1,
          );
          await c.sendPrompt(session.id, 'pause wire test');
          await c.sendDelivery(
            session.id,
            'Queued follow-up',
            const TurnOptions(),
            [],
            'queue',
          );
          await until(() => c.queues[session.id]?.isNotEmpty ?? false);
          final queued = c.queues[session.id]!.single;
          expect(queued.origin, 'local');
          await c.changeQueue(
            session.id,
            queued,
            'edit',
            text: 'Edited follow-up',
          );
          await until(
            () => c.queues[session.id]?.single.text == 'Edited follow-up',
          );
          final edited = c.queues[session.id]!.single;
          await expectLater(
            c.changeQueue(session.id, queued, 'delete'),
            throwsA(isA<HttpException>()),
          );
          await c.changeQueue(session.id, edited, 'delete');
          await until(() => c.queues[session.id]?.isEmpty ?? false);
          await c.interrupt(session.id);
          await until(
            () =>
                c.transcripts[session.id]?.blocks.any(
                  (b) => b.text == 'STOPPED',
                ) ??
                false,
          );
        }
        await until(() => c.sessions.any((s) => s.id == 'paged-session'));
        await c.watch('paged-session');
        await until(() => c.transcripts['paged-session']?.blocks.length == 40);
        final paged = c.transcripts['paged-session']!;
        expect(paged.blocks.first.text, 'Paged message 200');
        expect(paged.blocks.last.text, 'Paged message 239');
        expect(paged.hasMore, isTrue);
        await Future<void>.delayed(const Duration(milliseconds: 700));
        expect(paged.blocks.length, 40); // No automatic bulk catch-up.
        await c.loadOlder('paged-session');
        expect(paged.blocks.length, 80);
        expect(paged.blocks.first.text, 'Paged message 160');
        expect(paged.blocks.map((b) => b.key).toSet().length, 80);
        final directory = await c.outlinePage('paged-session');
        expect((directory['entries'] as List).length, 200);
        expect(directory['has_more'], true);
        final olderDirectory = await c.outlinePage(
          'paged-session',
          before: (directory['before'] as num).toInt(),
        );
        expect((olderDirectory['entries'] as List).length, 40);
        final firstRecord = Map<String, dynamic>.from(
          (olderDirectory['entries'] as List).last as Map,
        );
        await c.jumpToRecord(
          'paged-session',
          (firstRecord['first'] as num).toInt(),
        );
        expect(paged.blocks.length, 40);
        expect(paged.blocks.first.text, 'Paged message 0');
        expect(paged.blocks.first.key, firstRecord['key']);
        expect(paged.detached, true);
        await Future<void>.delayed(const Duration(milliseconds: 700));
        expect(paged.blocks.length, 40);
        await c.reload('paged-session');
        await until(
          () => c.transcripts['paged-session']!.blocks.any(
            (b) => b.text == 'Paged message 239',
          ),
        );
        expect(c.transcripts['paged-session']!.detached, false);

        await c.watch('e2e-session');
        await until(
          () => (c.transcripts['e2e-session']?.blocks.length ?? 0) >= 3,
        );
        expect(
          c.transcripts['e2e-session']!.blocks.any(
            (b) => b.text.contains('Go → Flutter'),
          ),
          isTrue,
        );
        final imageBlock = c.transcripts['e2e-session']!.blocks.firstWhere(
          (b) => b.kind == 'image',
        );
        final bytes = await c.image(imageBlock.media);
        expect(bytes.take(4), [137, 80, 78, 71]);
        final codec = await ui.instantiateImageCodec(bytes);
        final frame = await codec.getNextFrame();
        expect(frame.image.width, greaterThan(0));
        frame.image.dispose();
        codec.dispose();
        final linked = await c.attachment('e2e-session', 'linked.md:2');
        expect(linked.line, 2);
        expect(utf8.decode(linked.bytes), contains('Encrypted linked text'));
        final linkedImage = await c.attachment('e2e-session', 'local.png');
        expect(linkedImage.isImage, isTrue);
        expect(linkedImage.bytes, bytes);
        final audio = await c.attachment('e2e-session', 'tone.wav');
        expect(audio.mime, 'audio/wav');
        expect(utf8.decode(audio.bytes.sublist(0, 4)), 'RIFF');
        final textPreview = await c.attachment('e2e-session', 'notes.txt');
        expect(utf8.decode(textPreview.bytes), '中文 text preview');
        for (final legacy in ['word97.doc', 'book97.xls']) {
          final doc = await c.attachment('e2e-session', legacy);
          expect(doc.mime, 'application/vnd.agentdeck.office+json');
          final content = jsonDecode(utf8.decode(doc.bytes)) as Map;
          expect(content['kind'], legacy.endsWith('.doc') ? 'word' : 'excel');
        }
        final video = await c.attachment('e2e-session', 'clip.mp4');
        expect(video.mime, 'video/mp4');
        expect(video.bytes.sublist(4, 8), utf8.encode('ftyp'));
        expect(utf8.decode(video.bytes.sublist(12)), 'encrypted-video-fixture');
        await expectLater(
          c.attachment('different-session', 'local.png'),
          throwsA(isA<HttpException>()),
        );
        final before = c.epoch;
        wallOffset = -wallOffset * 2;
        expect(
          (clock.millis - DateTime.now().millisecondsSinceEpoch).abs(),
          lessThan(10000),
        );
        await Future<void>.delayed(const Duration(milliseconds: 1200));
        expect(c.epoch, greaterThan(before));
        await c.send({'method': 'ping'});
        final file = File('$pairDir/source/live.jsonl');
        await file.writeAsString(
          '${jsonEncode({
            'method': 'item/agentMessage/delta',
            'params': {'threadId': 'e2e-session', 'itemId': 'm', 'delta': ' · 新增'},
          })}\n',
          mode: FileMode.append,
        );
        await until(
          () => c.transcripts['e2e-session']!.blocks.any(
            (b) => b.text.contains('新增'),
          ),
        );
        final cursor = c.transcripts['e2e-session']!.cursor;
        final pairedRoot = vault.value!['root'];
        c.suspend();
        expect(c.connected, isFalse);
        await c.resume();
        await until(() => c.connected);
        expect(vault.value!['root'], pairedRoot);
        expect(
          (clock.millis - DateTime.now().millisecondsSinceEpoch).abs(),
          lessThan(10000),
        );
        await Future<void>.delayed(const Duration(milliseconds: 700));
        expect(c.transcripts['e2e-session']!.cursor, cursor);
        expect(
          c.transcripts['e2e-session']!.blocks
              .where((b) => b.kind == 'message')
              .length,
          1,
        );
        await until(() {
          final list =
              (vault.cache['transcripts'] as Map?)?['e2e-session'] as List?;
          return list != null &&
              list.isNotEmpty &&
              (list.last as Map)['seq'] == cursor;
        });
        final restored = AgentClient(vault: vault, trustedRoots: roots);
        addTearDown(restored.dispose);
        await restored.initialize();
        await restored.watch('e2e-session');
        expect(
          restored.transcripts['e2e-session']!.blocks
              .where((b) => b.kind == 'message')
              .single
              .text,
          contains('Go → Flutter'),
        );
        expect(restored.transcripts['e2e-session']!.cursor, cursor);
        if (proxy) {
          expect(vault.value!['tls_trust'], 'system');
          expect(
            await File('$pairDir/pin-before').readAsString(),
            isNot(await File('$pairDir/pin-after').readAsString()),
          );
          await File('$pairDir/rotate').writeAsString('rotate certificate key');
          await c.connect();
          expect(c.connected, isTrue);
          expect(clock.calibrated, isTrue);
          expect(
            (clock.millis - DateTime.now().millisecondsSinceEpoch).abs(),
            lessThan(10000),
          );
          expect(vault.value!['device'], c.device!['device']);
          await c.send({'method': 'ping'});
          await Future<void>.delayed(const Duration(milliseconds: 700));
          expect(c.connected, isTrue);
          expect(clock.calibrated, isTrue);
          expect(
            (clock.millis - DateTime.now().millisecondsSinceEpoch).abs(),
            lessThan(10000),
          );
        }
        final wrong = Map<String, dynamic>.from(vault.value!)
          ..['tls_trust'] = 'pin'
          ..['pin'] = '0' * 64;
        final bad = AgentClient(vault: MemoryVault());
        bad.device = wrong;
        addTearDown(bad.dispose);
        await bad.connect();
        expect(bad.connected, isFalse);
        expect(bad.error, contains('证书'));
        await c.forget();
        expect(vault.value, isNull);
        expect(c.transcripts, isEmpty);
      },
      // This case performs pairing, uploads, rotation and reconnects. Give the
      // whole integration the same budget as gateway coverage; per-step waits
      // and protocol epoch / expiry checks stay unchanged.
      timeout: const Timeout(Duration(seconds: 90)),
      skip: dir == null
          ? 'Run scripts/verify.sh for real Go/Flutter integration'
          : false,
    );
  }
}
