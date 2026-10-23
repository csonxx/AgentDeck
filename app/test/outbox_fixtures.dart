import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/input.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/core/vault.dart';

class MemoryOutgoingVault extends DeviceVault {
  final Map<String, Map<String, dynamic>> outgoing = {}, blobs = {};
  Map<String, dynamic> generation = {};
  bool failWrite = false;
  Completer<void>? commitGate;
  Map<String, dynamic> clone(Map<String, dynamic> v) =>
      Map<String, dynamic>.from(jsonDecode(jsonEncode(v)));
  @override
  Future<List<Map<String, dynamic>>> loadOutgoing() async =>
      outgoing.values.map(clone).toList();
  @override
  Future<void> saveOutgoing(String id, Map<String, dynamic> value) async {
    await commitGate?.future;
    if (failWrite) throw StateError('fixture storage full');
    outgoing[id] = clone(value);
  }

  @override
  Future<void> saveOutgoingFiles(
    String id,
    List<Map<String, dynamic>> files,
  ) async {
    blobs[id] = clone({'files': files});
  }

  @override
  Future<List<Map<String, dynamic>>> loadOutgoingFiles(String id) async =>
      (clone(blobs[id]!)['files'] as List)
          .map((v) => Map<String, dynamic>.from(v))
          .toList();
  @override
  Future<void> removeOutgoingFiles(String id) async {
    blobs.remove(id);
  }

  @override
  Future<void> removeOutgoing(String id) async {
    outgoing.remove(id);
    blobs.remove(id);
  }

  @override
  Future<Map<String, dynamic>> loadGeneration() async => clone(generation);
  @override
  Future<void> saveGeneration(Map<String, dynamic> v) async {
    generation = clone(v);
  }
}

class OutboxTestClient extends AgentClient {
  final Map<String, String> executed;
  final List<Map<String, dynamic>> calls = [];
  Completer<void>? ack, validation;
  Object? failure;
  bool loseAckOnce = false;
  int uploads = 0;
  int preparations = 0;
  OutboxTestClient(MemoryOutgoingVault vault, {Map<String, String>? executed})
    : executed = executed ?? {},
      super(vault: vault) {
    device = {'device': 'paired-fixture', 'url': 'https://fixture'};
    health.transition(ConnectionPhase.live);
    sessions = [
      AgentSession(
        id: 'a',
        agent: 'opencode',
        title: 'Fixture chat',
        source: 'opencode-api',
        status: 'idle',
        project: '/fixture',
        managed: true,
      ),
    ];
    transcripts['a'] = Transcript();
    projects = [AgentProject(id: 'p', name: 'Fixture', path: '/fixture')];
  }
  @override
  Future<AgentSession> prepareOutgoingSession(
    String agent,
    String project,
    String parent,
    String id,
  ) async {
    preparations++;
    return sessions.firstWhere((s) => s.id == parent);
  }

  @override
  Future<void> watch(String id) async {
    selected = id;
    transcripts.putIfAbsent(id, Transcript.new);
    notifyListeners();
  }

  @override
  Future<void> unwatch(String id) async {
    if (selected == id) selected = '';
  }

  @override
  Future<void> validateAttachments(
    String agent,
    String session,
    List<DraftFile> files,
  ) async {
    await validation?.future;
  }

  @override
  Future<String> uploadFile(
    String session,
    DraftFile file, {
    String kind = 'file',
    void Function(int, int)? progress,
  }) async {
    uploads++;
    file.upload = 'uploaded-${file.name}';
    file.uploadedSession = session;
    file.offset = file.bytes.length;
    progress?.call(file.offset, file.bytes.length);
    return file.upload;
  }

  @override
  Future<void> sendDelivery(
    String session,
    String text,
    TurnOptions options,
    List<String> files,
    String delivery, {
    String? requestId,
  }) async {
    final payload = jsonEncode({
      'session': session,
      'text': text,
      'options': options.toJson(),
      'files': files,
      'delivery': delivery,
    });
    calls.add({'id': requestId, 'payload': payload});
    if (failure != null) throw failure!;
    if (executed.containsKey(requestId)) {
      if (executed[requestId] != payload) {
        throw StateError('ID changed payload');
      }
    } else {
      executed[requestId!] = payload;
    }
    await ack?.future;
    if (loseAckOnce) {
      loseAckOnce = false;
      throw const HttpException('fixture ACK lost');
    }
  }

  void online(bool value) {
    health.transition(
      value ? ConnectionPhase.live : ConnectionPhase.reconnecting,
    );
    notifyListeners();
  }
}
