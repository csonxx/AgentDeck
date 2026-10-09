import 'dart:typed_data';
import 'package:sherpa_onnx/sherpa_onnx.dart' as asr;

// Shared by the phone worker and native accuracy checks. All utterance audio
// stays in memory, is bounded by the 60-second hold and is discarded on cancel.
class ClientSpeechRecognizer {
  late final asr.OnlineRecognizer _online;
  asr.OfflineRecognizer? _refiner;
  asr.OnlineStream? _stream;
  final _utterance = BytesBuilder(copy: false);
  int _total = 0;
  String _prefix = '';

  ClientSpeechRecognizer(String path) {
    _online = asr.OnlineRecognizer(
      asr.OnlineRecognizerConfig(
        model: asr.OnlineModelConfig(
          transducer: asr.OnlineTransducerModelConfig(
            encoder: '$path/encoder.int8.onnx',
            // Quantizing this decoder caused severe deletion errors in the
            // packaged model. Keep the pinned float32 decoder.
            decoder: '$path/decoder.onnx',
            joiner: '$path/joiner.int8.onnx',
          ),
          tokens: '$path/tokens.txt',
          modelType: 'zipformer',
          provider: 'cpu',
          numThreads: 2,
          debug: false,
        ),
        enableEndpoint: true,
        rule2MinTrailingSilence: 1.2,
        rule3MinUtteranceLength: 20,
      ),
    );
    try {
      _refiner = asr.OfflineRecognizer(
        asr.OfflineRecognizerConfig(
          model: asr.OfflineModelConfig(
            senseVoice: asr.OfflineSenseVoiceModelConfig(
              model: '$path/refiner.int8.onnx',
              language: 'auto',
              useInverseTextNormalization: true,
            ),
            tokens: '$path/refiner.tokens.txt',
            provider: 'cpu',
            numThreads: 2,
            debug: false,
          ),
        ),
      );
    } catch (_) {
      _online.free();
      rethrow;
    }
  }

  void start() {
    cancel();
    _stream = _online.createStream();
  }

  Float32List _samples(Uint8List bytes) {
    final pcm = ByteData.sublistView(bytes);
    final samples = Float32List(bytes.length ~/ 2);
    for (var i = 0; i < samples.length; i++) {
      samples[i] = pcm.getInt16(i * 2, Endian.little) / 32768;
    }
    return samples;
  }

  String _join(String left, String right) {
    if (left.isEmpty) return right;
    if (right.isEmpty) return left;
    // Chinese sentences do not need a space; adjacent Latin words do.
    return RegExp(r'[A-Za-z0-9]$').hasMatch(left) &&
            RegExp(r'^[A-Za-z0-9]').hasMatch(right)
        ? '$left $right'
        : '$left$right';
  }

  String _refine(String provisional) {
    final bytes = _utterance.takeBytes();
    // Do not run an offline model on silence: it can hallucinate text. The
    // streaming speech hypothesis gates correction, including at release.
    if (provisional.isEmpty || bytes.isEmpty) return provisional;
    final stream = _refiner!.createStream();
    try {
      stream.acceptWaveform(samples: _samples(bytes), sampleRate: 16000);
      _refiner!.decode(stream);
      final text = _refiner!.getResult(stream).text.trim();
      return text.isEmpty ? provisional : text;
    } finally {
      stream.free();
    }
  }

  void _decode() {
    while (_online.isReady(_stream!)) {
      _online.decode(_stream!);
    }
  }

  String accept(Uint8List pcm) {
    if (_stream == null) return '';
    if (pcm.length.isOdd || _total + pcm.length > 60 * 32000) {
      throw const FormatException('Audio bound');
    }
    _total += pcm.length;
    _utterance.add(pcm);
    _stream!.acceptWaveform(samples: _samples(pcm), sampleRate: 16000);
    _decode();
    final current = _online.getResult(_stream!).text.trim();
    if (_online.isEndpoint(_stream!)) {
      _prefix = _join(_prefix, _refine(current));
      _online.reset(_stream!);
      return _prefix;
    }
    return _join(_prefix, current);
  }

  String finish() {
    if (_stream == null) return '';
    try {
      _stream!.acceptWaveform(samples: Float32List(8000), sampleRate: 16000);
      _stream!.inputFinished();
      _decode();
      final current = _online.getResult(_stream!).text.trim();
      return _join(_prefix, _refine(current));
    } finally {
      cancel();
    }
  }

  void cancel() {
    _stream?.free();
    _stream = null;
    _utterance.clear();
    _prefix = '';
    _total = 0;
  }

  void free() {
    cancel();
    _online.free();
    _refiner?.free();
  }
}
