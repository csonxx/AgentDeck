import 'dart:async';
import 'dart:typed_data';
import 'package:agentdeck/main.dart';
import 'package:agentdeck/core/input.dart';
import 'package:agentdeck/input_composer.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/outbox_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'outbox_fixtures.dart';

Future<void> mount(WidgetTester tester, OutboxTestClient c) async {
  await tester.pumpWidget(
    MaterialApp(
      localizationsDelegates: UiStrings.delegates,
      supportedLocales: UiStrings.supportedLocales,
      locale: AppLanguage.simplifiedChinese.locale,
      home: Builder(
        builder: (context) => Scaffold(
          body: TextButton(
            onPressed: () {
              c.watch('a');
              Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (_) =>
                      ConversationPage(client: c, session: c.sessions.first),
                ),
              );
            },
            child: const Text('Open fixture chat'),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('Open fixture chat'));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets(
    'leaving while sending keeps the task and reopening displays its receipt',
    (tester) async {
      final v = MemoryOutgoingVault();
      final c = OutboxTestClient(v)..ack = Completer<void>();
      addTearDown(c.dispose);
      await mount(tester, c);
      await tester.enterText(find.byType(TextField), 'continue after back');
      await tester.pump();
      await tester.tap(find.byTooltip('发送消息'));
      await tester.pump();
      expect(c.calls.length, 1);
      expect(v.outgoing.values.single['text'], 'continue after back');
      Navigator.of(tester.element(find.byType(ConversationPage))).pop();
      await tester.pumpAndSettle();
      expect(c.outbox.tasks.single.running, true);
      c.ack!.complete();
      await tester.pump();
      expect(c.outbox.tasks.single.state, 'accepted');
      await tester.tap(find.text('Open fixture chat'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.text('continue after back'), findsOneWidget);
      expect(c.calls.length, 1);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'leaving during image validation continues upload and sends to original session',
    (tester) async {
      final c = OutboxTestClient(MemoryOutgoingVault())
        ..validation = Completer<void>();
      addTearDown(c.dispose);
      await mount(tester, c);
      final composer = tester.widget<InputComposer>(find.byType(InputComposer));
      composer.controller.text = 'image after back';
      final saved = composer.onSend(const TurnOptions(), [
        DraftFile('a.png', 'image/png', Uint8List.fromList([1, 2, 3])),
      ]);
      await tester.pump();
      expect(await saved, true);
      Navigator.of(tester.element(find.byType(ConversationPage))).pop();
      await tester.pumpAndSettle();
      c.selected = 'another-chat';
      c.validation!.complete();
      await tester.pump();
      expect(c.uploads, 1);
      expect(c.calls.length, 1);
      expect(c.calls.single['payload'], contains('"session":"a"'));
      expect(c.selected, 'another-chat');
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets('failing to save ciphertext leaves the original editable text', (
    tester,
  ) async {
    final v = MemoryOutgoingVault()..failWrite = true;
    final c = OutboxTestClient(v);
    addTearDown(c.dispose);
    await mount(tester, c);
    await tester.enterText(find.byType(TextField), 'not committed');
    await tester.pump();
    await tester.tap(find.byTooltip('发送消息'));
    await tester.pump();
    expect(
      tester.widget<TextField>(find.byType(TextField)).controller!.text,
      'not committed',
    );
    expect(c.calls, isEmpty);
    expect(find.textContaining('发送记录保存失败'), findsOneWidget);
  });
  testWidgets(
    'outbox remains accessible outside the originating conversation',
    (tester) async {
      final c = OutboxTestClient(MemoryOutgoingVault())..online(false);
      addTearDown(c.dispose);
      await c.outbox.enqueue(
        c.sessions.first,
        'retained offline',
        const TurnOptions(),
        [],
        'send',
      );
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.simplifiedChinese.locale,
          home: OutboxPage(client: c),
        ),
      );
      await tester.pump();
      expect(find.text('retained offline'), findsOneWidget);
      expect(find.text('发件箱'), findsOneWidget);
      await c.outbox.remove(c.outbox.tasks.single);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'paired offline composer commits a message without pretending it was delivered',
    (tester) async {
      final v = MemoryOutgoingVault();
      final c = OutboxTestClient(v)..online(false);
      addTearDown(c.dispose);
      await mount(tester, c);
      await tester.enterText(find.byType(TextField), 'offline durable');
      await tester.pump();
      await tester.tap(find.byTooltip('发送消息'));
      await tester.pump();
      expect(c.calls, isEmpty);
      expect(v.outgoing.values.single['text'], 'offline durable');
      expect(find.text('等待连接，恢复后自动重试'), findsOneWidget);
      await c.outbox.remove(c.outbox.tasks.single);
      await tester.pumpWidget(const SizedBox());
    },
  );
}
