import 'dart:async';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:video_player/video_player.dart';
import 'language.dart';

bool videoHref(String href) =>
    RegExp(r'\.(mp4|webm)(?:[?#].*)?$', caseSensitive: false).hasMatch(href);

bool audioHref(String href) => RegExp(
  r'\.(mp3|wav|m4a|m4b|aac|flac|ogg|oga|opus|aif|aiff|amr)(?:[?#].*)?$',
  caseSensitive: false,
).hasMatch(href);
bool mediaHref(String href) => videoHref(href) || audioHref(href);

// Native decoders need a URI. Serve decrypted bytes only from RAM on loopback,
// behind an unguessable per-view token. No public media route or plaintext cache.
class MemoryVideoServer {
  final HttpServer server;
  final Uri uri;
  MemoryVideoServer._(this.server, this.uri);
  static Future<MemoryVideoServer> open(Uint8List bytes, String mime) async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final token = List.generate(
      32,
      (_) => Random.secure().nextInt(256).toRadixString(16).padLeft(2, '0'),
    ).join();
    server.listen((request) async {
      try {
        final response = request.response;
        response.headers.set(HttpHeaders.cacheControlHeader, 'no-store');
        if (request.uri.path != '/$token' ||
            !['GET', 'HEAD'].contains(request.method)) {
          response.statusCode = HttpStatus.notFound;
          await response.close();
          return;
        }
        var start = 0, end = bytes.length - 1;
        final range = request.headers.value(HttpHeaders.rangeHeader);
        if (range != null) {
          final match = RegExp(r'^bytes=(\d*)-(\d*)$').firstMatch(range);
          if (match == null || (match[1]!.isEmpty && match[2]!.isEmpty)) {
            response.statusCode = 416;
            await response.close();
            return;
          }
          if (match[1]!.isEmpty) {
            start = max(0, bytes.length - (int.tryParse(match[2]!) ?? 0));
          } else {
            start = int.tryParse(match[1]!) ?? bytes.length;
            if (match[2]!.isNotEmpty) {
              end = min(end, int.tryParse(match[2]!) ?? -1);
            }
          }
          if (start < 0 || start > end || start >= bytes.length) {
            response.statusCode = 416;
            response.headers.set(
              HttpHeaders.contentRangeHeader,
              'bytes */${bytes.length}',
            );
            await response.close();
            return;
          }
          response.statusCode = 206;
          response.headers.set(
            HttpHeaders.contentRangeHeader,
            'bytes $start-$end/${bytes.length}',
          );
        }
        response.headers.contentType = ContentType.parse(mime);
        response.headers.set(HttpHeaders.acceptRangesHeader, 'bytes');
        response.contentLength = end - start + 1;
        if (request.method == 'GET') {
          response.add(Uint8List.sublistView(bytes, start, end + 1));
        }
        await response.close();
      } catch (_) {
        await request.response.close().catchError((Object _) {});
      }
    });
    return MemoryVideoServer._(
      server,
      Uri.parse('http://127.0.0.1:${server.port}/$token'),
    );
  }

  Future<void> close() => server.close(force: true);
}

class VideoAttachment extends StatefulWidget {
  final Uint8List bytes;
  final String mime;
  final bool audio;
  const VideoAttachment({
    super.key,
    required this.bytes,
    required this.mime,
    this.audio = false,
  });
  @override
  State<VideoAttachment> createState() => _VideoAttachmentState();
}

class _VideoAttachmentState extends State<VideoAttachment>
    with WidgetsBindingObserver {
  MemoryVideoServer? server;
  VideoPlayerController? controller;
  bool failed = false;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    unawaited(load());
  }

  Future<void> load() async {
    MemoryVideoServer? local;
    VideoPlayerController? player;
    try {
      local = await MemoryVideoServer.open(widget.bytes, widget.mime);
      if (!mounted) {
        await local.close();
        return;
      }
      server = local;
      player = VideoPlayerController.networkUrl(
        local.uri,
        videoPlayerOptions: VideoPlayerOptions(mixWithOthers: false),
      );
      controller = player;
      player.addListener(() {
        if (mounted && player!.value.hasError && !failed) {
          setState(() => failed = true);
        }
      });
      await player.initialize();
      if (mounted) setState(() {});
    } catch (_) {
      if (mounted) setState(() => failed = true);
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) controller?.pause();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    unawaited(controller?.dispose() ?? Future.value());
    unawaited(server?.close() ?? Future.value());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final player = controller;
    if (failed) {
      return Center(
        child: Text(
          context.tr(widget.audio ? '音频无法播放，请检查格式或重新打开' : '视频无法播放，请检查格式或重新打开'),
        ),
      );
    }
    if (player == null || !player.value.isInitialized) {
      return const Center(child: CircularProgressIndicator());
    }
    return Center(
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (widget.audio)
              Padding(
                padding: const EdgeInsets.all(40),
                child: Icon(
                  Icons.graphic_eq_rounded,
                  size: 72,
                  color: Theme.of(context).colorScheme.primary,
                ),
              )
            else
              AspectRatio(
                aspectRatio: player.value.aspectRatio,
                child: VideoPlayer(player),
              ),
            ValueListenableBuilder<VideoPlayerValue>(
              valueListenable: player,
              builder: (context, value, _) => Column(
                children: [
                  VideoProgressIndicator(
                    player,
                    allowScrubbing: true,
                    padding: const EdgeInsets.all(16),
                  ),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      IconButton(
                        tooltip: context.tr(value.isPlaying ? '暂停' : '播放'),
                        onPressed: () =>
                            value.isPlaying ? player.pause() : player.play(),
                        icon: Icon(
                          value.isPlaying
                              ? Icons.pause_rounded
                              : Icons.play_arrow_rounded,
                        ),
                      ),
                      Text(
                        '${mediaTime(value.position)} / ${mediaTime(value.duration)}',
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

String mediaTime(Duration duration) {
  final seconds = duration.inSeconds;
  final minutes = (seconds ~/ 60).toString().padLeft(2, '0');
  return '$minutes:${(seconds % 60).toString().padLeft(2, '0')}';
}
