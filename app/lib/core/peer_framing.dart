import 'dart:convert';
import 'dart:typed_data';

const peerFragmentSize = 16 << 10;
const peerMaxFrame = 24 << 20;

Iterable<Uint8List> peerFragments(Uint8List bytes) sync* {
  if (bytes.isEmpty || bytes.length > peerMaxFrame) {
    throw const FormatException('Invalid peer frame size');
  }
  for (var offset = 0; offset < bytes.length;) {
    final end = (offset + peerFragmentSize - 8).clamp(0, bytes.length);
    final packet = Uint8List(8 + end - offset);
    ByteData.sublistView(packet)
      ..setUint32(0, bytes.length)
      ..setUint32(4, offset);
    packet.setRange(8, packet.length, bytes, offset);
    yield packet;
    offset = end;
  }
}

class PeerAssembler {
  final int limit;
  final BytesBuilder _bytes = BytesBuilder(copy: false);
  int _total = 0;
  PeerAssembler({this.limit = peerMaxFrame});

  Uint8List? push(Uint8List packet) {
    if (packet.length < 9 || packet.length > peerFragmentSize) {
      throw const FormatException('Invalid peer fragment');
    }
    final view = ByteData.sublistView(packet);
    final total = view.getUint32(0), offset = view.getUint32(4);
    if (total == 0 ||
        total > limit ||
        offset != _bytes.length ||
        offset + packet.length - 8 > total ||
        (offset != 0 && total != _total)) {
      throw const FormatException('Invalid peer fragment sequence');
    }
    if (offset == 0) _total = total;
    _bytes.add(Uint8List.sublistView(packet, 8));
    if (_bytes.length != total) return null;
    final result = _bytes.takeBytes();
    _total = 0;
    return result;
  }
}

/// Merge the short WS/RTC handover window. This only orders ciphertext;
/// AgentClient still authenticates every frame and checks its epoch/sequence.
class PeerSequence {
  int _next = 1, _bytes = 0;
  final Map<int, String> _pending = {};
  // Go's version-1 Box writes epoch/seq before the large ciphertext string.
  // Peek only this bounded prefix; full JSON and AEAD validation happens once
  // in AgentClient. Small noncanonical/test frames retain JSON compatibility.
  static final _header = RegExp(
    r'^\s*\{\s*"epoch"\s*:\s*\d+\s*,\s*"seq"\s*:\s*(\d+)\s*[,}]',
  );

  List<String> accept(String data) {
    if (data.length > peerMaxFrame) {
      throw const FormatException('Peer frame too large');
    }
    final prefix = data.length > 160 ? data.substring(0, 160) : data;
    final match = _header.firstMatch(prefix);
    dynamic sequence;
    if (match != null) {
      sequence = int.tryParse(match.group(1)!);
    } else if (data.length <= 2048) {
      final value = jsonDecode(data);
      sequence = value is Map ? value['seq'] : null;
    }
    if (sequence is! int ||
        sequence < _next ||
        sequence >= _next + 32 ||
        _pending.containsKey(sequence)) {
      throw const FormatException('Peer sequence rejected');
    }
    _bytes += data.length;
    if (_bytes > peerMaxFrame) {
      throw const FormatException('Peer reorder window exceeded');
    }
    _pending[sequence] = data;
    final ordered = <String>[];
    while (_pending.containsKey(_next)) {
      final item = _pending.remove(_next++)!;
      _bytes -= item.length;
      ordered.add(item);
    }
    return ordered;
  }
}
