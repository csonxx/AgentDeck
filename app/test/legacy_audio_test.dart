import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/language.dart';
import 'package:agentdeck/media_preview.dart';
import 'package:agentdeck/office_preview.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:video_player/video_player.dart';
import 'package:video_player_platform_interface/video_player_platform_interface.dart';

class AudioPlatform extends VideoPlayerPlatform {
  bool playing = false, disposed = false;
  Duration position = Duration.zero;
  Uri? source;
  @override
  Future<void> init() async {}
  @override
  Future<int?> createWithOptions(VideoCreationOptions options) async {
    source = Uri.parse(options.dataSource.uri!);
    return 1;
  }

  @override
  Stream<VideoEvent> videoEventsFor(int playerId) => Stream.value(
    VideoEvent(
      eventType: VideoEventType.initialized,
      duration: const Duration(seconds: 20),
      size: Size.zero,
    ),
  );
  @override
  Future<void> dispose(int playerId) async {
    disposed = true;
  }

  @override
  Future<void> setLooping(int playerId, bool looping) async {}
  @override
  Future<void> setVolume(int playerId, double volume) async {}
  @override
  Future<void> setMixWithOthers(bool mixWithOthers) async {}
  @override
  Future<void> play(int playerId) async {
    playing = true;
  }

  @override
  Future<void> pause(int playerId) async {
    playing = false;
  }

  @override
  Future<void> seekTo(int playerId, Duration value) async {
    position = value;
  }

  @override
  Future<Duration> getPosition(int playerId) async => position;
  @override
  Future<void> setPlaybackSpeed(int playerId, double speed) async {}
}

Widget localized(Widget child) => MaterialApp(
  localizationsDelegates: UiStrings.delegates,
  supportedLocales: UiStrings.supportedLocales,
  locale: AppLanguage.english.locale,
  home: Scaffold(body: child),
);
void main() {
  test('audio paths and time labels are recognized', () {
    for (final ext in [
      'mp3',
      'wav',
      'm4a',
      'aac',
      'ogg',
      'opus',
      'flac',
      'amr',
    ]) {
      expect(audioHref('/audio/file.$ext?download=1'), isTrue);
    }
    expect(audioHref('/secret.txt'), isFalse);
    expect(mediaTime(const Duration(seconds: 81)), '01:21');
  });
  test('legacy JSON validates types and bounded row dimensions', () {
    Map<String, dynamic> input(Object value) => {
      'name': 'old.xls',
      'mime': 'application/vnd.agentdeck.office+json',
      'bytes': Uint8List.fromList(utf8.encode(jsonEncode(value))),
    };
    final p = parseOffice(
      input({
        'kind': 'excel',
        'sheets': [
          {
            'name': '预算',
            'rows': [
              ['项目', '', '42'],
            ],
            'columns': 3,
            'rowNumbers': [12],
            'truncated': false,
          },
        ],
      }),
    );
    expect(p['sheets'][0]['rowNumbers'], [12]);
    expect(
      () => parseOffice(
        input({
          'kind': 'excel',
          'sheets': [
            {
              'name': 'Bad',
              'rows': [
                ['x'],
              ],
              'columns': 101,
              'rowNumbers': [1],
            },
          ],
        }),
      ),
      throwsFormatException,
    );
    expect(
      () => parseOffice(
        input({
          'kind': 'word',
          'lines': [42],
        }),
      ),
      throwsA(anything),
    );
  });
  testWidgets('legacy Word preview shows paragraphs and truncation notice', (
    tester,
  ) async {
    final bytes = Uint8List.fromList(
      utf8.encode(
        jsonEncode({
          'kind': 'word',
          'lines': ['Legacy heading', '中文正文'],
          'truncated': true,
        }),
      ),
    );
    await tester.pumpWidget(
      localized(
        OfficeAttachment(
          attachment: SessionAttachment(
            name: 'old.doc',
            mime: 'application/vnd.agentdeck.office+json',
            bytes: bytes,
          ),
        ),
      ),
    );
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 150)),
    );
    await tester.pumpAndSettle();
    expect(find.text('中文正文'), findsOneWidget);
    expect(
      find.text('Large document: showing a partial preview.'),
      findsOneWidget,
    );
  });
  testWidgets(
    'audio player has play, pause, seek, lifecycle pause and cleanup',
    (tester) async {
      final old = VideoPlayerPlatform.instance;
      final native = AudioPlatform();
      VideoPlayerPlatform.instance = native;
      addTearDown(() {
        VideoPlayerPlatform.instance = old;
      });
      await tester.pumpWidget(
        localized(
          VideoAttachment(
            bytes: Uint8List.fromList([1, 2, 3]),
            mime: 'audio/wav',
            audio: true,
          ),
        ),
      );
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 100)),
      );
      await tester.pumpAndSettle();
      expect(find.byType(VideoPlayer), findsNothing);
      expect(find.text('00:00 / 00:20'), findsOneWidget);
      expect(native.source!.host, '127.0.0.1');
      await tester.tap(find.byTooltip('Play'));
      await tester.pump();
      expect(native.playing, isTrue);
      await tester.tap(find.byType(VideoProgressIndicator));
      await tester.pump();
      expect(native.position.inSeconds, greaterThan(0));
      await tester.tap(find.byTooltip('Pause'));
      await tester.pump();
      expect(native.playing, isFalse);
      await tester.tap(find.byTooltip('Play'));
      await tester.pump();
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      await tester.pump();
      expect(native.playing, isFalse);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      await tester.pumpWidget(const SizedBox());
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 30)),
      );
      expect(native.disposed, isTrue);
      expect(tester.takeException(), isNull);
    },
  );
}
