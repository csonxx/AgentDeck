import 'dart:io';
import 'dart:typed_data';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/download.dart';
import 'package:flutter_test/flutter_test.dart';

// Only remaps the test's HTTPS URL to a loopback fixture. Production's factory
// keeps platform TLS validation; certificate behavior has separate wire tests.
class FixtureHttp implements HttpClient {
  final HttpClient inner = HttpClient();
  final int port;
  FixtureHttp(this.port);
  @override
  Future<HttpClientRequest> getUrl(Uri url) => inner.getUrl(
    Uri(scheme: 'http', host: '127.0.0.1', port: port, path: url.path),
  );
  @override
  set connectionTimeout(Duration? value) {
    inner.connectionTimeout = value;
  }

  @override
  void close({bool force = false}) => inner.close(force: force);
  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class HttpDownloadClient extends AgentClient {
  final int port;
  HttpDownloadClient(this.port);
  @override
  HttpClient attachmentHttpClient() => FixtureHttp(port);
}

void main() {
  test(
    'HTTPS resume validates Content-Range/identity and safely restarts ignored ranges',
    () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(() => server.close(force: true));
      final body = List.generate(100, (i) => i);
      var mode = 'range';
      server.listen((request) async {
        final response = request.response;
        expect(request.headers.value('range'), 'bytes=17-');
        expect(request.headers.value('if-range'), '"v1"');
        response.headers.set('etag', mode == 'changed' ? '"v2"' : '"v1"');
        if (mode == 'range' || mode == 'invalid') {
          response.statusCode = 206;
          response.headers.set(
            'content-range',
            mode == 'range' ? 'bytes 17-99/100' : 'bytes 18-99/100',
          );
          response.contentLength = 83;
          response.add(body.sublist(17));
        } else {
          response.contentLength = 100;
          response.add(body);
        }
        await response.close();
      });
      final client = HttpDownloadClient(server.port);
      addTearDown(client.dispose);
      var output = body.sublist(0, 17);
      var restarted = false;
      Future<void> run() => client.downloadAttachment(
        's',
        'https://fixture.invalid/file',
        startOffset: 17,
        expectedSize: 100,
        version: '"v1"',
        control: DownloadControl(),
        restart: () async {
          restarted = true;
          output.clear();
        },
        write: (Uint8List b) async {
          output.addAll(b);
        },
        progress: (_, _) {},
      );
      await run();
      expect(output, body);
      expect(restarted, false);
      mode = 'ignore';
      output = body.sublist(0, 17);
      await run();
      expect(output, body);
      expect(restarted, true);
      for (final invalid in ['changed', 'invalid']) {
        mode = invalid;
        output = body.sublist(0, 17);
        restarted = false;
        await expectLater(run(), throwsA(isA<HttpException>()));
        expect(output, body.sublist(0, 17));
        expect(restarted, false);
      }
    },
  );
}
