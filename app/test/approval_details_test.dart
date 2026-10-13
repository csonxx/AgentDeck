import 'dart:convert';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class ApprovalClient extends AgentClient {
  final List<Map<String, String>> decisions = [];

  @override
  Future<void> approve(
    String session,
    String approval,
    String decision, {
    String answer = '',
  }) async {
    decisions.add({
      'session': session,
      'approval': approval,
      'decision': decision,
      'answer': answer,
    });
  }
}

Widget approval(ApprovalClient client, String text) => MaterialApp(
  localizationsDelegates: UiStrings.delegates,
  supportedLocales: UiStrings.supportedLocales,
  locale: AppLanguage.simplifiedChinese.locale,
  home: Scaffold(
    body: SingleChildScrollView(
      child: ApprovalTile(
        client: client,
        session: 'original-session',
        block: TranscriptBlock(
          key: 'request:approval',
          kind: 'approval',
          status: 'pending',
          text: text,
        ),
      ),
    ),
  ),
);

void main() {
  final payloads = <String, Map<String, dynamic>>{
    'Codex': {
      'command': ['sh', '-c', 'echo first\necho COMMAND_END'],
      'cwd': '/workspace/project with spaces',
      'reason': 'Run the requested check',
      'additionalPermissions': {
        'fileSystem': {
          'write': ['/workspace/output/FILE_END'],
        },
      },
    },
    'Claude': {
      'tool_name': 'Bash',
      'input': {
        'command': 'echo first\necho COMMAND_END',
        'description': 'Run the requested check',
        'timeout': 120000,
      },
      'blocked_path': '/workspace/output/FILE_END',
    },
    'OpenCode': {
      'permission': 'bash',
      'patterns': ['echo COMMAND_END'],
      'metadata': {'filepath': '/workspace/output/FILE_END'},
    },
    'ACP': {
      'title': 'Run command',
      'rawInput': {
        'command': 'echo first\necho COMMAND_END',
        'cwd': '/workspace/project',
      },
      'locations': [
        {'path': '/workspace/output/FILE_END'},
      ],
    },
  };
  for (final entry in payloads.entries) {
    testWidgets('${entry.key} shows every approval field without approving', (
      tester,
    ) async {
      final client = ApprovalClient();
      addTearDown(client.dispose);
      await tester.pumpWidget(approval(client, jsonEncode(entry.value)));
      await tester.pumpAndSettle();
      expect(find.byType(SelectableText), findsNothing);
      await tester.tap(find.text('展开完整执行内容'));
      await tester.pumpAndSettle();
      final details = tester.widget<SelectableText>(
        find.byType(SelectableText),
      );
      expect(details.data, contains('COMMAND_END'));
      expect(details.data, contains('FILE_END'));
      for (final key in entry.value.keys) {
        expect(details.data, contains('$key:'));
      }
      expect(details.maxLines, isNull);
      expect(client.decisions, isEmpty);
      await tester.tap(find.byIcon(Icons.close));
      await tester.pumpAndSettle();
      expect(find.text('完整执行内容'), findsNothing);
      expect(client.decisions, isEmpty);
      await tester.tap(find.text('允许此次'));
      await tester.pumpAndSettle();
      expect(client.decisions.single['session'], 'original-session');
      expect(client.decisions.single['approval'], 'request');
      expect(client.decisions.single['decision'], 'accept');
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets('long multiline commands can scroll to the end on a phone', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(360, 740);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final client = ApprovalClient();
    addTearDown(client.dispose);
    final command = List.generate(180, (i) => 'echo line_$i').join('\n');
    await tester.pumpWidget(
      approval(
        client,
        jsonEncode({
          'tool_name': 'Bash',
          'input': {'command': command},
          'cwd': '/workspace/project',
          'reason': 'TAIL_REASON',
        }),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('展开完整执行内容'));
    await tester.pumpAndSettle();
    final text = tester.widget<SelectableText>(find.byType(SelectableText));
    expect(text.data, contains('echo line_0\n'));
    expect(text.data, contains('echo line_179\n'));
    expect(text.data, contains('TAIL_REASON'));
    expect(text.data, isNot(contains(r'\necho')));
    final scroll = find.descendant(
      of: find.byType(BottomSheet),
      matching: find.byType(Scrollable),
    );
    final position = tester.state<ScrollableState>(scroll.first).position;
    expect(position.maxScrollExtent, greaterThan(1000));
    await tester.dragFrom(
      tester.getCenter(find.byType(BottomSheet)),
      const Offset(0, -250),
    );
    await tester.pumpAndSettle();
    expect(position.pixels, greaterThan(0));
    position.jumpTo(position.maxScrollExtent);
    await tester.pump();
    expect(position.extentAfter, 0);
    expect(client.decisions, isEmpty);
    expect(tester.takeException(), isNull);
  });

  testWidgets('details preserve question answers and support raw fallback', (
    tester,
  ) async {
    final client = ApprovalClient();
    addTearDown(client.dispose);
    await tester.pumpWidget(
      approval(
        client,
        jsonEncode({
          'questions': [
            {'id': 'choice', 'question': 'Which directory?', 'options': []},
          ],
          'permissions': {
            'write': ['/workspace/output'],
          },
        }),
      ),
    );
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextFormField), '/workspace/output');
    await tester.tap(find.text('展开完整执行内容'));
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(Icons.close));
    await tester.pumpAndSettle();
    expect(find.text('/workspace/output'), findsOneWidget);
    await tester.tap(find.text('提交回答'));
    await tester.pumpAndSettle();
    expect(jsonDecode(client.decisions.single['answer']!), {
      'choice': '/workspace/output',
    });

    const raw = 'Run this complete legacy command\necho RAW_COMMAND_END';
    await tester.pumpWidget(approval(client, raw));
    await tester.pumpAndSettle();
    await tester.tap(find.text('展开完整执行内容'));
    await tester.pumpAndSettle();
    expect(
      tester.widget<SelectableText>(find.byType(SelectableText)).data,
      raw,
    );
    expect(client.decisions, hasLength(1));
    expect(tester.takeException(), isNull);
  });
}
