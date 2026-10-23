import 'dart:async';
import 'dart:io';
import 'peer_channel.dart';
import 'peer_framing.dart';

/// One logical encrypted connection. The WS lifeline is kept open for Gateway
/// revocation, liveness and fallback; after activation only RTC carries data.
class HybridSocket extends Stream<String> {
  final WebSocket _ws;
  final _events = StreamController<String>();
  final _sequence = PeerSequence();
  PeerChannel? _peer;
  StreamSubscription<String>? _peerEvents;
  Future<void> _writes = Future.value();
  bool _closed = false, _direct = false, _peerEnded = false;
  HybridSocket(this._ws) {
    _ws.listen(
      (data) {
        if (data is! String) {
          _fail();
          return;
        }
        _accept(data);
      },
      onError: (Object _) => _fail(),
      onDone: () => unawaited(close()),
    );
  }
  bool get direct => _direct;
  int? get closeCode => _ws.closeCode;

  void _accept(String data) {
    if (_closed) return;
    try {
      for (final frame in _sequence.accept(data)) {
        _events.add(frame);
      }
    } catch (_) {
      _fail();
    }
  }

  void attach(PeerChannel peer) {
    if (_closed || _peer != null) throw StateError('Peer already attached');
    _peer = peer;
    _peerEnded = false;
    _peerEvents = peer.messages.listen(
      _accept,
      onError: (Object _) {
        _peerEnded = true;
        if (_direct) _fail();
      },
      onDone: () {
        _peerEnded = true;
        if (_direct) _fail();
      },
    );
  }

  void activate() {
    if (_closed || _peer == null || _peerEnded) {
      throw StateError('Peer closed during activation');
    }
    _direct = true;
  }

  Future<void> detach() async {
    if (_direct) {
      await close();
      return;
    }
    final peer = _peer;
    _peer = null;
    await _peerEvents?.cancel();
    _peerEvents = null;
    await peer?.close();
  }

  Future<void> add(String data) {
    final write = _writes.then((_) async {
      if (_closed) throw StateError('Connection closed');
      if (_direct) {
        await _peer!.send(data);
      } else {
        _ws.add(data);
      }
    });
    _writes = write.catchError((Object _) {
      _fail();
    });
    return write;
  }

  void _fail() {
    if (_closed) return;
    _events.addError(const HttpException('Transport interrupted'));
    unawaited(close());
  }

  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    unawaited(_events.close());
    await _peerEvents?.cancel();
    try {
      await _peer?.close();
    } finally {
      await _ws.close();
    }
  }

  @override
  StreamSubscription<String> listen(
    void Function(String)? onData, {
    Function? onError,
    void Function()? onDone,
    bool? cancelOnError,
  }) => _events.stream.listen(
    onData,
    onError: onError,
    onDone: onDone,
    cancelOnError: cancelOnError,
  );
}
