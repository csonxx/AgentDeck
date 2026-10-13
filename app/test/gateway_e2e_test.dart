// Real Go gateway + outbound S-LOCAL CLI tunnel + native Dart client. Only the
// external Feishu identity provider and human approval are simulated fixtures.
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:agentdeck/core/download.dart';
import 'dart:io';
import 'dart:typed_data';
import 'package:agentdeck/core/input.dart';
import 'dart:ui' as ui;
import 'package:flutter_test/flutter_test.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/core/clock.dart';
import 'package:agentdeck/core/gateway.dart';
import 'package:agentdeck/core/relay.dart';
import 'package:agentdeck/core/transport.dart';
import 'package:agentdeck/core/vault.dart';

class GatewayVault extends DeviceVault {
  List<dynamic> downloads = [];
  @override
  Future<List<dynamic>> loadDownloads() async => downloads;
  @override
  Future<void> saveDownloads(List<dynamic> value) async {
    downloads = value;
  }

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
  final end = DateTime.now().add(const Duration(seconds: 20));
  while (!f()) {
    if (DateTime.now().isAfter(end)) fail('Relay condition timed out');
    await Future<void>.delayed(const Duration(milliseconds: 50));
  }
}

void main() {
  final dir = Platform.environment['AGENTDECK_E2E_DIR'];
  test(
    'Feishu-matched relay is E2E encrypted, resumes, isolates tenants and device pairs',
    () async {
      final meta =
          jsonDecode(await File('$dir/gateway/metadata.json').readAsString())
              as Map;
      final origin = meta['url'] as String;
      final roots = SecurityContext(withTrustedRoots: false)
        ..setTrustedCertificates('$dir/gateway/tls/tls.pem');
      final h = createTransport('system', '', roots: roots);
      addTearDown(() => h.close(force: true));
      Future<int> fixture(String action, Map<String, dynamic> body) async {
        final req = await h.postUrl(Uri.parse('$origin/fixture/$action'));
        req.write(jsonEncode(body));
        final res = await req.close();
        await res.drain<void>();
        return res.statusCode;
      }

      Future<GatewayLogin> login([String user = '']) async {
        final g = GatewayLogin(
          origin,
          trustedRoots: roots,
          clock: ProtocolClock(
            wallMillis: () => DateTime.now()
                .subtract(const Duration(days: 1))
                .millisecondsSinceEpoch,
          ),
        );
        addTearDown(g.close);
        await g.begin();
        expect(await fixture('complete', {'flow': g.flow, 'user': user}), 200);
        expect(await g.poll(), isTrue);
        return g;
      }

      final gateway = await login();
      final nodes = await gateway.locals();
      expect(nodes.length, 1);
      expect(nodes.single['id'], meta['local']);
      expect(nodes.single['online'], isTrue);
      final vault = GatewayVault();
      var phoneWallOffset = -86400000;
      final clock = ProtocolClock(
        wallMillis: () =>
            DateTime.now().millisecondsSinceEpoch + phoneWallOffset,
      );
      final c = AgentClient(vault: vault, trustedRoots: roots, clock: clock);
      addTearDown(c.dispose);
      final pending = await gateway.offer(nodes.single['id'] as String);
      expect(pending.code.length, 12);
      await expectLater(pending.complete(c), throwsA(isA<ApprovalPending>()));
      expect(
        await fixture('approve', {
          'grant': pending.bundle.relay!['grant'],
          'code': '000000000000',
        }),
        401,
      );
      expect(
        await fixture('approve', {
          'grant': pending.bundle.relay!['grant'],
          'code': pending.code,
        }),
        200,
      );
      await pending.complete(c);
      expect(c.connected, isTrue);
      expect(clock.calibrated, isTrue);
      // Gateway runs one day ahead of Local and the phone wall clock is one day behind.
      expect(
        (clock.millis - DateTime.now().millisecondsSinceEpoch - 86400000).abs(),
        lessThan(10000),
      );
      phoneWallOffset = 3 * 86400000;
      await c.send({'method': 'ping'});
      c.suspend();
      await c.resume();
      expect(c.connected, isTrue);
      expect(
        (clock.millis - DateTime.now().millisecondsSinceEpoch - 86400000).abs(),
        lessThan(10000),
      );
      await until(() => c.sessions.any((s) => s.id == 'e2e-session'));
      expect(
        await c.request({
          'method': 'ping',
        }, timeout: const Duration(seconds: 5)),
        isNull,
      );
      await until(
        () =>
            c.sessions.any((s) => s.id == 'bridge-codex') &&
            c.sessions.any((s) => s.id == 'bridge-claude'),
      );
      expect(c.projects, isNotEmpty);
      final mobileSession = await c.createSession('codex', c.projects.first.id);
      final catalog = await c.inputCatalog('codex', mobileSession.id);
      expect(catalog.models.single.id, 'fixture-model');
      final file = DraftFile(
        'gateway.txt',
        'text/plain',
        Uint8List.fromList(utf8.encode('encrypted gateway file')),
      );
      final upload = await c.uploadFile(mobileSession.id, file);
      await c.sendInput(
        mobileSession.id,
        'generation-check',
        const TurnOptions(
          model: 'fixture-model',
          effort: 'high',
          speed: 'fast',
        ),
        [upload],
      );
      await until(
        () =>
            c.transcripts[mobileSession.id]?.blocks.any(
              (b) => b.text.contains('model=fixture-model effort=high'),
            ) ??
            false,
      );
      await until(
        () => c.sessions.any(
          (s) => s.id == mobileSession.id && s.status == 'completed',
        ),
      );
      expect(
        utf8.decode(
          (await c.attachment(
            mobileSession.id,
            'agentdeck-file:$upload',
          )).bytes,
        ),
        'encrypted gateway file',
      );
      expect(
        await c.transcribeRecording(
          mobileSession.id,
          recordingWav(Uint8List(32000)),
        ),
        'VOICE_FIXTURE_OK',
      );
      expect(
        await c.transcribeRecording(
          mobileSession.id,
          recordingWav(Uint8List(64000)),
          preview: true,
        ),
        'VOICE_FIXTURE_OK',
      );
      await c.sendPrompt(mobileSession.id, 'runtime through encrypted relay');
      await until(
        () =>
            c.transcripts[mobileSession.id]?.blocks.any(
              (b) => b.text == 'RUNTIME_OK',
            ) ??
            false,
      );
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
      var lastIndex = 39;
      while (paged.detached) {
        await c.loadNewer('paged-session');
        final nextIndex = int.parse(paged.blocks.last.text.split(' ').last);
        expect(nextIndex, greaterThan(lastIndex));
        expect(paged.blocks.length, lessThanOrEqualTo(Transcript.maxBlocks));
        expect(
          paged.blocks.map((b) => b.key).toSet().length,
          paged.blocks.length,
        );
        lastIndex = nextIndex;
      }
      expect(lastIndex, 239);
      expect(paged.blocks.length, 200);
      expect(paged.hasMore, true);
      await c.loadOlder('paged-session');
      expect(paged.detached, true);
      await c.loadNewer('paged-session');
      expect(paged.blocks.last.text, 'Paged message 239');
      expect(paged.detached, false);
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
      final image = c.transcripts['e2e-session']!.blocks.firstWhere(
        (b) => b.kind == 'image',
      );
      final bytes = await c.image(image.media);
      final codec = await ui.instantiateImageCodec(bytes);
      final frame = await codec.getNextFrame();
      expect(frame.image.width, greaterThan(0));
      frame.image.dispose();
      codec.dispose();
      final large = await c.attachment('e2e-session', 'large.bin');
      expect(large.downloadRequired, isTrue);
      expect(large.bytes, isEmpty);
      expect(large.size, 9 << 20);
      final digestOutput = <Digest>[];
      final hash = sha256.startChunkedConversion(
        ChunkedConversionSink<Digest>.withCallback(digestOutput.addAll),
      );
      var downloaded = 0;
      await c.downloadAttachment(
        'e2e-session',
        'large.bin',
        control: DownloadControl(),
        expectedSize: large.size,
        write: (chunk) async {
          expect(chunk.length, lessThanOrEqualTo(512 << 10));
          hash.add(chunk);
        },
        progress: (done, total) {
          downloaded = done;
          expect(total, large.size);
        },
      );
      hash.close();
      expect(downloaded, large.size);
      expect(
        digestOutput.single.toString(),
        sha256.convert(List.generate(9 << 20, (i) => i % 256)).toString(),
      );
      // Cancel after one chunk: later reads stop and the descriptor is closed.
      final cancelDownload = DownloadControl();
      await expectLater(
        c.downloadAttachment(
          'e2e-session',
          'large.bin',
          control: cancelDownload,
          write: (_) async {
            cancelDownload.cancelled = true;
          },
          progress: (_, _) {},
        ),
        throwsA(isA<DownloadCancelled>()),
      );
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
      final epoch = c.epoch;
      await Future<void>.delayed(
        Duration(
          milliseconds:
              int.parse(
                Platform.environment['AGENTDECK_E2E_ROTATION_MS'] ?? '1000',
              ) +
              200,
        ),
      );
      expect(c.epoch, greaterThan(epoch));
      await c.send({'method': 'ping'});
      final before = c.transcripts['e2e-session']!.cursor;
      expect(await fixture('disconnect', {}), 200);
      await until(() => !c.connected);
      await File('$dir/gateway/source/live.jsonl').writeAsString(
        '${jsonEncode({
          'method': 'item/agentMessage/delta',
          'params': {'threadId': 'e2e-session', 'itemId': 'm', 'delta': ' · Relay resumed'},
        })}\n',
        mode: FileMode.append,
      );
      await until(
        () => c.connected && c.transcripts['e2e-session']!.cursor > before,
      );
      expect(
        c.transcripts['e2e-session']!.blocks
            .where((b) => b.kind == 'message')
            .single
            .text,
        contains('Relay resumed'),
      );
      await until(
        () => (vault.cache['transcripts'] as Map?)?['e2e-session'] != null,
      );
      final restored = AgentClient(vault: vault, trustedRoots: roots);
      addTearDown(restored.dispose);
      await restored.initialize();
      expect(restored.connected, isTrue);
      final audit = await h.getUrl(Uri.parse('$origin/fixture/audit'));
      final res = await audit.close();
      final receipt =
          jsonDecode(await res.transform(utf8.decoder).join()) as Map;
      expect(receipt['gateway_has_no_local_content_or_device_roots'], isTrue);
      for (final identity in ['other-owner', 'other-tenant']) {
        final other = await login(identity);
        expect(await other.locals(), isEmpty);
      }
      final bad = AgentClient(vault: GatewayVault(), trustedRoots: roots);
      addTearDown(bad.dispose);
      bad.device = Map<String, dynamic>.from(vault.value!)
        ..['root'] = pending.bundle.relay!['key'];
      await bad.connect();
      expect(bad.connected, isFalse);
      final second = await gateway.offer(nodes.single['id'] as String);
      expect(
        await fixture('approve', {
          'grant': second.bundle.relay!['grant'],
          'code': second.code,
        }),
        200,
      );
      final vault2 = GatewayVault();
      final c2 = AgentClient(vault: vault2, trustedRoots: roots);
      addTearDown(c2.dispose);
      await second.complete(c2);
      expect(c2.connected, isTrue);
      expect(vault2.value!['root'], isNot(vault.value!['root']));
      expect(
        await fixture('revoke', {'grant': pending.bundle.relay!['grant']}),
        200,
      );
      await until(() => !c.connected && !restored.connected);
      await c2.send({'method': 'ping'});
      await Future<void>.delayed(const Duration(milliseconds: 500));
      expect(c2.connected, isTrue);
      await c2.forget();
      expect(vault2.value, isNull);
    },
    skip: dir == null
        ? 'Run scripts/verify.sh for real gateway integration'
        : false,
    timeout: const Timeout(Duration(seconds: 180)),
  );
}
