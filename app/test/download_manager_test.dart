import 'dart:async';
import 'dart:typed_data';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/download.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:agentdeck/attachment_page.dart';
import 'package:agentdeck/language.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'download_test.dart' show DownloadClient;

class DownloadsVault extends DeviceVault {
  List<dynamic> rows = [];
  @override
  Future<List<dynamic>> loadDownloads() async => rows;
  @override
  Future<void> saveDownloads(List<dynamic> value) async {
    rows = value;
  }
}

class ResumeClient extends DownloadClient {
  String version = 'v1';
  Completer<void>? gate;
  final offsets = <int>[];
  @override
  Future<dynamic> request(
    Map<String, dynamic> data, {
    Duration timeout = const Duration(seconds: 70),
  }) async {
    if (data['method'] == 'attachment_open') {
      return {'download': 'f', 'size': size, 'version': version};
    }
    if (data['method'] == 'attachment_read') {
      offsets.add(data['offset'] as int);
      await gate?.future;
    }
    return super.request(data, timeout: timeout);
  }
}

Future<void> until(bool Function() done) async {
  for (var i = 0; i < 300 && !done(); i++) {
    await Future<void>.delayed(const Duration(milliseconds: 5));
  }
  expect(done(), true);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'range resume starts at committed offset and rejects changed source',
    () async {
      final c = ResumeClient();
      addTearDown(c.dispose);
      final control = DownloadControl();
      var written = 0;
      await c.downloadAttachment(
        's',
        'file.bin',
        startOffset: 256 << 10,
        version: 'v1',
        expectedSize: c.size,
        write: (b) async {
          written += b.length;
        },
        control: control,
        progress: (_, _) {},
      );
      expect(c.offsets.first, 256 << 10);
      expect(written, c.size - (256 << 10));
      c.version = 'v2';
      c.offsets.clear();
      await expectLater(
        c.downloadAttachment(
          's',
          'file.bin',
          startOffset: 256 << 10,
          version: 'v1',
          write: (_) async {
            fail('changed bytes must never be appended');
          },
          control: DownloadControl(),
          progress: (_, _) {},
        ),
        throwsA(isA<Exception>()),
      );
      expect(c.offsets, isEmpty);
    },
  );
  test(
    'task persists offset, holds background connection, resumes and restores records',
    () async {
      final c = ResumeClient()..device = {'device': 'owner-a'};
      addTearDown(c.dispose);
      final vault = DownloadsVault();
      final tasksClient = AgentClient(vault: vault)
        ..device = {'device': 'owner-a'};
      // Bind the manager to the scripted transport while persisting through its own vault.
      final manager = c.downloads;
      final platform = <String>[];
      var bytes = 0;
      var pauseOnce = true;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(DownloadDestination.channel, (call) async {
            platform.add(call.method);
            if (call.method == 'choose') return 'destination';
            if (call.method == 'write') {
              bytes += (call.arguments['bytes'] as Uint8List).length;
              if (pauseOnce) {
                pauseOnce = false;
                manager.pause(manager.tasks.single);
              }
            }
            return null;
          });
      addTearDown(() {
        tasksClient.dispose();
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(DownloadDestination.channel, null);
      });
      final task = await manager.start(
        's',
        'file.bin',
        'file.bin',
        'application/octet-stream',
        c.size,
      );
      await until(() => task!.control == null);
      expect(task!.received, 256 << 10);
      expect(task.status, 'paused');
      expect(platform, isNot(contains('cancel')));
      final persisted = await c.vault.loadDownloads();
      expect((persisted.single as Map)['received'], task.received);
      c.gate = Completer<void>();
      final future = manager.resume(task);
      await until(() => c.offsets.length >= 5);
      c.suspend();
      expect(c.connected, true);
      c.gate!.complete();
      await future;
      expect(task.complete, true);
      expect(bytes, c.size);
      expect(platform, contains('finish'));
      // Restore the saved metadata without starting another completed transfer.
      vault.rows = await c.vault.loadDownloads();
      final restored = tasksClient.downloads;
      await restored.restore();
      expect(restored.tasks.single.complete, true);
      expect(restored.tasks.single.received, c.size);
      final other = AgentClient(vault: vault)..device = {'device': 'owner-b'};
      await other.downloads.restore();
      expect(other.downloads.tasks, isEmpty);
      other.dispose();
    },
  );
  testWidgets('popping attachment preview keeps the independent task alive', (
    tester,
  ) async {
    final c = ResumeClient()..gate = Completer<void>();
    addTearDown(c.dispose);
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          DownloadDestination.channel,
          (call) async => call.method == 'choose' ? 'destination' : null,
        );
    addTearDown(
      () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(DownloadDestination.channel, null),
    );
    await tester.pumpWidget(
      MaterialApp(
        locale: AppLanguage.simplifiedChinese.locale,
        localizationsDelegates: UiStrings.delegates,
        supportedLocales: UiStrings.supportedLocales,
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (_) =>
                      AttachmentPage(client: c, session: 's', href: 'file.bin'),
                ),
              ),
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('下载原文件'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认下载'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
    expect(c.downloads.tasks.single.running, true);
    tester.state<NavigatorState>(find.byType(Navigator).first).pop();
    await tester.pumpAndSettle();
    expect(c.downloads.tasks.single.control!.cancelled, false);
    c.gate!.complete();
    await tester.pumpAndSettle();
    expect(c.downloads.tasks.single.complete, true);
  });
}
