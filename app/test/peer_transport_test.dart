import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'package:agentdeck/core/hybrid_socket.dart';
import 'package:agentdeck/core/peer_channel.dart';
import 'package:agentdeck/core/peer_framing.dart';
import 'package:flutter_test/flutter_test.dart';

class FakePeer implements PeerChannel {
  final events = StreamController<String>();
  final sent = <String>[];
  bool closed = false;
  @override
  Stream<String> get messages => events.stream;
  @override
  Future<String> offer() async => 'synthetic';
  @override
  Future<void> answer(String sdp) async {}
  @override
  Future<void> get ready async {}
  @override
  Future<void> send(String data) async {
    sent.add(data);
  }

  @override
  Future<void> close() async {
    if (closed) return;
    closed = true;
    unawaited(events.close());
  }
}

Future<(HybridSocket, WebSocket)> sockets() async {
  final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
  final accepted = Completer<WebSocket>();
  server.listen((request) async {
    accepted.complete(await WebSocketTransformer.upgrade(request));
  });
  final client = await WebSocket.connect('ws://127.0.0.1:${server.port}');
  final remote = await accepted.future;
  addTearDown(() async {
    await client.close();
    await remote.close();
    await server.close(force: true);
  });
  return (HybridSocket(client), remote);
}

void main() {
  test(
    'native transport without a Flutter engine fails without leaking events',
    () async {
      await expectLater(NativePeerChannel.create([]), throwsA(anything));
      await Future<void>.delayed(Duration.zero);
    },
  );
  test('large frames fragment, round trip and reject malformed offsets', () {
    final data = Uint8List.fromList(List.generate(800000, (i) => i % 256));
    final chunks = peerFragments(data).toList();
    expect(chunks.length, greaterThan(1));
    final assembler = PeerAssembler();
    Uint8List? result;
    for (final packet in chunks) {
      expect(packet.length, lessThanOrEqualTo(peerFragmentSize));
      result = assembler.push(packet) ?? result;
    }
    expect(result, data);
    expect(() => PeerAssembler().push(chunks[1]), throwsFormatException);
    final invalid = Uint8List(9);
    ByteData.sublistView(invalid).setUint32(0, peerMaxFrame + 1);
    expect(() => PeerAssembler().push(invalid), throwsFormatException);
  });
  test('handover merges ciphertext sequence and rejects replay', () {
    final sequence = PeerSequence();
    expect(sequence.accept('{"seq":1}'), ['{"seq":1}']);
    expect(sequence.accept('{"seq":3}'), isEmpty);
    expect(sequence.accept('{"seq":2}'), ['{"seq":2}', '{"seq":3}']);
    expect(() => sequence.accept('{"seq":3}'), throwsFormatException);
    expect(() => sequence.accept('{"seq":999}'), throwsFormatException);
  });
  test('WebSocket and direct paths share one ordered stream', () async {
    final (socket, remote) = await sockets();
    final peer = FakePeer();
    socket.attach(peer);
    final frames = <String>[];
    final subscription = socket.listen(frames.add);
    remote.add('{"seq":1}');
    peer.events.add('{"seq":3}');
    remote.add('{"seq":2}');
    await Future<void>.delayed(const Duration(milliseconds: 30));
    expect(frames.map((v) => (jsonDecode(v) as Map)['seq']), [1, 2, 3]);
    socket.activate();
    await socket.add('{"seq":1,"ciphertext":"synthetic"}');
    expect(peer.sent.length, 1);
    expect(socket.direct, isTrue);
    await socket.close();
    expect(peer.closed, isTrue);
    await subscription.cancel();
  });
  test('failed direct negotiation keeps relay usable', () async {
    final (socket, remote) = await sockets();
    final peer = FakePeer();
    socket.attach(peer);
    final frames = <String>[];
    final subscription = socket.listen(frames.add);
    await socket.detach();
    expect(peer.closed, isTrue);
    expect(socket.direct, isFalse);
    remote.add('{"seq":1}');
    await Future<void>.delayed(const Duration(milliseconds: 20));
    expect(frames, ['{"seq":1}']);
    await socket.close();
    await subscription.cancel();
  });
  test('active direct loss closes lifeline for safe reconnect', () async {
    final (socket, remote) = await sockets();
    final peer = FakePeer();
    socket.attach(peer);
    socket.activate();
    final done = Completer<void>();
    var failures = 0;
    socket.listen(
      (_) {},
      onError: (Object _) {
        failures++;
      },
      onDone: done.complete,
    );
    await peer.close();
    await done.future.timeout(const Duration(seconds: 2));
    expect(failures, 1);
    await remote.drain<void>().timeout(const Duration(seconds: 2));
    expect(remote.readyState, isNot(WebSocket.open));
  });
  test('gateway lifeline closure closes active direct channel', () async {
    final (socket, remote) = await sockets();
    final peer = FakePeer();
    socket.attach(peer);
    socket.activate();
    final done = Completer<void>();
    socket.listen((_) {}, onDone: done.complete);
    await remote.close(4003, 'pairing revoked');
    await done.future.timeout(const Duration(seconds: 2));
    expect(peer.closed, isTrue);
    expect(socket.closeCode, 4003);
  });
}
