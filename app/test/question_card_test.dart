import 'dart:async';
import 'dart:convert';
import 'package:agentdeck/approval_tile.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/language.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class QuestionClient extends AgentClient {
  final calls = <Map<String, String>>[];
  Completer<void>? pending;
  bool fail = false;
  @override
  Future<void> approve(
    String session,
    String approval,
    String decision, {
    String answer = '',
  }) async {
    calls.add({
      'session': session,
      'approval': approval,
      'decision': decision,
      'answer': answer,
    });
    if (fail) throw StateError('fixture disconnected');
    await pending?.future;
  }
}

Widget view(
  QuestionClient client,
  Map<String, dynamic> params, {
  String status = 'pending',
}) => MaterialApp(
  localizationsDelegates: UiStrings.delegates,
  supportedLocales: UiStrings.supportedLocales,
  locale: AppLanguage.simplifiedChinese.locale,
  home: Scaffold(
    body: SingleChildScrollView(
      child: ApprovalTile(
        client: client,
        session: 'original',
        block: TranscriptBlock(
          key: 'native-id:approval',
          kind: 'approval',
          status: status,
          text: jsonEncode(params),
        ),
      ),
    ),
  ),
);
Map<String, dynamic> question(
  String id, {
  bool multi = false,
  bool custom = true,
}) => {
  'id': id,
  'question': 'Question $id',
  'header': 'Header $id',
  'multiple': multi,
  'custom': custom,
  'options': [
    {'label': 'A$id', 'description': 'Description A$id'},
    {'label': 'B$id', 'description': 'Description B$id'},
  ],
};
void main() {
  testWidgets('Codex async choices preserve native IDs and are human answers', (
    tester,
  ) async {
    final c = QuestionClient();
    addTearDown(c.dispose);
    const id = '["request_user_input_async","ask-1",0]';
    await tester.pumpWidget(
      view(c, {
        'question_kind': 'codex_async',
        'questions': [
          {
            'id': id,
            'question': 'Which project?',
            'isOther': true,
            'options': [
              {'label': 'Project A'},
              {'label': 'Project B'},
            ],
          },
        ],
      }),
    );
    expect(find.text('Which project?'), findsOneWidget);
    expect(find.text('允许此次'), findsNothing);
    expect(find.text('其他回答'), findsOneWidget);
    await tester.tap(find.text('Project B'));
    await tester.pump();
    await tester.ensureVisible(find.text('提交回答'));
    await tester.tap(find.text('提交回答'));
    await tester.pumpAndSettle();
    expect(c.calls.single['session'], 'original');
    expect(c.calls.single['decision'], 'accept');
    expect(jsonDecode(c.calls.single['answer']!), {id: 'Project B'});
    expect(find.text('提问已处理'), findsOneWidget);
  });

  testWidgets('Codex async custom answers are single native answer strings', (
    tester,
  ) async {
    final c = QuestionClient();
    addTearDown(c.dispose);
    const id = '["request_user_input_async","ask-1",0]';
    await tester.pumpWidget(
      view(c, {
        'question_kind': 'codex_async',
        'questions': [
          {
            'id': id,
            'question': 'Which project?',
            'isOther': true,
            'options': [
              {'label': 'Project A'},
            ],
          },
        ],
      }),
    );
    await tester.tap(find.text('Project A'));
    await tester.pump();
    await tester.enterText(find.byType(TextFormField), 'Another project');
    await tester.ensureVisible(find.text('提交回答'));
    await tester.tap(find.text('提交回答'));
    await tester.pumpAndSettle();
    expect(jsonDecode(c.calls.single['answer']!), {id: 'Another project'});
  });

  testWidgets(
    'descriptions, multiple questions and multi-select return native arrays',
    (tester) async {
      final c = QuestionClient();
      addTearDown(c.dispose);
      await tester.pumpWidget(
        view(c, {
          'question_kind': 'opencode',
          'questions': [question('one'), question('two', multi: true)],
        }),
      );
      expect(find.text('允许此次'), findsNothing);
      expect(find.text('Description Aone'), findsOneWidget);
      expect(
        tester.widget<FilledButton>(find.byType(FilledButton)).onPressed,
        isNull,
      );
      await tester.tap(find.text('Aone'));
      await tester.pump();
      await tester.ensureVisible(find.text('Atwo'));
      await tester.tap(find.text('Atwo'));
      await tester.pump();
      await tester.ensureVisible(find.text('Btwo'));
      await tester.tap(find.text('Btwo'));
      await tester.pump();
      await tester.ensureVisible(find.text('提交回答'));
      await tester.tap(find.text('提交回答'));
      await tester.pumpAndSettle();
      expect(c.calls.single['session'], 'original');
      expect(c.calls.single['approval'], 'native-id');
      expect(jsonDecode(c.calls.single['answer']!), {
        'one': 'Aone',
        'two': ['Atwo', 'Btwo'],
      });
      expect(find.text('提问已处理'), findsOneWidget);
      expect(find.text('提交回答'), findsNothing);
    },
  );
  testWidgets(
    'custom and single choice replace each other and drafts survive rebuild',
    (tester) async {
      final c = QuestionClient();
      addTearDown(c.dispose);
      final params = {
        'question_kind': 'codex',
        'questions': [
          {...question('one'), 'isOther': true},
        ],
      };
      await tester.pumpWidget(view(c, params));
      await tester.enterText(find.byType(TextFormField), 'Custom answer');
      await tester.pumpWidget(view(c, params));
      expect(find.text('Custom answer'), findsOneWidget);
      // Virtualized history may unmount a card; an unsent draft stays in memory.
      await tester.pumpWidget(const SizedBox());
      await tester.pumpWidget(view(c, params));
      expect(find.text('Custom answer'), findsOneWidget);
      await tester.tap(find.text('Aone'));
      await tester.pump();
      await tester.pump();
      expect(find.text('Custom answer'), findsNothing);
      await tester.enterText(find.byType(TextFormField), 'Final custom');
      await tester.ensureVisible(find.text('提交回答'));
      await tester.tap(find.text('提交回答'));
      await tester.pumpAndSettle();
      expect(jsonDecode(c.calls.single['answer']!), {'one': 'Final custom'});
    },
  );
  testWidgets(
    'native no-custom, secret input, pending protection and failure keep choices',
    (tester) async {
      final c = QuestionClient();
      addTearDown(c.dispose);
      c.pending = Completer<void>();
      await tester.pumpWidget(
        view(c, {
          'question_kind': 'codex',
          'questions': [question('one', custom: false)],
        }),
      );
      expect(find.byType(TextFormField), findsNothing);
      await tester.tap(find.text('Aone'));
      await tester.pump();
      await tester.pump();
      await tester.tap(find.text('提交回答'));
      await tester.pump();
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
      expect(
        tester.widget<FilledButton>(find.byType(FilledButton)).onPressed,
        isNull,
      );
      expect(c.calls, hasLength(1));
      c.pending!.complete();
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SizedBox());
      c.pending = null;
      c.fail = true;
      await tester.pumpWidget(
        view(c, {
          'question_kind': 'codex',
          'questions': [
            {
              'id': 'secret',
              'question': 'Secret',
              'isSecret': true,
              'options': [],
            },
          ],
        }),
      );
      expect(
        tester.widget<TextField>(find.byType(TextField)).obscureText,
        isTrue,
      );
      await tester.enterText(find.byType(TextFormField), 'fixture secret');
      await tester.tap(find.text('提交回答'));
      await tester.pumpAndSettle();
      expect(find.text('需要你的回答'), findsOneWidget);
      expect(find.text('fixture secret'), findsOneWidget);
      expect(
        tester.widget<FilledButton>(find.byType(FilledButton)).onPressed,
        isNotNull,
      );
    },
  );
  testWidgets('skip submits no implicit selection; expired cards cannot send', (
    tester,
  ) async {
    final c = QuestionClient();
    addTearDown(c.dispose);
    final params = {
      'question_kind': 'codex',
      'questions': [question('one')],
    };
    await tester.pumpWidget(view(c, params));
    expect(c.calls, isEmpty);
    await tester.tap(find.text('跳过回答'));
    await tester.pumpAndSettle();
    expect(c.calls.single['decision'], 'decline');
    expect(jsonDecode(c.calls.single['answer']!), {});
    await tester.pumpWidget(view(c, params, status: 'resolved'));
    expect(find.text('提交回答'), findsNothing);
    expect(find.text('提问已处理'), findsOneWidget);
  });
  testWidgets(
    'ACP presents every native permission choice and preserves explicit choice ID',
    (tester) async {
      final c = QuestionClient();
      addTearDown(c.dispose);
      await tester.pumpWidget(
        view(c, {
          'toolCall': {'title': 'Read fixture'},
          'permission_options': [
            {'optionId': 'once', 'name': 'Once', 'kind': 'allow_once'},
            {'optionId': 'always', 'name': 'Always', 'kind': 'allow_always'},
            {'optionId': 'never', 'name': 'Never', 'kind': 'reject_always'},
          ],
        }),
      );
      expect(find.text('Once'), findsOneWidget);
      expect(find.text('Never'), findsOneWidget);
      expect(c.calls, isEmpty);
      await tester.tap(find.text('Always'));
      await tester.pumpAndSettle();
      expect(c.calls.single['decision'], 'accept');
      expect(jsonDecode(c.calls.single['answer']!), {
        'permission_option': 'always',
      });
    },
  );
}
