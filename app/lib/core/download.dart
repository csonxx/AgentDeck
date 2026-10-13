import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/services.dart';
import 'client.dart';

class DownloadCancelled implements Exception {}

class DownloadControl {
  bool cancelled = false;
  void check() {
    if (cancelled) throw DownloadCancelled();
  }
}

// Only the user's explicit system-picker destination receives plaintext.
// There is no app cache, temporary file or whole-file accumulation.
class DownloadDestination {
  static const channel = MethodChannel('dev.agentdeck/download');
  final String id;
  DownloadDestination(this.id);
  static Future<DownloadDestination?> choose(String name, String mime) async {
    final id = await channel.invokeMethod<String>('choose', {
      'name': name,
      'mime': mime,
    });
    return id == null ? null : DownloadDestination(id);
  }

  Future<void> write(Uint8List bytes) =>
      channel.invokeMethod('write', {'id': id, 'bytes': bytes});
  Future<void> resume(int offset) =>
      channel.invokeMethod('resume', {'id': id, 'offset': offset});
  Future<void> pause() => channel.invokeMethod('pause', {'id': id});
  Future<void> finish() => channel.invokeMethod('finish', {'id': id});
  Future<void> cancel() => channel.invokeMethod('cancel', {'id': id});
  Future<void> open() => channel.invokeMethod('open', {'id': id});
}

extension AttachmentDownloads on AgentClient {
  Future<void> downloadAttachment(
    String session,
    String href, {
    required Future<void> Function(Uint8List) write,
    required DownloadControl control,
    required void Function(int received, int total) progress,
    int expectedSize = -1,
    int startOffset = 0,
    String version = '',
    Future<void> Function(String version, int total)? identity,
    Future<void> Function()? restart,
  }) async {
    var received = startOffset;
    Future<void> consume(Uint8List bytes, int total) async {
      control.check();
      await write(bytes);
      control.check();
      received += bytes.length;
      progress(received, total);
    }

    final uri = Uri.parse(href);
    if (!['https', 'data'].contains(uri.scheme)) {
      await prepareAttachmentTransfer();
      control.check();
    }
    if (uri.scheme == 'https') {
      final http = attachmentHttpClient()
        ..connectionTimeout = const Duration(seconds: 15);
      try {
        var target = uri;
        for (var hop = 0; hop < 6; hop++) {
          control.check();
          if (target.scheme != 'https' ||
              target.host.isEmpty ||
              target.userInfo.isNotEmpty) {
            throw const FormatException('图片链接必须使用 HTTPS');
          }
          final req = await http
              .getUrl(target)
              .timeout(const Duration(seconds: 20));
          req.followRedirects = false;
          if (received > 0 && version.isNotEmpty) {
            req.headers.set('Range', 'bytes=$received-');
            req.headers.set('If-Range', version);
          }
          final res = await req.close().timeout(const Duration(seconds: 20));
          if ([301, 302, 303, 307, 308].contains(res.statusCode)) {
            final location = res.headers.value('location');
            if (location == null) throw const HttpException('Invalid redirect');
            target = target.resolve(location);
            // Close this response/connection rather than draining an unbounded redirect body.
            final socket = await res.detachSocket();
            socket.destroy();
            continue;
          }
          if (res.statusCode != 200 && res.statusCode != 206) {
            throw const HttpException('附件下载失败');
          }
          final tag =
              res.headers.value('etag') ??
              res.headers.value('last-modified') ??
              '';
          if (version.isNotEmpty && tag.isNotEmpty && version != tag) {
            throw const HttpException('附件已变化，请重新下载');
          }
          if (res.statusCode == 200 && received > 0) {
            if (expectedSize >= 0 &&
                res.contentLength >= 0 &&
                expectedSize != res.contentLength) {
              throw const HttpException('附件已变化，请重新下载');
            }
            if (restart == null) throw const HttpException('服务器不支持续传，请重新下载');
            await restart();
            received = 0;
          }
          var total = res.contentLength;
          if (res.statusCode == 206) {
            final range = RegExp(
              r'^bytes (\d+)-(\d+)/(\d+)$',
            ).firstMatch(res.headers.value('content-range') ?? '');
            if (range == null ||
                int.parse(range[1]!) != received ||
                int.parse(range[2]!) + 1 != int.parse(range[3]!)) {
              throw const HttpException('无效续传响应');
            }
            total = int.parse(range[3]!);
          }
          if (identity != null) await identity(tag, total);
          if (expectedSize >= 0 && total >= 0 && expectedSize != total) {
            throw const HttpException('附件已变化，请重新下载');
          }
          await for (final part in res.timeout(const Duration(seconds: 20))) {
            final chunk = Uint8List.fromList(part);
            for (var offset = 0; offset < part.length; offset += 256 << 10) {
              await consume(
                Uint8List.sublistView(
                  chunk,
                  offset,
                  (offset + (256 << 10)).clamp(0, part.length),
                ),
                total,
              );
            }
          }
          if ((total >= 0 && received != total) ||
              (expectedSize >= 0 && received != expectedSize)) {
            throw const HttpException('附件下载失败');
          }
          return;
        }
        throw const HttpException('Too many redirects');
      } finally {
        http.close(force: true);
      }
    }
    if (uri.scheme == 'agentdeck-file') {
      var total = -1;
      do {
        control.check();
        final v = Map<String, dynamic>.from(
          await request({
                'method': 'upload_read',
                'limit': 512 << 10,
                'session': session,
                'upload': uri.path,
                'offset': received,
              }, timeout: const Duration(seconds: 20))
              as Map,
        );
        final chunk = base64Decode(v['bytes'] as String);
        final size = v['size'] as int;
        if (size < 0 ||
            total >= 0 && total != size ||
            chunk.isEmpty ||
            chunk.length > 512 << 10 ||
            v['offset'] != received + chunk.length ||
            received + chunk.length > size) {
          throw const FormatException('无效附件响应');
        }
        if (expectedSize >= 0 && expectedSize != size) {
          throw const HttpException('附件已变化，请重新下载');
        }
        total = size;
        if (identity != null && received == startOffset) {
          await identity(uri.path, total);
        }
        await consume(chunk, total);
      } while (received < total);
      return;
    }
    if (['data', 'agentdeck-media'].contains(uri.scheme)) {
      final bytes = uri.scheme == 'data'
          ? uri.data!.contentAsBytes()
          : (await attachment(session, href)).bytes;
      for (var off = received; off < bytes.length; off += 256 << 10) {
        await consume(
          Uint8List.sublistView(
            bytes,
            off,
            (off + (256 << 10)).clamp(0, bytes.length),
          ),
          bytes.length,
        );
      }
      return;
    }
    if (!connected) throw const HttpException('当前未连接工作站');
    String? token;
    try {
      control.check();
      final meta = Map<String, dynamic>.from(
        await request({
              'method': 'attachment_open',
              'session': session,
              'href': href,
              'offset': received,
              'version': version,
            })
            as Map,
      );
      token = meta['download'] as String;
      final total = meta['size'] as int;
      if (total < 0 || expectedSize >= 0 && expectedSize != total) {
        throw const HttpException('附件已变化，请重新下载');
      }
      final sourceVersion = meta['version'] as String? ?? '';
      if (received > 0 && (version.isEmpty || sourceVersion != version)) {
        throw const HttpException('附件已变化，请重新下载');
      }
      if (identity != null) await identity(sourceVersion, total);
      progress(received, total);
      final chunkSize = (meta['chunk_size'] as int? ?? (256 << 10)).clamp(
        1,
        512 << 10,
      );
      // Pipeline four bounded reads; write in order and commit only written bytes.
      final pending = <int, Future<Map<String, dynamic>>>{};
      var scheduled = received;
      void schedule() {
        while (scheduled < total && pending.length < 4) {
          final offset = scheduled;
          pending[offset] = request(
            {'method': 'attachment_read', 'download': token, 'offset': offset},
            timeout: const Duration(seconds: 30),
          ).then((v) => Map<String, dynamic>.from(v as Map));
          // Consume failures even when cancellation leaves prefetched replies unused.
          pending[offset]!.ignore();
          scheduled += chunkSize;
        }
      }

      schedule();
      while (received < total) {
        control.check();
        final v = await pending.remove(received)!;
        final bytes = base64Decode(v['bytes'] as String);
        if (bytes.isEmpty ||
            bytes.length > chunkSize ||
            v['size'] != total ||
            v['offset'] != received + bytes.length ||
            received + bytes.length > total ||
            bytes.length != (total - received).clamp(0, chunkSize)) {
          throw const FormatException('无效附件响应');
        }
        await consume(bytes, total);
        schedule();
      }
    } finally {
      if (token != null && connected) {
        try {
          await request({
            'method': 'attachment_close',
            'download': token,
          }, timeout: const Duration(seconds: 5));
        } catch (_) {}
      }
    }
  }
}
