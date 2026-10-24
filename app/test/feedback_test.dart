import 'dart:convert';
import 'dart:io';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/app_update.dart';
import 'package:agentdeck/core/clock.dart';
import 'package:agentdeck/core/diagnostics.dart';
import 'package:agentdeck/core/feedback.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:agentdeck/feedback_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

class FeedbackVault extends DeviceVault {
  Map<String, dynamic> diagnostics = {}, feedback = {};
  @override
  Future<Map<String, dynamic>> loadDiagnostics() async =>
      jsonDecode(jsonEncode(diagnostics));
  @override
  Future<void> saveDiagnostics(Map<String, dynamic> value) async {
    diagnostics = jsonDecode(jsonEncode(value));
  }

  @override
  Future<Map<String, dynamic>> loadFeedback() async =>
      jsonDecode(jsonEncode(feedback));
  @override
  Future<void> saveFeedback(Map<String, dynamic> value) async {
    feedback = jsonDecode(jsonEncode(value));
  }
}

class FeedbackClient extends AgentClient {
  final calls = <Map<String, dynamic>>[];
  FeedbackClient() : super(vault: FeedbackVault()) {
    device = {'device': 'fixture', 'url': 'https://fixture'};
    localFeedback = true;
    health.transition(ConnectionPhase.live);
  }
  @override
  Future<dynamic> request(
    Map<String, dynamic> data, {
    Duration timeout = const Duration(seconds: 70),
  }) async {
    calls.add(data);
    return {'id': data['feedback']['id'], 'stored': 'workstation'};
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'diagnostic journal is bounded, persistent and drops arbitrary fields',
    () async {
      final vault = FeedbackVault();
      final clock = ProtocolClock();
      final log = DiagnosticLog(vault, clock);
      await log.initialize();
      for (var i = 0; i < 550; i++) {
        log.record('send_ack', request: 'a' * 32, duration: i);
      }
      log.record('raw_token_secret', request: 'a' * 32);
      log.record('send_error', request: 'https://secret');
      await log.flush();
      expect(log.records.length, 500);
      expect(jsonEncode(log.records).contains('secret'), false);
      expect(log.records.first['duration_ms'], 50);
      log.dispose();
      await log.flush();
      final restored = DiagnosticLog(vault, clock);
      await restored.initialize();
      expect(restored.records.length, 500);
      restored.dispose();
      await restored.flush();
    },
  );
  test(
    'diagnostic files use authenticated encryption and clear removes them',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final dir = await Directory.systemTemp.createTemp('ad-feedback-');
      addTearDown(() => dir.delete(recursive: true));
      final vault = DeviceVault(directory: dir);
      final v = {
        'records': [
          {'code': 'send_started', 'request_id': 'a' * 32},
        ],
      };
      await vault.saveDiagnostics(v);
      await vault.saveFeedback({'message': 'private fixture feedback'});
      final raw = await File('${dir.path}/diagnostics.vault').readAsString();
      expect(raw.contains('send_started'), false);
      expect(raw.contains('a' * 32), false);
      expect(await vault.loadDiagnostics(), v);
      await vault.clear();
      expect(await vault.loadDiagnostics(), isEmpty);
      expect(await vault.loadFeedback(), isEmpty);
    },
  );
  test(
    'uncertain feedback freezes ID and diagnostics across restart and binding change',
    () async {
      final vault = FeedbackVault(), clock = ProtocolClock();
      final log = DiagnosticLog(vault, clock);
      log.record('send_total', request: 'a' * 32, duration: 21000);
      var target = 'one', fail = true;
      final sent = <String>[];
      Future<dynamic> exchange(Map<String, dynamic> req) async {
        sent.add(jsonEncode(req['feedback']));
        if (fail) throw const SocketException('private address');
        return {'id': req['feedback']['id'], 'stored': 'workstation'};
      }

      FeedbackManager manager() => FeedbackManager(
        vault: vault,
        log: log,
        exchange: exchange,
        binding: () => target,
        connected: () => true,
        supported: () => true,
        version: () async => '0.4.51+9068',
      );
      final first = manager();
      expect(await first.submit('Fixture delayed send', true), false);
      expect(first.error.contains('private'), false);
      expect(first.pending, isNotNull);
      first.dispose();
      log.record('disconnected');
      final second = manager();
      await second.initialize();
      fail = false;
      target = 'two';
      expect(await second.submit('Changed message', false), false);
      expect(sent.length, 1);
      target = 'one';
      expect(await second.submit('Changed message', false), true);
      expect(sent[0], sent[1]);
      expect(second.pending, isNull);
      expect(second.lastID, startsWith('AD-FB-'));
      second.dispose();
      log.dispose();
      await log.flush();
    },
  );
  test(
    'unsupported workstation keeps frozen feedback and diagnostic opt out',
    () async {
      final vault = FeedbackVault(), clock = ProtocolClock();
      final log = DiagnosticLog(vault, clock);
      log.record('send_ack', request: 'a' * 32, duration: 200);
      var calls = 0;
      final f = FeedbackManager(
        vault: vault,
        log: log,
        exchange: (req) async {
          calls++;
          return {};
        },
        binding: () => 'paired',
        connected: () => true,
        supported: () => false,
        version: () async => '0.4.51',
      );
      expect(await f.submit('Fixture', false), false);
      expect(calls, 0);
      expect(f.pending!.containsKey('diagnostics'), false);
      expect(f.error, '请更新本机后端后提交，或复制诊断记录');
      f.dispose();
      log.dispose();
      await log.flush();
    },
  );
  testWidgets('feedback screen submits diagnostic metadata and shows receipt', (
    tester,
  ) async {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          AppUpdater.channel,
          (call) async => {'version': '0.4.51', 'version_code': 9068},
        );
    final c = FeedbackClient();
    c.diagnosticLog.record('send_total', request: 'a' * 32, duration: 21000);
    await tester.pumpWidget(MaterialApp(home: FeedbackPage(client: c)));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const ValueKey('feedback-message')),
      'Fixture delay',
    );
    await tester.ensureVisible(find.byKey(const ValueKey('feedback-submit')));
    await tester.tap(find.byKey(const ValueKey('feedback-submit')));
    await tester.pumpAndSettle();
    expect(c.calls.length, 1);
    expect(c.calls.single['method'], 'feedback_submit');
    await tester.scrollUntilVisible(
      find.byKey(const ValueKey('feedback-receipt')),
      250,
      scrollable: find
          .descendant(
            of: find.byType(ListView),
            matching: find.byType(Scrollable),
          )
          .first,
    );
    expect(find.byKey(const ValueKey('feedback-receipt')), findsOneWidget);
    expect(
      (c.calls.single['feedback']['diagnostics'] as List).single['code'],
      'send_total',
    );
    await tester.pumpWidget(const SizedBox());
    c.dispose();
    await c.diagnosticLog.flush();
  });
}
