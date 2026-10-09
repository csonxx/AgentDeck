import 'package:agentdeck/language.dart';
import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/input.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:agentdeck/input_composer.dart';
import 'package:agentdeck/design.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class InputVault extends DeviceVault {
  Map<String, dynamic> settings = {};
  @override
  Future<Map<String, dynamic>> loadGeneration() async => Map.of(settings);
  @override
  Future<void> saveGeneration(Map<String, dynamic> v) async {
    settings = v;
  }
}

class InputClient extends AgentClient {
  final calls = <Map<String, dynamic>>[];
  int previews = 0;
  Completer<String>? previewGate;
  Completer<Map<String, dynamic>>? capabilitiesGate;
  InputClient() : super(vault: InputVault()) {
    connected = true;
  }
  @override
  Future<dynamic> request(Map<String, dynamic> data) async {
    calls.add(data);
    switch (data['method']) {
      case 'input_capabilities':
        if (capabilitiesGate != null) return capabilitiesGate!.future;
        return {
          'voice': true,
          'files': true,
          'images': data['agent'] != 'workbuddy',
          'model_switch': data['agent'] != 'workbuddy',
        };
      case 'models':
        if (data['agent'] == 'workbuddy') {
          return {
            'voice': true,
            'models': [],
            'detail': 'WorkBuddy 当前接口未提供模型切换',
          };
        }
        return {
          'voice': true,
          'effort_label': data['agent'] == 'opencode' ? 'Variant' : 'Effort',
          'models': [
            {
              'id': 'vision',
              'name': 'Vision',
              'efforts': ['low', 'high'],
              'default_effort': 'low',
              'images': true,
            },
            {'id': 'small', 'name': 'Small', 'efforts': [], 'images': false},
          ],
        };
      case 'upload_begin':
        return {'upload': 'fixture-id'};
      case 'upload_chunk':
        return {
          'offset':
              (data['offset'] as int) +
              base64Decode(data['bytes'] as String).length,
        };
      case 'transcribe':
        return {'text': '检查登录页面'};
      case 'transcribe_preview':
        previews++;
        return {
          'text': previewGate != null
              ? await previewGate!.future
              : previews == 1
              ? '检查登录'
              : '检查登录业面',
        };
      default:
        return null;
    }
  }
}

class FakeInputs implements InputServices {
  final audio = StreamController<Uint8List>();
  bool cancelled = false;
  int starts = 0;
  @override
  Future<List<DraftFile>> pickFiles() async => [
    DraftFile(
      'report.txt',
      'text/plain',
      Uint8List.fromList(utf8.encode('hello')),
    ),
  ];
  @override
  Future<DraftFile?> pickImage(bool camera) async => DraftFile(
    'pixel.png',
    'image/png',
    base64Decode(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4v0z5PwAHOALI/Huk1wAAAABJRU5ErkJggg==',
    ),
  );
  @override
  Future<Stream<Uint8List>> record() async {
    starts++;
    return audio.stream;
  }

  @override
  Future<void> stopRecording() async {}
  @override
  Future<void> cancelRecording() async {
    cancelled = true;
  }

  @override
  Future<void> dispose() async {
    await audio.close();
  }
}

class FakeLocalInputs extends FakeInputs implements LocalVoiceInputs {
  final partial = StreamController<String>();
  Completer<void>? prepareGate;
  String finalText = '检查登录页面';
  int localStarts = 0;
  @override
  Future<void> prepareLocalVoice() async {
    await prepareGate?.future;
  }

  @override
  Future<Stream<String>> startLocalVoice() async {
    localStarts++;
    return partial.stream;
  }

  @override
  Future<String> finishLocalVoice() async => finalText;
  @override
  Future<void> dispose() async {
    await partial.close();
    await super.dispose();
  }
}

Future<TestGesture> pressMic(WidgetTester t) async {
  if (find.byKey(const ValueKey('voice-hold')).evaluate().isEmpty) {
    await t.tap(find.byTooltip('语音输入'));
    await t.pump();
  }
  final gesture = await t.startGesture(
    t.getCenter(find.byKey(const ValueKey('voice-hold'))),
  );
  await t.pump(const Duration(milliseconds: 600));
  for (var i = 0; i < 6; i++) {
    await t.pump(const Duration(milliseconds: 1));
  }
  return gesture;
}

Future<void> mount(
  WidgetTester t,
  InputClient c,
  FakeInputs native,
  TextEditingController controller, {
  bool send = false,
  double width = 420,
  double scale = 1,
  String agent = 'codex',
}) async {
  t.view.physicalSize = Size(width, 900);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  await t.pumpWidget(
    MaterialApp(
      localizationsDelegates: UiStrings.delegates,
      supportedLocales: UiStrings.supportedLocales,
      locale: AppLanguage.simplifiedChinese.locale,
      theme: deckTheme(),
      home: MediaQuery(
        data: MediaQueryData(
          size: Size(width, 900),
          textScaler: TextScaler.linear(scale),
        ),
        child: Scaffold(
          body: Align(
            alignment: Alignment.bottomCenter,
            child: InputComposer(
              client: c,
              session: 'chat',
              agent: agent,
              controller: controller,
              sending: false,
              running: false,
              canSend: true,
              services: native,
              onSend: (options, files) async {
                if (send) {
                  final ids = <String>[];
                  for (final file in files) {
                    ids.add(await c.uploadFile('chat', file));
                  }
                  await c.sendInput('chat', controller.text, options, ids);
                  controller.clear();
                }
                return true;
              },
            ),
          ),
        ),
      ),
    ),
  );
  await t.pumpAndSettle();
}

void main() {
  testWidgets(
    'OpenCode photo selection never waits for remote model discovery',
    (t) async {
      final c = InputClient()
        ..capabilitiesGate = Completer<Map<String, dynamic>>();
      final native = FakeInputs(),
          controller = TextEditingController(text: 'draft');
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(t, c, native, controller, agent: 'opencode');
      await t.tap(find.byTooltip('添加图片或文件'));
      await t.pumpAndSettle();
      for (final title in ['照片', '拍照', '文件']) {
        expect(
          t.widget<ListTile>(find.widgetWithText(ListTile, title)).enabled,
          isTrue,
        );
      }
      expect(find.byType(LinearProgressIndicator), findsNothing);
      await t.tap(find.text('照片'));
      await t.pumpAndSettle();
      expect(find.text('pixel.png'), findsOneWidget);
      expect(controller.text, 'draft');
      expect(c.calls, isEmpty);
      expect(native.starts, 0);
    },
  );
  test(
    'image validation happens before upload and rejects unsupported inputs',
    () async {
      final c = InputClient();
      addTearDown(c.dispose);
      final text = DraftFile(
        'report.txt',
        'text/plain',
        Uint8List.fromList([1]),
      );
      final image = DraftFile(
        'pixel.png',
        'image/png',
        Uint8List.fromList([1]),
      );
      await c.validateAttachments('opencode', 'chat', [text]);
      expect(c.calls, isEmpty);
      await expectLater(
        c.validateAttachments('workbuddy', 'chat', [image]),
        throwsFormatException,
      );
      expect(c.calls.single['method'], 'input_capabilities');
      expect(
        c.calls.where(
          (v) => v['method'] == 'upload_begin' || v['method'] == 'send',
        ),
        isEmpty,
      );
      c.calls.clear();
      await c.validateAttachments('opencode', 'chat', [image]);
      expect(c.calls.single['agent'], 'opencode');
    },
  );
  testWidgets(
    'plus chooses a file locally even when capability RPC would never finish',
    (t) async {
      final c = InputClient()
        ..capabilitiesGate = Completer<Map<String, dynamic>>();
      final native = FakeInputs(),
          controller = TextEditingController(text: '原草稿');
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(t, c, native, controller);
      await t.showKeyboard(find.byType(TextField));
      expect(
        t.widget<TextField>(find.byType(TextField)).focusNode!.hasFocus,
        isTrue,
      );
      await t.tap(find.byTooltip('添加图片或文件'));
      await t.pump(const Duration(milliseconds: 300));
      expect(find.text('添加附件'), findsOneWidget);
      expect(find.text('照片'), findsOneWidget);
      expect(find.text('拍照'), findsOneWidget);
      expect(find.text('文件'), findsOneWidget);
      expect(
        t.widget<TextField>(find.byType(TextField)).focusNode!.hasFocus,
        isFalse,
      );
      expect(
        t.widget<ListTile>(find.widgetWithText(ListTile, '文件')).enabled,
        isTrue,
      );
      expect(find.byType(LinearProgressIndicator), findsNothing);
      expect(
        c.calls.where((v) => v['method'] == 'input_capabilities'),
        isEmpty,
      );
      await t.pumpAndSettle();
      await t.tap(find.text('文件'));
      await t.pumpAndSettle();
      expect(find.text('report.txt'), findsOneWidget);
      expect(controller.text, '原草稿');
      expect(native.starts, 0);
      expect(c.calls.where((v) => v['method'] == 'send'), isEmpty);
    },
  );
  testWidgets(
    'local attachment sheet closes without starting a capability request',
    (t) async {
      final c = InputClient()
        ..capabilitiesGate = Completer<Map<String, dynamic>>();
      final native = FakeInputs(), controller = TextEditingController();
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(t, c, native, controller);
      await t.tap(find.byTooltip('添加图片或文件'));
      await t.pump(const Duration(milliseconds: 300));
      await t.tapAt(const Offset(10, 10));
      await t.pumpAndSettle();
      expect(find.text('添加附件'), findsNothing);
      expect(
        c.calls.where((v) => v['method'] == 'input_capabilities'),
        isEmpty,
      );
      await t.pumpAndSettle();
      expect(find.text('添加附件'), findsNothing);
      expect(
        t
            .widget<IconButton>(find.widgetWithIcon(IconButton, Icons.add))
            .onPressed,
        isNotNull,
      );
      expect(find.text('report.txt'), findsNothing);
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    'mic switches to a wide hold button without recording and keyboard preserves draft',
    (t) async {
      final c = InputClient(), native = FakeLocalInputs();
      final controller = TextEditingController(text: '原草稿');
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(t, c, native, controller, width: 320, scale: 1.8);
      await t.showKeyboard(find.byType(TextField));
      await t.tap(find.byTooltip('语音输入'));
      await t.pumpAndSettle();
      final button = find.byKey(const ValueKey('voice-hold'));
      expect(t.getSize(button).width, greaterThan(270));
      expect(t.getSize(button).height, greaterThanOrEqualTo(52));
      expect(find.text('按住说话'), findsOneWidget);
      expect(native.localStarts, 0);
      expect(
        t.widget<TextField>(find.byType(TextField)).focusNode!.hasFocus,
        isFalse,
      );
      await t.tap(find.byTooltip('键盘输入'));
      await t.pumpAndSettle();
      expect(button, findsNothing);
      expect(t.widget<TextField>(find.byType(TextField)).readOnly, isFalse);
      expect(
        t.widget<TextField>(find.byType(TextField)).focusNode!.hasFocus,
        isTrue,
      );
      expect(controller.text, '原草稿');
      expect(native.localStarts, 0);
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    'send glyph remains visible after typing with the production theme',
    (tester) async {
      final c = InputClient();
      final controller = TextEditingController();
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(tester, c, FakeInputs(), controller);
      final send = find.widgetWithIcon(IconButton, Icons.arrow_upward);
      final glyph = find.descendant(of: send, matching: find.byType(RichText));
      expect(tester.widget<IconButton>(send).onPressed, isNull);
      expect(
        tester.widget<RichText>(glyph).text.style?.color,
        DeckColors.muted,
      );
      await tester.enterText(find.byType(TextField), 'visible send button');
      await tester.pump();
      expect(tester.widget<IconButton>(send).onPressed, isNotNull);
      expect(tester.widget<RichText>(glyph).text.style?.color, Colors.white);
      expect(
        tester.widget<IconButton>(send).style?.backgroundColor?.resolve({}),
        DeckColors.ink,
      );
      expect(tester.takeException(), isNull);
    },
  );
  test('generation settings are isolated between device pairings', () async {
    final c = InputClient();
    addTearDown(c.dispose);
    c.device = {'device': 'fixture-pair-a'};
    await c.saveTurnOptions(
      'same-chat',
      const TurnOptions(model: 'vision', effort: 'high'),
    );
    c.device = {'device': 'fixture-pair-b'};
    expect((await c.loadTurnOptions('same-chat')).inherited, isTrue);
    c.device = {'device': 'fixture-pair-a'};
    expect((await c.loadTurnOptions('same-chat')).effort, 'high');
  });
  testWidgets('model and effort reach next send and persist per chat', (
    t,
  ) async {
    final c = InputClient(),
        native = FakeInputs(),
        controller = TextEditingController();
    addTearDown(c.dispose);
    addTearDown(controller.dispose);
    await mount(t, c, native, controller, send: true);
    await t.tap(find.text('模型 · Effort'));
    await t.pumpAndSettle();
    await t.tap(find.text('Vision'));
    await t.pumpAndSettle();
    await t.tap(find.text('high'));
    await t.pumpAndSettle();
    await t.ensureVisible(find.text('应用到下次发送'));
    await t.tap(find.text('应用到下次发送'));
    await t.pumpAndSettle();
    expect((await c.loadTurnOptions('chat')).effort, 'high');
    expect((await c.loadTurnOptions('other')).inherited, true);
    await t.enterText(find.byType(TextField), 'work');
    await t.pump();
    await t.tap(find.byTooltip('发送消息'));
    await t.pumpAndSettle();
    final request = c.calls.lastWhere((v) => v['method'] == 'send');
    expect(request['model'], 'vision');
    expect(request['effort'], 'high');
    expect(request['text'], 'work');
  });
  testWidgets(
    'attachment-only send uploads image and file, removal sends neither',
    (t) async {
      final c = InputClient(),
          native = FakeInputs(),
          controller = TextEditingController();
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(t, c, native, controller, send: true);
      await t.tap(find.byTooltip('添加图片或文件'));
      await t.pumpAndSettle();
      await t.tap(find.text('照片'));
      await t.pumpAndSettle();
      expect(find.text('pixel.png'), findsOneWidget);
      await t.tap(find.byTooltip('添加图片或文件'));
      await t.pumpAndSettle();
      await t.tap(find.text('文件'));
      await t.pumpAndSettle();
      expect(find.text('report.txt'), findsOneWidget);
      await t.tap(find.byTooltip('移除 report.txt'));
      await t.pumpAndSettle();
      await t.tap(find.byTooltip('发送消息'));
      await t.pumpAndSettle();
      expect(
        c.calls.where((v) => v['method'] == 'upload_begin').single['mime'],
        'image/png',
      );
      expect(c.calls.lastWhere((v) => v['method'] == 'send')['attachments'], [
        'fixture-id',
      ]);
      expect(find.text('pixel.png'), findsNothing);
    },
  );
  testWidgets(
    'client streaming writes while held, release finalizes without server upload',
    (t) async {
      final c = InputClient()..connected = false,
          native = FakeLocalInputs(),
          controller = TextEditingController(text: '请');
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(t, c, native, controller, agent: 'opencode');
      final hold = await pressMic(t);
      native.partial.add('检查登陆');
      await t.pump();
      expect(controller.text, '请 检查登陆');
      native.partial.add('检查登陆页面');
      await t.pump();
      expect(controller.text, '请 检查登陆页面');
      expect(find.text('完成'), findsNothing);
      await hold.up();
      for (var i = 0; i < 12; i++) {
        await t.pump(const Duration(milliseconds: 20));
      }
      expect(find.textContaining('松开结束'), findsNothing);
      expect(find.text('正在收尾…'), findsNothing);
      expect(find.text('正在校正识别结果…'), findsNothing);
      expect(native.localStarts, 1);
      expect(controller.text, '请 检查登录页面');
      expect(
        c.calls,
        isEmpty,
        reason:
            'Offline client ASR needs neither capabilities nor audio upload',
      );
      expect(t.widget<TextField>(find.byType(TextField)).enabled, true);
      await t.tap(find.byType(TextField));
      await t.pumpAndSettle();
      expect(find.byKey(const ValueKey('voice-hold')), findsNothing);
      await t.enterText(find.byType(TextField), '请 检查登录页面并测试');
      expect(controller.text, '请 检查登录页面并测试');
      expect(c.calls, isEmpty);
      await t.pumpWidget(const SizedBox());
      await t.pump();
    },
  );
  testWidgets('sliding up cancels client voice and restores the draft', (
    t,
  ) async {
    final c = InputClient(),
        native = FakeLocalInputs(),
        controller = TextEditingController(text: '原草稿');
    addTearDown(c.dispose);
    addTearDown(controller.dispose);
    await mount(t, c, native, controller);
    final hold = await pressMic(t);
    native.partial.add('临时识别');
    await t.pump();
    expect(controller.text, contains('临时识别'));
    await hold.moveBy(const Offset(0, -90));
    await t.pump();
    expect(find.text('松开取消'), findsOneWidget);
    await hold.up();
    await t.pumpAndSettle();
    native.partial.add('迟到结果');
    await t.pump();
    expect(controller.text, '原草稿');
    expect(native.cancelled, true);
    expect(c.calls, isEmpty);
    await t.pumpWidget(const SizedBox());
    await t.pump();
  });
  testWidgets(
    'release during model preparation never opens the microphone later',
    (t) async {
      final c = InputClient(),
          native = FakeLocalInputs()..prepareGate = Completer<void>(),
          controller = TextEditingController();
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(t, c, native, controller);
      final hold = await pressMic(t);
      await hold.up();
      await t.pump();
      native.prepareGate!.complete();
      await t.pumpAndSettle();
      expect(native.localStarts, 0);
      expect(native.cancelled, true);
      expect(controller.text, isEmpty);
      await t.pumpWidget(const SizedBox());
      await t.pump();
    },
  );
  for (final agent in ['opencode', 'qoder', 'trae', 'workbuddy']) {
    testWidgets(
      '$agent exposes voice and file controls independently of model discovery',
      (t) async {
        final c = InputClient(),
            native = FakeInputs(),
            controller = TextEditingController();
        addTearDown(c.dispose);
        addTearDown(controller.dispose);
        await mount(t, c, native, controller, agent: agent);
        expect(find.byTooltip('添加图片或文件'), findsOneWidget);
        expect(find.byTooltip('语音输入'), findsOneWidget);
        final hold = await pressMic(t);
        for (var i = 0; i < 10; i++) {
          await t.pump(const Duration(milliseconds: 1));
        }
        expect(native.starts, 1);
        expect(c.calls.where((v) => v['method'] == 'models'), isEmpty);
        expect(
          c.calls.firstWhere(
            (v) => v['method'] == 'input_capabilities',
          )['agent'],
          agent,
        );
        native.audio.add(Uint8List(32000));
        await t.pump();
        await hold.up();
        for (var i = 0; i < 30; i++) {
          await t.pump(const Duration(milliseconds: 1));
        }
        expect(controller.text, '检查登录页面');
        expect(c.calls.where((v) => v['method'] == 'send'), isEmpty);
        await t.pumpWidget(const SizedBox());
        await t.pump();
      },
    );
  }
  testWidgets(
    'WorkBuddy explains model and image limitations while retaining file input',
    (t) async {
      final c = InputClient(),
          native = FakeInputs(),
          controller = TextEditingController();
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(t, c, native, controller, agent: 'workbuddy');
      await t.tap(find.text('会话模型'));
      await t.pumpAndSettle();
      expect(find.text('WorkBuddy 当前接口未提供模型切换'), findsOneWidget);
      expect(find.text('Vision'), findsNothing);
      await t.tap(find.text('应用到下次发送'));
      await t.pumpAndSettle();
      await t.tap(find.byTooltip('添加图片或文件'));
      await t.pumpAndSettle();
      expect(
        t.widget<ListTile>(find.widgetWithText(ListTile, '照片')).enabled,
        false,
      );
      await t.tap(find.text('文件'));
      await t.pumpAndSettle();
      expect(find.text('report.txt'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      await t.pump();
    },
  );
  testWidgets('OpenCode offers model variants in its session input', (t) async {
    final c = InputClient(),
        native = FakeInputs(),
        controller = TextEditingController();
    addTearDown(c.dispose);
    addTearDown(controller.dispose);
    await mount(t, c, native, controller, agent: 'opencode');
    await t.tap(find.text('模型 · Variant'));
    await t.pumpAndSettle();
    await t.tap(find.text('Vision'));
    await t.pumpAndSettle();
    await t.scrollUntilVisible(
      find.text('high'),
      150,
      scrollable: find.byType(Scrollable).last,
    );
    await t.pumpAndSettle();
    expect(find.text('Variant'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    await t.pump();
  });
  testWidgets('voice inserts editable text and never automatically sends', (
    t,
  ) async {
    final c = InputClient(),
        native = FakeInputs(),
        controller = TextEditingController(text: '请');
    addTearDown(c.dispose);
    addTearDown(controller.dispose);
    await mount(t, c, native, controller, send: true);
    final hold = await pressMic(t);
    for (var i = 0; i < 8; i++) {
      await t.pump(const Duration(milliseconds: 1));
    }
    await t.pump();
    await t.pump();
    native.audio.add(Uint8List(32000));
    await t.pump();
    await hold.up();
    await t.pump();
    await t.pump();
    await t.pump();
    for (var i = 0; i < 20; i++) {
      await t.pump(const Duration(milliseconds: 1));
    }
    expect(
      controller.text,
      contains('检查登录页面'),
      reason:
          'starts=${native.starts} calls=${c.calls.map((v) => v["method"]).toList()} notices=${t.widgetList<Text>(find.byType(Text)).map((v) => v.data).toList()}',
    );
    expect(c.calls.where((v) => v['method'] == 'send'), isEmpty);
    final chunk = c.calls.firstWhere((v) => v['method'] == 'upload_chunk');
    expect(
      utf8.decode(base64Decode(chunk['bytes'] as String).sublist(0, 4)),
      'RIFF',
    );
    await t.pumpWidget(const SizedBox());
    await t.pump();
  });
  testWidgets(
    'live voice replaces hypotheses then corrects without duplicate text',
    (t) async {
      final c = InputClient(),
          native = FakeInputs(),
          controller = TextEditingController(text: '请');
      controller.selection = const TextSelection.collapsed(offset: 1);
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(t, c, native, controller, send: true);
      final hold = await pressMic(t);
      for (var i = 0; i < 10; i++) {
        await t.pump(const Duration(milliseconds: 1));
      }
      native.audio.add(Uint8List(64000));
      await t.pump();
      await t.pump(const Duration(seconds: 2));
      for (var i = 0; i < 20; i++) {
        await t.pump(const Duration(milliseconds: 1));
      }
      expect(controller.text, '请 检查登录');
      native.audio.add(Uint8List(64000));
      await t.pump();
      await t.pump(const Duration(seconds: 2));
      for (var i = 0; i < 20; i++) {
        await t.pump(const Duration(milliseconds: 1));
      }
      expect(controller.text, '请 检查登录业面');
      await hold.up();
      for (var i = 0; i < 30; i++) {
        await t.pump(const Duration(milliseconds: 1));
      }
      expect(controller.text, '请 检查登录页面');
      expect(c.calls.where((v) => v['method'] == 'send'), isEmpty);
      await t.pumpWidget(const SizedBox());
      await t.pump();
    },
  );
  testWidgets(
    'live requests stay bounded and cancel restores a displayed hypothesis',
    (t) async {
      final c = InputClient(),
          native = FakeInputs(),
          controller = TextEditingController(text: '原草稿');
      controller.selection = const TextSelection.collapsed(offset: 3);
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(t, c, native, controller);
      final hold = await pressMic(t);
      for (var i = 0; i < 10; i++) {
        await t.pump(const Duration(milliseconds: 1));
      }
      native.audio.add(Uint8List(20 * 32000));
      await t.pump();
      await t.pump(const Duration(seconds: 2));
      for (var i = 0; i < 20; i++) {
        await t.pump(const Duration(milliseconds: 1));
      }
      expect(controller.text, '原草稿 检查登录');
      await t.pump(const Duration(seconds: 2));
      for (var i = 0; i < 20; i++) {
        await t.pump(const Duration(milliseconds: 1));
      }
      expect(controller.text, '原草稿 检查登录检查登录业面');
      final sizes = c.calls
          .where((v) => v['method'] == 'upload_begin')
          .map((v) => v['size'])
          .toList();
      expect(sizes, [
        6 * 32000 + 44,
        6 * 32000 + 44,
      ], reason: 'bounded spans instead of the whole 20-second recording');
      await t.tap(find.byTooltip('取消录音'));
      await hold.up();
      for (var i = 0; i < 10; i++) {
        await t.pump(const Duration(milliseconds: 1));
      }
      expect(controller.text, '原草稿');
      expect(c.calls.where((v) => v['method'] == 'transcribe'), isEmpty);
      await t.pumpWidget(const SizedBox());
      await t.pump();
    },
  );
  testWidgets(
    'cancel restores original draft and ignores late live transcript',
    (t) async {
      final c = InputClient()..previewGate = Completer<String>(),
          native = FakeInputs(),
          controller = TextEditingController(text: '原草稿');
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(t, c, native, controller);
      final hold = await pressMic(t);
      for (var i = 0; i < 10; i++) {
        await t.pump(const Duration(milliseconds: 1));
      }
      native.audio.add(Uint8List(64000));
      await t.pump();
      await t.pump(const Duration(seconds: 4));
      for (var i = 0; i < 20; i++) {
        await t.pump(const Duration(milliseconds: 1));
      }
      expect(
        c.previews,
        1,
        reason: 'one request in flight despite another timer tick',
      );
      await t.tap(find.byTooltip('取消录音'));
      await hold.up();
      await t.pump();
      c.previewGate!.complete('迟到的文字');
      for (var i = 0; i < 20; i++) {
        await t.pump(const Duration(milliseconds: 1));
      }
      expect(controller.text, '原草稿');
      expect(c.calls.where((v) => v['method'] == 'transcribe'), isEmpty);
      await t.pumpWidget(const SizedBox());
      await t.pump();
    },
  );
  testWidgets(
    'background cancels recording; compact layout supports large text',
    (t) async {
      final c = InputClient(),
          native = FakeInputs(),
          controller = TextEditingController();
      addTearDown(c.dispose);
      addTearDown(controller.dispose);
      await mount(t, c, native, controller, width: 320, scale: 2);
      expect(t.takeException(), isNull);
      final hold = await pressMic(t);
      for (var i = 0; i < 8; i++) {
        await t.pump(const Duration(milliseconds: 1));
      }
      await t.pump();
      await t.pump();
      native.audio.add(Uint8List(100));
      await t.pump();
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      await hold.up();
      await t.pump();
      expect(
        native.cancelled,
        true,
        reason:
            'starts=${native.starts} status=${t.widgetList<Text>(find.byType(Text)).map((v) => v.data).toList()}',
      );
      expect(c.calls.where((v) => v['method'] == 'transcribe'), isEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pump();
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    },
  );
  test('WAV size/length and chunk retry preserve byte identity', () async {
    final pcm = Uint8List(32000);
    final wav = recordingWav(pcm);
    expect(wav.length, 32044);
    expect(ByteData.sublistView(wav).getUint32(4, Endian.little), 32036);
    expect(
      () => recordingWav(Uint8List(60 * 32000 + 2)),
      throwsFormatException,
    );
    final c = InputClient();
    addTearDown(c.dispose);
    final file = DraftFile(
      'large.bin',
      'application/octet-stream',
      Uint8List(130000),
    );
    await c.uploadFile('chat', file);
    expect(c.calls.where((v) => v['method'] == 'upload_chunk').length, 2);
    await c.uploadFile('chat', file);
    expect(c.calls.where((v) => v['method'] == 'upload_chunk').length, 2);
  });
}
