import 'dart:async';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/loading_image.dart';
import 'package:agentdeck/main.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class ControlledFrames extends ImageStreamCompleter {
  void frame(ui.Image image) => setImage(ImageInfo(image: image));
  void fail() => reportError(
    context: ErrorDescription('Controlled image decode'),
    exception: StateError('Decode failed'),
    silent: true,
  );
}

class ControlledImage extends ImageProvider<ControlledImage> {
  final streams = <ControlledFrames>[];
  @override
  Future<ControlledImage> obtainKey(ImageConfiguration configuration) =>
      SynchronousFuture(this);
  @override
  ImageStreamCompleter loadImage(
    ControlledImage key,
    ImageDecoderCallback decode,
  ) {
    final stream = ControlledFrames();
    streams.add(stream);
    return stream;
  }
}

class DelayedImageClient extends AgentClient {
  final requests = <Completer<Uint8List>>[];
  @override
  Future<Uint8List> image(String id) {
    final request = Completer<Uint8List>();
    requests.add(request);
    return request.future;
  }
}

Widget screen(Widget child) => MaterialApp(
  localizationsDelegates: UiStrings.delegates,
  supportedLocales: UiStrings.supportedLocales,
  locale: AppLanguage.simplifiedChinese.locale,
  home: Scaffold(body: child),
);

Future<ui.Image> decodedFrame(WidgetTester tester) async =>
    (await tester.runAsync(() async {
      final recorder = ui.PictureRecorder();
      Canvas(recorder).drawRect(const Rect.fromLTWH(0, 0, 64, 48), Paint());
      final picture = recorder.endRecording();
      final image = await picture.toImage(64, 48);
      picture.dispose();
      return image;
    }))!;

void main() {
  testWidgets('spinner stays visible until the first decoded frame', (t) async {
    final provider = ControlledImage();
    await t.pumpWidget(screen(LoadingImage(image: provider)));
    await t.pump(const Duration(seconds: 2));
    expect(find.byKey(const ValueKey('image-loading')), findsOneWidget);
    expect(find.bySemanticsLabel('正在加载图片…'), findsOneWidget);
    expect(t.getSize(find.byKey(const ValueKey('image-loading'))).height, 120);
    provider.streams.single.frame(await decodedFrame(t));
    await t.pump();
    expect(find.byType(CircularProgressIndicator), findsNothing);
    expect(find.byType(RawImage), findsOneWidget);
    expect(t.takeException(), isNull);
  });

  testWidgets('changing source never leaves an old image during loading', (
    t,
  ) async {
    final first = ControlledImage(), second = ControlledImage();
    await t.pumpWidget(screen(LoadingImage(image: first)));
    first.streams.single.frame(await decodedFrame(t));
    await t.pump();
    expect(find.byType(RawImage), findsOneWidget);
    await t.pumpWidget(screen(LoadingImage(image: second)));
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    expect(find.byType(RawImage), findsNothing);
    second.streams.single.frame(await decodedFrame(t));
    await t.pump();
    expect(find.byType(CircularProgressIndicator), findsNothing);
    expect(t.takeException(), isNull);
  });

  testWidgets('decode failure can retry and display a successful frame', (
    t,
  ) async {
    final provider = ControlledImage();
    await t.pumpWidget(screen(LoadingImage(image: provider)));
    provider.streams.single.fail();
    await t.pump();
    expect(find.byType(CircularProgressIndicator), findsNothing);
    expect(find.text('图片无法解码，点击重试'), findsOneWidget);
    await t.tap(find.byKey(const ValueKey('image-retry')));
    await t.pump();
    expect(provider.streams, hasLength(2));
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    provider.streams.last.frame(await decodedFrame(t));
    await t.pump();
    expect(find.byType(RawImage), findsOneWidget);
    expect(find.byKey(const ValueKey('image-retry')), findsNothing);
    expect(t.takeException(), isNull);
  });

  testWidgets('encrypted fetch shows progress and replacing source reloads', (
    t,
  ) async {
    final client = DelayedImageClient();
    addTearDown(client.dispose);
    final bytes = (await t.runAsync(
      () => File('test/fixtures/output.png').readAsBytes(),
    ))!;
    await t.pumpWidget(screen(EncryptedImage(client: client, id: 'first')));
    await t.pump(const Duration(seconds: 1));
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    client.requests.single.complete(bytes);
    await t.pump();
    final image = t.widget<Image>(find.byType(Image));
    await t.runAsync(
      () => precacheImage(image.image, t.element(find.byType(Image))),
    );
    await t.pump();
    expect(find.byType(CircularProgressIndicator), findsNothing);
    expect(find.byType(RawImage), findsOneWidget);
    await t.pumpWidget(screen(EncryptedImage(client: client, id: 'second')));
    await t.pump();
    expect(client.requests, hasLength(2));
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    expect(find.byType(RawImage), findsNothing);
    client.requests.last.completeError(
      const HttpException('Image unavailable'),
    );
    await t.pump();
    expect(find.byType(CircularProgressIndicator), findsNothing);
    await t.tap(find.textContaining('点击重试'));
    await t.pump();
    expect(client.requests, hasLength(3));
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    client.requests.last.complete(bytes);
    await t.pump();
    await t.pump();
    expect(find.byType(RawImage), findsOneWidget);
    expect(t.takeException(), isNull);
  });
}
