import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/main.dart';
import 'package:agentdeck/message_queue.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'dart:async';

class QueueClient extends AgentClient {
  String? action, edited;
  int calls = 0;
  Completer<void>? pending;
  QueueClient() {
    health.transition(ConnectionPhase.live);
    queues['s'] = [
      const QueuedMessage(
        id: 'q',
        text: 'Read attached files',
        state: 'queued',
        origin: 'codex',
        version: 'v1',
        attachments: 2,
      ),
    ];
  }
  @override
  Future<void> changeQueue(
    String session,
    QueuedMessage message,
    String action, {
    String text = '',
  }) async {
    calls++;
    this.action = action;
    edited = text;
    if (pending != null) await pending!.future;
    if (action == 'edit') {
      queues[session] = [
        QueuedMessage(
          id: 'q',
          text: text,
          state: 'queued',
          origin: 'codex',
          version: 'v2',
          attachments: 2,
        ),
      ];
    } else {
      queues[session] = [];
    }
    notifyListeners();
  }

  void publish(List<QueuedMessage> values) {
    queues['s'] = values;
    notifyListeners();
  }
}

Widget app(QueueClient c, {bool running = true}) => MaterialApp(
  localizationsDelegates: UiStrings.delegates,
  supportedLocales: UiStrings.supportedLocales,
  locale: AppLanguage.english.locale,
  home: Scaffold(
    body: Column(
      children: [
        MessageQueue(client: c, session: 's', agent: 'codex', running: running),
        const TextField(key: ValueKey('composer')),
      ],
    ),
  ),
);
void main() {
  testWidgets(
    'queued card is above composer; native pushes update without transcript history',
    (t) async {
      final c = QueueClient();
      addTearDown(c.dispose);
      await t.pumpWidget(app(c));
      await t.pump();
      expect(
        t.getBottomLeft(find.byKey(const ValueKey('message-queue'))).dy,
        lessThanOrEqualTo(
          t.getTopLeft(find.byKey(const ValueKey('composer'))).dy,
        ),
      );
      expect(find.text('Codex queue'), findsOneWidget);
      await t.tap(find.byKey(const ValueKey('queue-menu-q')));
      await t.pumpAndSettle();
      for (final value in ['Steer', 'Edit message', 'Delete message']) {
        expect(find.text(value), findsOneWidget);
      }
      await t.tap(find.text('Steer'));
      await t.pumpAndSettle();
      expect(c.action, 'steer');
      expect(c.calls, 1);
      expect(find.byKey(const ValueKey('message-queue')), findsNothing);
      c.publish([
        const QueuedMessage(
          id: 'new',
          text: 'From desktop',
          state: 'queued',
          origin: 'codex',
          version: 'fresh',
        ),
      ]);
      await t.pump();
      expect(find.text('From desktop'), findsOneWidget);
    },
  );
  testWidgets(
    'edit saves same queue item with attachments; delete removes it',
    (t) async {
      final c = QueueClient();
      addTearDown(c.dispose);
      await t.pumpWidget(app(c));
      await t.pump();
      await t.tap(find.byKey(const ValueKey('queue-menu-q')));
      await t.pumpAndSettle();
      await t.tap(find.text('Edit message'));
      await t.pumpAndSettle();
      await t.enterText(
        find.byKey(const ValueKey('queue-edit-field')),
        'Edited instructions',
      );
      await t.tap(find.text('Save'));
      await t.pumpAndSettle();
      await t.pump(const Duration(milliseconds: 500));
      expect(c.action, 'edit');
      expect(c.edited, 'Edited instructions');
      expect(c.queues['s']!.single.attachments, 2);
      expect(find.text('Edited instructions'), findsOneWidget);
      await t.tap(find.byKey(const ValueKey('queue-menu-q')));
      await t.pumpAndSettle();
      await t.tap(find.text('Delete message'));
      await t.pumpAndSettle();
      expect(c.action, 'delete');
      expect(c.queues['s'], isEmpty);
    },
  );
  testWidgets(
    'uncertain entries cannot steer or edit; idle queue cannot steer',
    (t) async {
      final c = QueueClient();
      addTearDown(c.dispose);
      c.queues['s'] = [
        const QueuedMessage(
          id: 'q',
          text: 'Unconfirmed',
          state: 'unknown',
          origin: 'codex',
          version: 'v',
        ),
      ];
      await t.pumpWidget(app(c));
      await t.tap(find.byKey(const ValueKey('queue-menu-q')));
      await t.pumpAndSettle();
      for (final label in ['Steer', 'Edit message']) {
        final menu = t.widget<PopupMenuItem<String>>(
          find.ancestor(
            of: find.text(label),
            matching: find.byType(PopupMenuItem<String>),
          ),
        );
        expect(menu.enabled, isFalse);
      }
      await t.tap(find.text('Delete message'));
      await t.pumpAndSettle();
      expect(c.action, 'delete');
    },
  );
  testWidgets(
    'rapid repeated steering only submits once while acknowledgment is pending',
    (t) async {
      final c = QueueClient()..pending = Completer<void>();
      addTearDown(c.dispose);
      await t.pumpWidget(app(c));
      await t.tap(find.byKey(const ValueKey('queue-menu-q')));
      await t.pumpAndSettle();
      await t.tap(find.text('Steer'));
      await t.pump(const Duration(milliseconds: 400));
      expect(find.byKey(const ValueKey('queue-menu-q')), findsNothing);
      expect(c.calls, 1);
      c.pending!.complete();
      await t.pumpAndSettle();
    },
  );
  testWidgets('full conversation places queue above InputComposer', (t) async {
    final c = QueueClient();
    addTearDown(c.dispose);
    final ss = AgentSession(
      id: 's',
      agent: 'codex',
      title: 'Fixture',
      source: 'desktop',
      status: 'running',
      managed: true,
      project: '/fixture',
    );
    c.sessions = [ss];
    await t.pumpWidget(
      MaterialApp(
        localizationsDelegates: UiStrings.delegates,
        supportedLocales: UiStrings.supportedLocales,
        locale: AppLanguage.english.locale,
        home: ConversationPage(client: c, session: ss),
      ),
    );
    await t.pump();
    expect(find.text('Read attached files'), findsOneWidget);
    expect(find.text('Queue message'), findsNothing);
    expect(
      t.getBottomLeft(find.byKey(const ValueKey('message-queue'))).dy,
      lessThan(t.getTopLeft(find.byType(TextField)).dy),
    );
    await t.pumpWidget(const SizedBox());
  });
}
