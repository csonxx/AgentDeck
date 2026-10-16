import 'dart:async';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:agentdeck/core/display.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:agentdeck/display_setting.dart';
import 'package:agentdeck/agent_settings.dart';
import 'package:agentdeck/input_composer.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'widget_test.dart' show OperableClient, loadScreenshotFonts;

class DisplayVault extends DeviceVault {
  String? saved;
  bool fail = false;
  Completer<String?>? read;
  @override
  Future<String?> loadDisplaySize() async =>
      read == null ? saved : read!.future;
  @override
  Future<void> saveDisplaySize(String value) async {
    if (fail) throw const FileSystemException('synthetic save failure');
    saved = value;
  }
}

Size paintedSize(WidgetTester tester, Finder finder) {
  final box = tester.renderObject<RenderBox>(finder);
  final start = box.localToGlobal(Offset.zero);
  final end = box.localToGlobal(box.size.bottomRight(Offset.zero));
  return Size(end.dx - start.dx, end.dy - start.dy);
}

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  test(
    'size persists in secure storage and unknown values use standard',
    () async {
      final c = DisplayController(DeviceVault());
      await c.load();
      expect(c.size, InterfaceSize.standard);
      await c.select(InterfaceSize.small);
      final restored = DisplayController(DeviceVault());
      await restored.load();
      expect(restored.size, InterfaceSize.small);
      await restored.select(InterfaceSize.standard);
      await c.load();
      expect(
        c.size,
        InterfaceSize.small,
      ); // A user choice wins over later reads.
      expect(InterfaceSize.fromCode('future'), InterfaceSize.standard);
      c.dispose();
      restored.dispose();
    },
  );

  test('late reads and failed saves preserve the chosen size', () async {
    final v = DisplayVault()..read = Completer<String?>();
    final c = DisplayController(v);
    final loading = c.load();
    await c.select(InterfaceSize.small);
    v.read!.complete('standard');
    await loading;
    expect(c.size, InterfaceSize.small);
    v.fail = true;
    await expectLater(
      c.select(InterfaceSize.standard),
      throwsA(isA<FileSystemException>()),
    );
    expect(c.size, InterfaceSize.small);
    v.fail = false;
    await c.select(InterfaceSize.standard);
    expect(c.size, InterfaceSize.standard);
    c.dispose();
  });

  testWidgets(
    'text icons cards and hits scale together while keyboard and safe areas remain physical',
    (tester) async {
      final c = DisplayController(DisplayVault());
      addTearDown(c.dispose);
      var taps = 0;
      MediaQueryData? inner;
      await tester.pumpWidget(
        MaterialApp(
          home: MediaQuery(
            data: const MediaQueryData(
              size: Size(800, 600),
              padding: EdgeInsets.only(top: 24),
              viewPadding: EdgeInsets.only(top: 24, bottom: 20),
              viewInsets: EdgeInsets.only(bottom: 180),
              textScaler: TextScaler.linear(1.5),
            ),
            child: DisplayScale(
              controller: c,
              child: Builder(
                builder: (context) {
                  inner = MediaQuery.of(context);
                  return Scaffold(
                    body: SafeArea(
                      child: Align(
                        alignment: Alignment.topLeft,
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            const Icon(
                              Icons.folder,
                              size: 24,
                              key: ValueKey('sample-icon'),
                            ),
                            const Text(
                              'Synthetic text',
                              style: TextStyle(fontSize: 20),
                              key: ValueKey('sample-text'),
                            ),
                            GestureDetector(
                              onTap: () => taps++,
                              child: Container(
                                key: const ValueKey('sample-card'),
                                width: 120,
                                height: 60,
                                color: Colors.black,
                              ),
                            ),
                            const TextField(key: ValueKey('sample-input')),
                          ],
                        ),
                      ),
                    ),
                  );
                },
              ),
            ),
          ),
        ),
      );
      final icon = paintedSize(
        tester,
        find.byKey(const ValueKey('sample-icon')),
      );
      final text = paintedSize(
        tester,
        find.byKey(const ValueKey('sample-text')),
      );
      await c.select(InterfaceSize.small);
      await tester.pump();
      expect(
        paintedSize(tester, find.byKey(const ValueKey('sample-icon'))).width,
        closeTo(icon.width * .9, .01),
      );
      expect(
        paintedSize(tester, find.byKey(const ValueKey('sample-text'))).height,
        closeTo(text.height * .9, .01),
      );
      expect(
        paintedSize(tester, find.byKey(const ValueKey('sample-card'))),
        const Size(108, 54),
      );
      expect(inner!.padding.top * .9, closeTo(24, .01));
      expect(inner!.viewInsets.bottom * .9, closeTo(180, .01));
      expect(inner!.viewPadding.bottom * .9, closeTo(20, .01));
      expect(inner!.textScaler.scale(20), 30);
      final input = tester.renderObject<RenderBox>(
        find.byKey(const ValueKey('sample-input')),
      );
      expect(
        input.localToGlobal(input.size.bottomRight(Offset.zero)).dy,
        lessThanOrEqualTo(420),
      );
      await tester.tap(find.byKey(const ValueKey('sample-card')));
      expect(taps, 1);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets('settings toggle preserves the open conversation and its draft', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final client = OperableClient();
    final display = DisplayController(DisplayVault());
    final languages = LanguageController(
      client.vault,
      initial: AppLanguage.simplifiedChinese,
    );
    addTearDown(client.dispose);
    addTearDown(display.dispose);
    addTearDown(languages.dispose);
    const boundaryKey = ValueKey('display-preview');
    if (const String.fromEnvironment(
      'AGENTDECK_DISPLAY_PREVIEW_DIR',
    ).isNotEmpty) {
      await tester.runAsync(loadScreenshotFonts);
    }
    await tester.pumpWidget(
      RepaintBoundary(
        key: boundaryKey,
        child: AgentDeckApp(
          client: client,
          languages: languages,
          display: display,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('session-a')));
    await tester.pumpAndSettle();
    expect(find.byType(ConversationPage), findsOneWidget);
    final composer = tester.widget<InputComposer>(find.byType(InputComposer));
    composer.controller.text = 'Synthetic unsent draft';
    final ctx = tester.element(find.byType(ConversationPage));
    Navigator.of(ctx).push(
      MaterialPageRoute<void>(
        builder: (_) => AgentSettingsPage(client: client),
      ),
    );
    await tester.pumpAndSettle();
    Future<void> capture(String name) async {
      const dir = String.fromEnvironment('AGENTDECK_DISPLAY_PREVIEW_DIR');
      if (dir.isEmpty) return;
      final boundary = tester.renderObject<RenderRepaintBoundary>(
        find.byKey(boundaryKey),
      );
      await tester.runAsync(() async {
        final image = await boundary.toImage(pixelRatio: 2);
        final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
        await File('$dir/$name.png').writeAsBytes(bytes!.buffer.asUint8List());
        image.dispose();
      });
    }

    await capture('display-standard');
    await tester.tap(find.byKey(const ValueKey('display-size-setting')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('display-size-small')));
    await tester.pumpAndSettle();
    expect(display.size, InterfaceSize.small);
    await capture('display-small');
    await tester.tap(find.byType(BackButton));
    await tester.pumpAndSettle();
    expect(find.byType(ConversationPage), findsOneWidget);
    expect(
      tester.widget<InputComposer>(find.byType(InputComposer)).controller,
      same(composer.controller),
    );
    expect(composer.controller.text, 'Synthetic unsent draft');
    expect(tester.takeException(), isNull);
  });
}
