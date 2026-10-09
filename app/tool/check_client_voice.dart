// Native, non-private audio accuracy/latency check. Run from the repository:
// dart --packages=app/.dart_tool/package_config.json app/tool/check_client_voice.dart FIXTURES.json
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'package:sherpa_onnx/sherpa_onnx.dart' as asr;
import 'package:agentdeck/core/speech_recognizer.dart';

String normalizeSpeech(String value) =>
    value.toLowerCase().replaceAll(RegExp(r'[^a-z0-9\u4e00-\u9fff]'), '');
int speechErrors(String reference, String actual) {
  final previous = List.generate(actual.length + 1, (i) => i);
  for (var i = 0; i < reference.length; i++) {
    var diagonal = previous[0];
    previous[0] = i + 1;
    for (var j = 0; j < actual.length; j++) {
      final old = previous[j + 1];
      previous[j + 1] = [
        previous[j] + 1,
        old + 1,
        diagonal + (reference[i] == actual[j] ? 0 : 1),
      ].reduce((a, b) => a < b ? a : b);
      diagonal = old;
    }
  }
  return previous.last;
}

Uint8List wavePcm(String path) {
  final wave = asr.readWave(path);
  if (wave.sampleRate != 16000) {
    throw const FormatException('Fixture must be mono 16kHz');
  }
  final bytes = Uint8List(wave.samples.length * 2),
      data = ByteData.sublistView(bytes);
  for (var i = 0; i < wave.samples.length; i++) {
    data.setInt16(
      i * 2,
      (wave.samples[i] * 32768).round().clamp(-32768, 32767),
      Endian.little,
    );
  }
  return bytes;
}

void main(List<String> args) {
  if (args.length != 1) {
    throw ArgumentError('Provide non-private fixtures JSON');
  }
  asr.initBindings();
  final engine = ClientSpeechRecognizer(
    '${Directory.current.path}/app/assets/asr',
  );
  final fixtures = jsonDecode(File(args.single).readAsStringSync()) as List;
  try {
    for (final fixture in fixtures) {
      final pcm = wavePcm(fixture['path'] as String);
      engine.start();
      int? first;
      for (var offset = 0; offset < pcm.length; offset += 3200) {
        final text = engine.accept(
          Uint8List.sublistView(
            pcm,
            offset,
            (offset + 3200).clamp(0, pcm.length),
          ),
        );
        if (text.isNotEmpty) first ??= (offset + 3200) * 1000 ~/ 32000;
      }
      final finish = Stopwatch()..start();
      final finalText = engine.finish();
      final reference = normalizeSpeech(fixture['text'] as String);
      stdout.writeln(
        jsonEncode({
          'id': fixture['id'],
          'kind': fixture['kind'],
          'reference_characters': reference.length,
          'errors': speechErrors(reference, normalizeSpeech(finalText)),
          'first_partial_audio_ms': first,
          'release_compute_ms': finish.elapsedMilliseconds,
          'empty': finalText.isEmpty,
        }),
      );
    }
    // Cancel discards both pending audio and the corrected prefix.
    engine.start();
    engine.accept(wavePcm(fixtures.first['path']));
    engine.cancel();
    engine.start();
    engine.accept(Uint8List(32000));
    if (engine.finish().isNotEmpty) throw StateError('Canceled text leaked');
    engine.start();
    var bounded = false;
    try {
      engine.accept(Uint8List(60 * 32000 + 2));
    } on FormatException {
      bounded = true;
    }
    if (!bounded) throw StateError('Missing recording bound');
    stdout.writeln(
      jsonEncode({'cancel_isolated': true, 'recording_bounded': true}),
    );
  } finally {
    engine.free();
  }
}
