// Monotonic timings only. Never retain message bodies, URLs, keys or attachments.
class SessionOpenTiming {
  final String session, request;
  final int started;
  int? sent, arrived, decoding, decoded, applied;
  int? historyRevision;
  int wireBytes = 0;
  bool reported = false;
  SessionOpenTiming(this.session, this.request, this.started);

  void received({
    required int arrival,
    required int decodeStart,
    required int decodeEnd,
    required int applyEnd,
    required int bytes,
    required int revision,
  }) {
    if (applied != null) return;
    arrived = arrival;
    decoding = decodeStart;
    decoded = decodeEnd;
    applied = applyEnd;
    wireBytes = bytes;
    historyRevision = revision;
  }

  Map<String, int>? presented(int stamp, int renderedRevision) {
    if (reported ||
        sent == null ||
        applied == null ||
        renderedRevision < historyRevision!) {
      return null;
    }
    reported = true;
    return {
      'send_ms': sent! - started,
      'round_trip_ms': arrived! - sent!,
      'receive_queue_ms': decoding! - arrived!,
      'decode_ms': decoded! - decoding!,
      'apply_ms': applied! - decoded!,
      'paint_ms': stamp - applied!,
      'total_ms': stamp - started,
      'wire_bytes': wireBytes,
    };
  }
}
