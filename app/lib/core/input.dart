import 'dart:typed_data';

class TurnOptions {
  final String model, effort;
  const TurnOptions({this.model = '', this.effort = ''});
  factory TurnOptions.fromJson(Map<String, dynamic> v) => TurnOptions(
    model: v['model'] as String? ?? '',
    effort: v['effort'] as String? ?? '',
  );
  Map<String, dynamic> toJson() => {'model': model, 'effort': effort};
  bool get inherited => model.isEmpty && effort.isEmpty;
}

class ModelChoice {
  final String id, name, defaultEffort;
  final List<String> efforts;
  final bool images, isDefault;
  ModelChoice.fromJson(Map<String, dynamic> v)
    : id = v['id'] as String,
      name = v['name'] as String,
      defaultEffort = v['default_effort'] as String? ?? '',
      efforts = List<String>.from(v['efforts'] as List? ?? []),
      images = v['images'] == true,
      isDefault = v['default'] == true;
}

class InputCapabilities {
  final bool voice, files, images, modelSwitch;
  final String detail, effortLabel;
  InputCapabilities.fromJson(Map<String, dynamic> v)
    : voice = v['voice'] == true,
      files = v['files'] == true,
      images = v['images'] == true,
      modelSwitch = v['model_switch'] == true,
      detail = v['detail'] as String? ?? '',
      effortLabel = v['effort_label'] as String? ?? 'Effort';
}

class InputCatalog {
  final List<ModelChoice> models;
  final bool voice;
  final String detail, effortLabel;
  InputCatalog.fromJson(Map<String, dynamic> v)
    : models = (v['models'] as List? ?? [])
          .map((m) => ModelChoice.fromJson(Map<String, dynamic>.from(m as Map)))
          .toList(),
      voice = v['voice'] == true,
      detail = v['detail'] as String? ?? '',
      effortLabel = v['effort_label'] as String? ?? 'Effort';
}

class DraftFile {
  final String name, mime;
  final Uint8List bytes;
  String upload = '', uploadedSession = '';
  int offset = 0;
  DraftFile(this.name, this.mime, this.bytes);
  bool get image => mime.startsWith('image/');
  String get sizeLabel => bytes.length < 1024 * 1024
      ? '${(bytes.length / 1024).ceil()} KB'
      : '${(bytes.length / (1024 * 1024)).toStringAsFixed(1)} MB';
}

Uint8List recordingWav(Uint8List pcm) {
  if (pcm.isEmpty || pcm.length.isOdd || pcm.length > 60 * 32000) {
    throw const FormatException('录音为空或超过 60 秒');
  }
  final header = ByteData(44);
  void ascii(int offset, String text) {
    for (var i = 0; i < text.length; i++) {
      header.setUint8(offset + i, text.codeUnitAt(i));
    }
  }

  ascii(0, 'RIFF');
  header.setUint32(4, 36 + pcm.length, Endian.little);
  ascii(8, 'WAVE');
  ascii(12, 'fmt ');
  header.setUint32(16, 16, Endian.little);
  header.setUint16(20, 1, Endian.little);
  header.setUint16(22, 1, Endian.little);
  header.setUint32(24, 16000, Endian.little);
  header.setUint32(28, 32000, Endian.little);
  header.setUint16(32, 2, Endian.little);
  header.setUint16(34, 16, Endian.little);
  ascii(36, 'data');
  header.setUint32(40, pcm.length, Endian.little);
  return (BytesBuilder(copy: false)
        ..add(header.buffer.asUint8List())
        ..add(pcm))
      .takeBytes();
}
