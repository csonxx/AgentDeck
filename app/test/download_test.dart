import 'dart:convert';
import 'dart:typed_data';
import 'package:agentdeck/attachment_page.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/download.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/language.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class DownloadClient extends AgentClient {
  final methods = <String>[];
  int size = 9 << 20;
  bool failRead = false;
  int preparations = 0;
  @override
  Future<void> prepareAttachmentTransfer() async {
    preparations++;
    connected = true;
  }

  DownloadClient() {
    connected = true;
  }
  @override
  Future<SessionAttachment> attachment(String session, String href) async =>
      SessionAttachment(
        name: 'large.bin',
        mime: 'application/octet-stream',
        bytes: Uint8List(0),
        size: size,
        downloadRequired: true,
      );
  @override
  Future<dynamic> request(
    Map<String, dynamic> data, {
    Duration timeout = const Duration(seconds: 70),
  }) async {
    methods.add(data['method'] as String);
    if (data['method'] == 'attachment_open') {
      return {'download': 'fixture', 'size': size, 'version': 'v1'};
    }
    if (data['method'] == 'attachment_read') {
      if (failRead) throw StateError('fixture failure');
      final offset = data['offset'] as int;
      final count = (size - offset).clamp(0, 256 << 10);
      return {
        'bytes': base64Encode(Uint8List(count)),
        'offset': offset + count,
        'size': size,
      };
    }
    return true;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'download streams beyond 8MB and closes on cancellation/failure',
    () async {
      final c = DownloadClient();
      addTearDown(c.dispose);
      int count = 0, parts = 0;
      await c.downloadAttachment(
        's',
        'large.bin',
        control: DownloadControl(),
        expectedSize: c.size,
        write: (bytes) async {
          count += bytes.length;
          parts++;
          expect(bytes.length, lessThanOrEqualTo(256 << 10));
        },
        progress: (_, _) {},
      );
      expect(count, 9 << 20);
      expect(parts, 36);
      expect(c.methods.last, 'attachment_close');
      final cancel = DownloadControl();
      await expectLater(
        c.downloadAttachment(
          's',
          'large.bin',
          control: cancel,
          write: (_) async {
            cancel.cancelled = true;
          },
          progress: (_, _) {},
        ),
        throwsA(isA<DownloadCancelled>()),
      );
      expect(c.methods.last, 'attachment_close');
      c.failRead = true;
      await expectLater(
        c.downloadAttachment(
          's',
          'large.bin',
          control: DownloadControl(),
          write: (_) async {},
          progress: (_, _) {},
        ),
        throwsStateError,
      );
      expect(c.methods.last, 'attachment_close');
      c.failRead = false;
      await expectLater(
        c.downloadAttachment(
          's',
          'large.bin',
          expectedSize: 1,
          control: DownloadControl(),
          write: (_) async {
            fail('Changed size must not write');
          },
          progress: (_, _) {},
        ),
        throwsA(isA<Exception>()),
      );
      expect(c.methods.last, 'attachment_close');
    },
  );

  testWidgets(
    'large file requires confirmation and picker; writes chunks, opens saved file',
    (tester) async {
      final c = DownloadClient();
      addTearDown(c.dispose);
      final platform = <String>[];
      var written = 0;
      bool pickerCancelled = true;
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        DownloadDestination.channel,
        (call) async {
          platform.add(call.method);
          if (call.method == 'choose') {
            c.connected = false;
            return pickerCancelled ? null : 'saved';
          }
          if (call.method == 'write') {
            written += (call.arguments['bytes'] as Uint8List).length;
          }
          return null;
        },
      );
      addTearDown(
        () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          DownloadDestination.channel,
          null,
        ),
      );
      await tester.pumpWidget(
        MaterialApp(
          locale: AppLanguage.simplifiedChinese.locale,
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          home: AttachmentPage(client: c, session: 's', href: 'large.bin'),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('9.0 MB'), findsOneWidget);
      expect(c.methods, isEmpty);
      expect(platform, isEmpty);
      await tester.tap(find.text('下载原文件'));
      await tester.pumpAndSettle();
      expect(find.text('下载附件？'), findsOneWidget);
      await tester.tap(find.text('取消'));
      await tester.pumpAndSettle();
      expect(c.methods, isEmpty);
      expect(platform, isEmpty);
      await tester.tap(find.text('下载原文件'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('确认下载'));
      await tester.pumpAndSettle();
      expect(platform, ['choose']);
      expect(c.methods, isEmpty);
      pickerCancelled = false;
      await tester.tap(find.text('下载原文件'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('确认下载'));
      await tester.pumpAndSettle();
      expect(written, c.size);
      expect(c.preparations, greaterThan(0));
      expect(platform, contains('finish'));
      expect(find.textContaining('已保存到所选位置'), findsOneWidget);
      await tester.tap(find.text('其他应用打开'));
      await tester.pumpAndSettle();
      expect(platform.last, 'open');
      c.failRead = true;
      await tester.tap(find.text('下载原文件'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('确认下载'));
      await tester.pumpAndSettle();
      expect(platform, isNot(contains('cancel')));
      expect(find.text('下载暂停，已保留进度。检查网络或存储后继续。'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );
}
