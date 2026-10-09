import 'package:agentdeck/language.dart';
import 'dart:io';
import 'dart:convert';
import 'dart:typed_data';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/main.dart';
import 'package:agentdeck/attachment_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:url_launcher_platform_interface/url_launcher_platform_interface.dart';

class AttachmentClient extends AgentClient {
  SessionAttachment? output;
  String requestedSession = '', requestedHref = '';
  bool unavailable = false;
  @override
  Future<SessionAttachment> attachment(String session, String href) async {
    requestedSession = session;
    requestedHref = href;
    if (unavailable) throw const HttpException('文件已不存在或无法读取');
    return output!;
  }
}

class BrowserLauncher extends UrlLauncherPlatform {
  @override
  get linkDelegate => null;
  String opened = '';
  PreferredLaunchMode? mode;
  @override
  Future<bool> launchUrl(String url, LaunchOptions options) async {
    opened = url;
    mode = options.mode;
    return true;
  }
}

void main() {
  testWidgets('uploaded binary file opens a safe details view', (tester) async {
    final c = AttachmentClient();
    addTearDown(c.dispose);
    c.output = SessionAttachment(
      name: 'sample.pdf',
      mime: 'application/pdf',
      bytes: Uint8List.fromList([37, 80, 68, 70, 255]),
    );
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: UiStrings.delegates,
        supportedLocales: UiStrings.supportedLocales,
        locale: AppLanguage.simplifiedChinese.locale,
        home: Scaffold(
          body: Builder(
            builder: (context) => TextButton(
              onPressed: () =>
                  openSessionLink(context, c, 'chat', 'agentdeck-file:fixture'),
              child: const Text('文件'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('文件'));
    await tester.pumpAndSettle();
    expect(c.requestedHref, 'agentdeck-file:fixture');
    expect(find.text('sample.pdf'), findsNWidgets(2));
    expect(find.textContaining('此格式暂不支持预览'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  testWidgets(
    'Markdown image loads through session attachment and opens zoom',
    (tester) async {
      final c = AttachmentClient();
      addTearDown(c.dispose);
      final bytes = (await tester.runAsync(
        () => File('test/fixtures/output.png').readAsBytes(),
      ))!;
      c.output = SessionAttachment(
        name: 'a b.png',
        mime: 'image/png',
        bytes: bytes,
      );
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: Scaffold(
            body: TranscriptTile(
              client: c,
              session: 'existing',
              block: TranscriptBlock(
                key: 'm',
                kind: 'message',
                role: 'assistant',
                text: '![结果](</workspace/a b.png>)',
              ),
            ),
          ),
        ),
      );
      await tester.pump();
      await tester.runAsync(
        () => precacheImage(
          MemoryImage(bytes),
          tester.element(find.byType(EncryptedImage)),
        ),
      );
      await tester.pumpAndSettle();
      expect(c.requestedSession, 'existing');
      expect(Uri.decodeFull(c.requestedHref), '/workspace/a b.png');
      expect(find.byType(Image), findsOneWidget);
      expect(find.text('图片见独立预览'), findsNothing);
      await tester.tap(find.byType(Image));
      await tester.pumpAndSettle();
      expect(find.byType(InteractiveViewer), findsOneWidget);
      expect(find.text('图片预览'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets('Local Markdown link previews text at its source line', (
    tester,
  ) async {
    final c = AttachmentClient();
    addTearDown(c.dispose);
    c.output = SessionAttachment(
      name: 'README.md',
      mime: 'text/plain',
      line: 12,
      bytes: Uint8List.fromList(
        utf8.encode(List.generate(30, (i) => 'line ${i + 1}').join('\n')),
      ),
    );
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: UiStrings.delegates,
        supportedLocales: UiStrings.supportedLocales,
        locale: AppLanguage.simplifiedChinese.locale,
        home: Scaffold(
          body: TranscriptTile(
            client: c,
            session: 's',
            block: TranscriptBlock(
              key: 'm',
              kind: 'message',
              role: 'assistant',
              text: '[查看文档](/workspace/README.md:12)',
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('查看文档'));
    await tester.pumpAndSettle();
    expect(c.requestedHref, '/workspace/README.md:12');
    expect(c.requestedSession, 's');
    expect(find.text('README.md'), findsOneWidget);
    expect(find.text('定位到第 12 行'), findsOneWidget);
    expect(find.text('line 12'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  testWidgets(
    'Markdown HTTPS links launch browser; unsafe schemes stay blocked',
    (tester) async {
      final c = AgentClient();
      addTearDown(c.dispose);
      final old = UrlLauncherPlatform.instance;
      final browser = BrowserLauncher();
      UrlLauncherPlatform.instance = browser;
      addTearDown(() => UrlLauncherPlatform.instance = old);
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: Scaffold(
            body: TranscriptTile(
              client: c,
              block: TranscriptBlock(
                key: 'm',
                kind: 'message',
                text: '[网页](https://example.com/docs)',
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('网页'));
      await tester.pumpAndSettle();
      expect(browser.opened, 'https://example.com/docs');
      expect(browser.mode, PreferredLaunchMode.inAppBrowserView);
      await openSessionLink(
        tester.element(find.byType(TranscriptTile)),
        c,
        's',
        'javascript:alert(1)',
      );
      await tester.pump();
      expect(browser.opened, 'https://example.com/docs');
      expect(find.text('手机暂不支持此链接类型'), findsOneWidget);
    },
  );
  testWidgets('Missing image gives readable error and retries after recovery', (
    tester,
  ) async {
    final c = AttachmentClient()..unavailable = true;
    addTearDown(c.dispose);
    final bytes = (await tester.runAsync(
      () => File('test/fixtures/output.png').readAsBytes(),
    ))!;
    c.output = SessionAttachment(
      name: 'image.png',
      mime: 'image/png',
      bytes: bytes,
    );
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: UiStrings.delegates,
        supportedLocales: UiStrings.supportedLocales,
        locale: AppLanguage.simplifiedChinese.locale,
        home: Scaffold(
          body: EncryptedImage(
            client: c,
            session: 's',
            href: '/workspace/image.png',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.textContaining('文件已不存在或无法读取'), findsOneWidget);
    c.unavailable = false;
    await tester.tap(find.textContaining('点击重试'));
    await tester.pump();
    await tester.runAsync(
      () => precacheImage(
        MemoryImage(bytes),
        tester.element(find.byType(EncryptedImage)),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byType(Image), findsOneWidget);
    expect(find.textContaining('点击重试'), findsNothing);
    expect(tester.takeException(), isNull);
  });
  testWidgets('Relative filename with source line opens local preview', (
    tester,
  ) async {
    final c = AttachmentClient();
    addTearDown(c.dispose);
    c.output = SessionAttachment(
      name: 'README.md',
      mime: 'text/plain',
      line: 3,
      bytes: Uint8List.fromList(utf8.encode('first\nsecond\nthird')),
    );
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: UiStrings.delegates,
        supportedLocales: UiStrings.supportedLocales,
        locale: AppLanguage.simplifiedChinese.locale,
        home: Scaffold(
          body: TranscriptTile(
            client: c,
            session: 's',
            block: TranscriptBlock(
              key: 'm',
              kind: 'message',
              text: '[文件](README.md:3)',
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('文件'));
    await tester.pumpAndSettle();
    expect(c.requestedHref, 'README.md:3');
    expect(find.text('定位到第 3 行'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  testWidgets(
    'Attachment list collects real transcript images and switches to files',
    (tester) async {
      final c = AttachmentClient();
      addTearDown(c.dispose);
      c.transcripts['s'] = Transcript()
        ..add({
          'id': 'm',
          'seq': 1,
          'kind': 'message',
          'role': 'assistant',
          'text': '![截图](/workspace/image.png) [文档](/workspace/README.md)',
          'mode': 'snapshot',
        });
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: SessionAttachmentsPage(client: c, session: 's'),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('图片 1 · 截图'), findsOneWidget);
      expect(find.text('文档'), findsNothing);
      await tester.tap(find.text('文件与链接'));
      await tester.pumpAndSettle();
      expect(find.text('文档'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );
  test(
    'Data images decode locally and non-TLS image URLs are rejected',
    () async {
      final c = AgentClient();
      addTearDown(c.dispose);
      final bytes = File('test/fixtures/output.png').readAsBytesSync();
      final a = await c.attachment(
        's',
        'data:image/png;base64,${base64Encode(bytes)}',
      );
      expect(a.bytes, bytes);
      await expectLater(
        c.attachment('s', 'http://example.com/image.png'),
        throwsFormatException,
      );
    },
  );
}
