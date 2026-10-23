import 'dart:async';
import 'dart:convert';
import 'package:flutter/services.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';
import 'peer_framing.dart';

abstract interface class PeerChannel {
  Stream<String> get messages;
  Future<String> offer();
  Future<void> answer(String sdp);
  Future<void> get ready;
  Future<void> send(String data);
  Future<void> close();
}

typedef PeerFactory = Future<PeerChannel> Function(List<String> stun);

/// Native WebRTC is used only for its reliable ordered data channel. No media
/// track, microphone, camera or third-party STUN service is opened implicitly.
class NativePeerChannel implements PeerChannel {
  final RTCPeerConnection _pc;
  final RTCDataChannel _dc;
  final _events = StreamController<String>();
  final _open = Completer<void>();
  final _gathered = Completer<void>();
  final _assembler = PeerAssembler();
  bool _closed = false;
  Timer? _disconnectTimer;
  NativePeerChannel._(this._pc, this._dc) {
    // Attach a listener now, even if a failed negotiation never awaits ready.
    unawaited(_open.future.catchError((Object _) {}));
    _pc.onIceGatheringState = (state) {
      if (state == RTCIceGatheringState.RTCIceGatheringStateComplete &&
          !_gathered.isCompleted) {
        _gathered.complete();
      }
    };
    _pc.onConnectionState = (state) {
      _disconnectTimer?.cancel();
      if (state == RTCPeerConnectionState.RTCPeerConnectionStateDisconnected) {
        _disconnectTimer = Timer(const Duration(seconds: 3), () {
          if (_pc.connectionState ==
              RTCPeerConnectionState.RTCPeerConnectionStateDisconnected) {
            unawaited(close());
          }
        });
      }
      if (state == RTCPeerConnectionState.RTCPeerConnectionStateFailed ||
          state == RTCPeerConnectionState.RTCPeerConnectionStateClosed) {
        unawaited(close());
      }
    };
    _dc.onDataChannelState = (state) {
      if (state == RTCDataChannelState.RTCDataChannelOpen &&
          !_open.isCompleted) {
        _open.complete();
      } else if (state == RTCDataChannelState.RTCDataChannelClosed) {
        unawaited(close());
      }
    };
    _dc.onMessage = (message) {
      if (_closed) return;
      try {
        if (!message.isBinary) {
          throw const FormatException('Binary peer channel required');
        }
        final frame = _assembler.push(message.binary);
        if (frame != null) _events.add(utf8.decode(frame));
      } catch (_) {
        unawaited(close());
      }
    };
  }

  static Future<PeerChannel> create(List<String> stun) async {
    // Fail before WebRTC creates its asynchronous event listener when invoked
    // outside a Flutter engine (for example the host protocol integration).
    ServicesBinding.instance.defaultBinaryMessenger;
    final pc = await createPeerConnection({
      'iceServers': [
        if (stun.isNotEmpty) {'urls': stun},
      ],
      'iceTransportPolicy': 'all',
    });
    try {
      final dc = await pc.createDataChannel(
        'agentdeck-v1',
        RTCDataChannelInit(),
      );
      return NativePeerChannel._(pc, dc);
    } catch (_) {
      await pc.close();
      rethrow;
    }
  }

  @override
  Stream<String> get messages => _events.stream;
  @override
  Future<void> get ready => _open.future.timeout(const Duration(seconds: 12));

  @override
  Future<String> offer() async {
    final description = await _pc.createOffer({
      'offerToReceiveAudio': false,
      'offerToReceiveVideo': false,
    });
    await _pc.setLocalDescription(description);
    if (_pc.iceGatheringState ==
            RTCIceGatheringState.RTCIceGatheringStateComplete &&
        !_gathered.isCompleted) {
      _gathered.complete();
    }
    await _gathered.future.timeout(const Duration(seconds: 8));
    if (_closed) throw StateError('Peer closed');
    return (await _pc.getLocalDescription())!.sdp!;
  }

  @override
  Future<void> answer(String sdp) async {
    if (sdp.isEmpty || sdp.length > 65536) {
      throw const FormatException('Invalid peer answer');
    }
    await _pc.setRemoteDescription(RTCSessionDescription(sdp, 'answer'));
  }

  @override
  Future<void> send(String data) async {
    final deadline = DateTime.now().add(const Duration(seconds: 10));
    for (final fragment in peerFragments(utf8.encode(data))) {
      while ((_dc.bufferedAmount ?? 0) > 256 << 10) {
        if (_closed || DateTime.now().isAfter(deadline)) {
          throw StateError('Peer send stalled');
        }
        await Future<void>.delayed(const Duration(milliseconds: 10));
        await _dc.getBufferedAmount();
      }
      if (_closed) throw StateError('Peer closed');
      await _dc.send(RTCDataChannelMessage.fromBinary(fragment));
    }
  }

  @override
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    _disconnectTimer?.cancel();
    if (!_open.isCompleted) _open.completeError(StateError('Peer closed'));
    unawaited(_events.close());
    try {
      await _dc.close();
    } catch (_) {}
    try {
      await _pc.close();
    } catch (_) {}
  }
}
