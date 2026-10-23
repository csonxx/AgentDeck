// Native-only synthetic acceptance entry point. The normal app never imports it.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:crypto/crypto.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/gateway.dart';
import 'package:agentdeck/core/relay.dart';
import 'package:agentdeck/core/peer_channel.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:agentdeck/core/transport.dart';
import 'package:agentdeck/client_status.dart';

class MemoryVault extends DeviceVault {
  Map<String, dynamic>? value;
  @override
  Future<Map<String, dynamic>?> loadDevice() async => value;
  @override
  Future<void> saveDevice(Map<String, dynamic> v) async {
    value = v;
  }

  @override
  Future<Map<String, dynamic>> readCache() async => {};
  @override
  Future<void> writeCache(Map<String, dynamic> v) async {}
  @override
  Future<void> saveDeviceName(Map<String, dynamic> v) async {}
  @override
  Future<void> clear({bool keepGateway = false}) async {
    value = null;
  }
}

Future<void> until(bool Function() ready, {int seconds = 40}) async {
  final end = DateTime.now().add(Duration(seconds: seconds));
  while (!ready()) {
    if (DateTime.now().isAfter(end)) {
      throw TimeoutException('acceptance stage timeout');
    }
    await Future<void>.delayed(const Duration(milliseconds: 50));
  }
}

void require(bool value) {
  if (!value) throw StateError('acceptance assertion');
}

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final config = Map<String, dynamic>.from(
    jsonDecode(
          utf8.decode(
            base64Decode(const String.fromEnvironment('AD_PEER_FIXTURE')),
          ),
        )
        as Map,
  );
  final origin = config['url'] as String;
  final roots = SecurityContext(withTrustedRoots: false)
    ..setTrustedCertificatesBytes(utf8.encode(config['pem'] as String));
  final http = createTransport('system', '', roots: roots);
  final report = <String, dynamic>{
    'native_android': true,
    'stun_servers': 0,
    'synthetic_only': true,
  };
  Future<Map<String, dynamic>> fixture(
    String path, [
    Map<String, dynamic>? body,
  ]) async {
    final request = await http.openUrl(
      body == null ? 'GET' : 'POST',
      Uri.parse('$origin/fixture/$path'),
    );
    if (body != null) {
      request.headers.contentType = ContentType.json;
      request.write(jsonEncode(body));
    }
    final response = await request.close();
    require(response.statusCode == 200);
    return Map<String, dynamic>.from(
      jsonDecode(await utf8.decoder.bind(response).join()) as Map,
    );
  }

  final gate = Completer<void>();
  PeerChannel? native;
  final c = AgentClient(
    vault: MemoryVault(),
    trustedRoots: roots,
    peerFactory: (stun) async {
      await gate.future;
      native = await NativePeerChannel.create(stun);
      return native!;
    },
  );
  runApp(
    MaterialApp(
      home: Scaffold(
        body: SafeArea(
          child: AnimatedBuilder(
            animation: c,
            builder: (_, _) => Column(
              children: [
                const Text('AgentDeck native P2P acceptance'),
                ClientStatusButton(client: c),
                Text(c.directNote),
              ],
            ),
          ),
        ),
      ),
    ),
  );
  var stage = 'login';
  void progress(String value) {
    stage = value;
    debugPrint('AD_P2P_NATIVE_STAGE $value');
  }

  final gateway = GatewayLogin(origin, trustedRoots: roots);
  try {
    await gateway.begin();
    await fixture('complete', {'flow': gateway.flow});
    require(await gateway.poll());
    final nodes = await gateway.locals();
    require(nodes.length == 1);
    progress('pair');
    final pending = await gateway.offer(nodes.single['id'] as String);
    try {
      await pending.complete(c);
      throw StateError('approval skipped');
    } on ApprovalPending {
      report["approval_required"] = true;
    }
    await fixture('approve', {
      'grant': pending.bundle.relay!['grant'],
      'code': pending.code,
    });
    await pending.complete(c);
    await until(() => c.live);
    require(c.connectionMethod == 'gateway');
    report['initial_gateway'] = true;
    progress('open_before_handover');
    final opened = Map<String, dynamic>.from(
      await c.request({
            'method': 'attachment_open',
            'session': 'peer-session',
            'href': 'large.bin',
          })
          as Map,
    );
    gate.complete();
    progress('direct');
    await until(() => c.connectionMethod == 'webrtc');
    final status = await fixture('transports');
    require(status['webrtc'] == 1);
    require(status['webrtc_with_ip'] == 1);
    report['direct_ip_available'] = true;
    report['direct_verified'] = true;
    await c.request({'method': 'ping'});
    await c.watch('peer-session');
    await until(() => c.transcripts['peer-session']?.blocks.isNotEmpty == true);
    final imageBlock = c.transcripts['peer-session']!.blocks.firstWhere(
      (b) => b.media.isNotEmpty,
    );
    require((await c.image(imageBlock.media)).isNotEmpty);
    report['image_over_direct'] = true;
    progress('direct_download');
    final first = Map<String, dynamic>.from(
      await c.request({
            'method': 'attachment_read',
            'download': opened['download'],
            'offset': 0,
          })
          as Map,
    );
    final bytes = base64Decode(first['bytes'] as String);
    require(bytes.length == 524288);
    report['download_handle_survived_handover'] = true;
    await c.request({
      'method': 'attachment_close',
      'download': opened['download'],
    });
    final device = c.device!['device'];
    progress('fallback');
    await native!.close();
    await until(() => c.live && c.connectionMethod == 'gateway');
    require(c.device!['device'] == device);
    require(c.selected == 'peer-session');
    await c.request({'method': 'ping'});
    report['fallback_verified'] = true;
    progress('resume_download');
    final resumed = Map<String, dynamic>.from(
      await c.request({
            'method': 'attachment_open',
            'session': 'peer-session',
            'href': 'large.bin',
            'offset': bytes.length,
            'version': opened['version'],
          })
          as Map,
    );
    final second = Map<String, dynamic>.from(
      await c.request({
            'method': 'attachment_read',
            'download': resumed['download'],
            'offset': bytes.length,
          })
          as Map,
    );
    require(
      sha256.convert([
            ...bytes,
            ...base64Decode(second['bytes'] as String),
          ]).toString() ==
          config['file_sha256'],
    );
    await c.request({
      'method': 'attachment_close',
      'download': resumed['download'],
    });
    report['download_resumed_after_fallback'] = true;
    progress('revoke');
    await fixture('revoke', {'grant': pending.bundle.relay!['grant']});
    await until(() => !c.live);
    final audit = await fixture('audit');
    require(audit['gateway_has_no_local_content_or_device_roots'] == true);
    report['revocation_verified'] = true;
    report['gateway_opaque'] = true;
    report['passed'] = true;
  } catch (error) {
    report['passed'] = false;
    report['failed_stage'] = stage;
    report['error_type'] = error.runtimeType.toString();
  }
  c.dispose();
  gateway.close();
  try {
    await fixture('native-result', report);
  } finally {
    http.close(force: true);
  }
  debugPrint('AD_P2P_NATIVE_RESULT ${jsonEncode(report)}');
}
