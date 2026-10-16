import 'model.dart';

class TranscriptSection {
  final List<TranscriptBlock> blocks;
  final bool activity;
  String? _stableKey;
  TranscriptSection(this.blocks, {this.activity = false});
  String get key => _stableKey ?? blocks.first.key;
  bool contains(String? key) => blocks.any((b) => b.key == key);
}

bool isActivity(TranscriptBlock b) => const [
  'thinking',
  'tool_call',
  'tool_result',
  'file_change',
].contains(b.kind);

List<TranscriptSection> transcriptSections(
  List<TranscriptBlock> blocks, {
  List<TranscriptSection> previous = const [],
}) {
  final sections = <TranscriptSection>[];
  for (final b in blocks) {
    if (isActivity(b)) {
      if (sections.isNotEmpty && sections.last.activity) {
        sections.last.blocks.add(b);
      } else {
        sections.add(TranscriptSection([b], activity: true));
      }
    } else {
      sections.add(TranscriptSection([b]));
    }
  }
  final oldKeys = <String, String>{
    for (final section in previous.where((s) => s.activity))
      for (final block in section.blocks) block.key: section.key,
  };
  final used = <String>{};
  for (final section in sections.where((s) => s.activity)) {
    for (final block in section.blocks) {
      final oldKey = oldKeys[block.key];
      if (oldKey != null && used.add(oldKey)) {
        section._stableKey = oldKey;
        break;
      }
    }
    if (section._stableKey == null && used.contains(section.key)) {
      final base = section.key;
      var suffix = 1;
      while (used.contains('$base/$suffix')) {
        suffix++;
      }
      section._stableKey = '$base/$suffix';
    }
    used.add(section.key);
  }
  return sections;
}

enum InputDeliveryPhase { preparing, submitted, accepted, uncertain }

// A transient presentation receipt; canonical history stays server-owned.
// It is never inserted into the encrypted history cache or automatically resent.
class InputReceipt {
  final String id, text, matchText, baselineUpdated;
  int cursor;
  String? nativeMessageID;
  bool _responseSeen = false;
  InputDeliveryPhase phase = InputDeliveryPhase.preparing;
  InputReceipt({
    required this.id,
    required this.text,
    required this.cursor,
    String? matchText,
    this.baselineUpdated = '',
  }) : matchText = matchText ?? text;

  bool observed(List<TranscriptBlock> blocks) => blocks.any(
    (b) =>
        b.kind == 'message' &&
        b.role == 'user' &&
        b.first > cursor &&
        (b.key.startsWith('$id:') ||
            matchText.isNotEmpty &&
                (cleanMessage(b.text) == cleanMessage(matchText) ||
                    b.text.startsWith('$matchText\n'))),
  );

  void reconcile(List<TranscriptBlock> blocks) {
    if (responded(blocks)) _responseSeen = true;
  }

  bool responded(List<TranscriptBlock> blocks) {
    if (_responseSeen) return true;
    if (phase == InputDeliveryPhase.preparing) return false;
    var cutoff = cursor;
    if (phase == InputDeliveryPhase.submitted) {
      final own = blocks
          .where(
            (b) =>
                b.kind == 'message' &&
                b.role == 'user' &&
                b.first > cursor &&
                b.key.startsWith('${nativeMessageID ?? id}:'),
          )
          .firstOrNull;
      if (own == null) return false;
      cutoff = own.first;
    }
    return blocks.any(
      (b) =>
          b.first > cutoff &&
          (b.kind == 'message' && b.role == 'assistant' || isActivity(b)),
    );
  }

  bool waiting(
    List<TranscriptBlock> blocks, {
    String status = '',
    String updated = '',
  }) =>
      !responded(blocks) &&
      !(phase != InputDeliveryPhase.preparing &&
          observed(blocks) &&
          const [
            'completed',
            'failed',
            'interrupted',
            'error',
          ].contains(status) &&
          updated.isNotEmpty &&
          updated != baselineUpdated) &&
      (phase != InputDeliveryPhase.uncertain || observed(blocks));
}
