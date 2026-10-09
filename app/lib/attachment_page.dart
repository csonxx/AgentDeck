import 'language.dart';
import 'media_preview.dart';
import 'office_preview.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import 'core/client.dart';
import 'core/model.dart';

Future<void> openSessionLink(
  BuildContext context,
  AgentClient client,
  String session,
  String href, {
  void Function(AgentSession)? onSession,
}) async {
  final raw = href.trim();
  var uri = Uri.tryParse(raw);
  if (uri != null &&
      uri.hasScheme &&
      !raw.contains('://') &&
      ![
        'file',
        'data',
        'codex',
        'agentdeck-media',
        'agentdeck-file',
        'http',
        'https',
        'javascript',
        'intent',
      ].contains(uri.scheme) &&
      RegExp(r':\d+(?::\d+)?(?:#.*)?$').hasMatch(raw)) {
    uri = Uri.tryParse(raw.replaceFirst(RegExp(r':\d+(?::\d+)?(?=$|#)'), ''));
  }
  if (uri == null) return;
  var web = uri;
  if (uri.scheme == 'codex' && uri.host == 'review') {
    web = Uri.tryParse(uri.queryParameters['pr'] ?? '') ?? uri;
  }
  if ((web.scheme == 'https' || web.scheme == 'http') && !mediaHref(web.path)) {
    try {
      if (web.host.isEmpty ||
          !await launchUrl(web, mode: LaunchMode.inAppBrowserView)) {
        throw StateError('Browser unavailable');
      }
    } catch (_) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(context.tr('无法打开网页，请检查浏览器或网络连接'))),
        );
      }
    }
    return;
  }
  if (uri.scheme == 'codex' && uri.host == 'threads') {
    final id = uri.pathSegments.firstOrNull;
    final matches = client.sessions.where((s) => s.id == id);
    if (matches.isNotEmpty && onSession != null) {
      onSession(matches.first);
      return;
    }
  }
  if (uri.scheme.isNotEmpty &&
      uri.scheme != 'file' &&
      uri.scheme != 'data' &&
      uri.scheme != 'agentdeck-file' &&
      uri.scheme != 'agentdeck-media' &&
      !(uri.scheme == 'https' && mediaHref(uri.path))) {
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(context.tr('手机暂不支持此链接类型'))));
    return;
  }
  if (context.mounted) {
    await Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (_) =>
            AttachmentPage(client: client, session: session, href: href),
      ),
    );
  }
}

class AttachmentPage extends StatefulWidget {
  final AgentClient client;
  final String session, href;
  const AttachmentPage({
    super.key,
    required this.client,
    required this.session,
    required this.href,
  });
  @override
  State<AttachmentPage> createState() => _AttachmentPageState();
}

class _AttachmentPageState extends State<AttachmentPage> {
  late Future<SessionAttachment> data = widget.client.attachment(
    widget.session,
    widget.href,
  );
  @override
  Widget build(BuildContext context) => FutureBuilder<SessionAttachment>(
    future: data,
    builder: (context, snap) {
      final attachment = snap.data;
      return Scaffold(
        appBar: AppBar(
          title: Text(
            attachment == null
                ? context.tr('会话附件')
                : (attachment.generatedName
                      ? context.tr(attachment.name)
                      : attachment.name),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
        ),
        body: snap.hasError
            ? Center(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Padding(
                      padding: const EdgeInsets.all(24),
                      child: Text(
                        context.tr(attachmentError(snap.error)),
                        textAlign: TextAlign.center,
                      ),
                    ),
                    TextButton(
                      onPressed: () => setState(() {
                        data = widget.client.attachment(
                          widget.session,
                          widget.href,
                        );
                      }),
                      child: Text(context.tr('重试')),
                    ),
                  ],
                ),
              )
            : attachment == null
            ? const Center(child: CircularProgressIndicator(strokeWidth: 2))
            : attachment.mime.startsWith('audio/')
            ? VideoAttachment(
                bytes: attachment.bytes,
                mime: attachment.mime,
                audio: true,
              )
            : attachment.mime.startsWith('video/')
            ? VideoAttachment(bytes: attachment.bytes, mime: attachment.mime)
            : officeName(attachment.name)
            ? OfficeAttachment(attachment: attachment)
            : attachment.name.toLowerCase().endsWith('.md') &&
                  attachment.line == 0
            ? SingleChildScrollView(
                padding: const EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if (attachment.truncated ||
                        attachment.bytes.length > 128 << 10)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 12),
                        child: Text(context.tr('文件较大，预览前 128 KB')),
                      ),
                    MarkdownBody(
                      data: utf8.decode(
                        Uint8List.sublistView(
                          attachment.bytes,
                          0,
                          attachment.bytes.length.clamp(0, 128 << 10),
                        ),
                        allowMalformed: true,
                      ),
                      selectable: true,
                      onTapLink: (text, href, title) {
                        if (href != null) {
                          openSessionLink(
                            context,
                            widget.client,
                            widget.session,
                            href,
                          );
                        }
                      },
                    ),
                  ],
                ),
              )
            : attachment.isImage
            ? ZoomableImage(bytes: attachment.bytes)
            : attachment.mime.startsWith('text/') ||
                  [
                    'application/json',
                    'application/xml',
                    'application/javascript',
                  ].contains(attachment.mime.split(';').first)
            ? TextAttachment(attachment: attachment)
            : Center(
                child: Padding(
                  padding: const EdgeInsets.all(24),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(Icons.insert_drive_file_outlined, size: 40),
                      const SizedBox(height: 16),
                      Text(
                        attachment.generatedName
                            ? context.tr(attachment.name)
                            : attachment.name,
                        textAlign: TextAlign.center,
                      ),
                      const SizedBox(height: 8),
                      Text(
                        '${attachment.mime} · ${(attachment.bytes.length / 1024).ceil()} KB',
                        style: const TextStyle(
                          color: Colors.grey,
                          fontSize: 12,
                        ),
                      ),
                      const SizedBox(height: 16),
                      Text(
                        context.tr('此格式暂不支持预览，附件可供 agent 读取。'),
                        textAlign: TextAlign.center,
                      ),
                    ],
                  ),
                ),
              ),
      );
    },
  );
}

String attachmentError(Object? error) {
  final message = error.toString();
  // Backend returns fixed safe errors rather than file contents or credentials.
  const known = [
    '文件已不存在或无法读取',
    '文件不在工作站允许的附件目录内',
    '此文件未在当前会话中引用',
    '不提供凭据文件预览',
    '附件不是普通文件或超过 8 MiB',
    '暂不支持此文件格式，请在工作站打开',
    '文档无法预览，文件可能损坏或格式不支持',
    '加密的 Office 文件需要先在工作站解密',
    '当前未连接工作站',
    '图片链接必须使用 HTTPS',
    '图片超过 8 MiB',
  ];
  for (final v in known) {
    if (message.contains(v)) return v;
  }
  return '附件加载失败，请检查工作站连接后重试';
}

class TextAttachment extends StatefulWidget {
  final SessionAttachment attachment;
  const TextAttachment({super.key, required this.attachment});
  @override
  State<TextAttachment> createState() => _TextAttachmentState();
}

class _TextAttachmentState extends State<TextAttachment> {
  ScrollController? scroll;
  late final lines = const LineSplitter().convert(
    utf8.decode(
      Uint8List.sublistView(
        widget.attachment.bytes,
        0,
        widget.attachment.bytes.length.clamp(0, 128 << 10),
      ),
      allowMalformed: true,
    ),
  );
  @override
  void dispose() {
    scroll?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final attachment = widget.attachment;

    final line = attachment.line.clamp(1, lines.isEmpty ? 1 : lines.length);
    final rowHeight = 24 * MediaQuery.textScalerOf(context).scale(1);
    scroll ??= ScrollController(
      initialScrollOffset: ((line - 5).clamp(0, lines.length) * rowHeight)
          .toDouble(),
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (attachment.truncated || attachment.bytes.length > 128 << 10)
          Padding(
            padding: EdgeInsets.all(16),
            child: Text(
              context.tr('文件较大，预览前 128 KB'),
              style: TextStyle(fontSize: 12, color: Colors.grey),
            ),
          ),
        if (attachment.line > 0)
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(context.tr('定位到第 {0} 行', [line])),
          ),
        Expanded(
          child: ListView.builder(
            key: ValueKey('file-${attachment.name}'),
            controller: scroll,
            itemExtent: rowHeight,
            itemCount: lines.length,
            itemBuilder: (_, i) => Container(
              color: i + 1 == attachment.line ? const Color(0xFFFFE9BF) : null,
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Row(
                children: [
                  SizedBox(
                    width: 44,
                    child: Text(
                      '${i + 1}',
                      style: const TextStyle(color: Colors.grey, fontSize: 12),
                    ),
                  ),
                  Expanded(
                    child: SingleChildScrollView(
                      scrollDirection: Axis.horizontal,
                      child: SelectableText(
                        lines[i],
                        style: const TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 13,
                          height: 1.5,
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ],
    );
  }
}

class ZoomableImage extends StatelessWidget {
  final Uint8List bytes;
  const ZoomableImage({super.key, required this.bytes});
  @override
  Widget build(BuildContext context) => ColoredBox(
    color: const Color(0xFF161719),
    child: InteractiveViewer(
      minScale: .5,
      maxScale: 6,
      child: Center(
        child: Image.memory(
          bytes,
          fit: BoxFit.contain,
          errorBuilder: (_, _, _) =>
              Text(context.tr('图片无法解码'), style: TextStyle(color: Colors.white)),
        ),
      ),
    ),
  );
}

class _AttachmentEntry {
  final String label, href;
  final bool image;
  const _AttachmentEntry(this.label, this.href, this.image);
}

class SessionAttachmentsPage extends StatefulWidget {
  final AgentClient client;
  final String session;
  const SessionAttachmentsPage({
    super.key,
    required this.client,
    required this.session,
  });
  @override
  State<SessionAttachmentsPage> createState() => _SessionAttachmentsPageState();
}

class _SessionAttachmentsPageState extends State<SessionAttachmentsPage> {
  bool images = true;
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.client,
    builder: (context, _) {
      final entries = <_AttachmentEntry>[];
      final seen = <String>{};
      final links = RegExp(
        r'''(!?)\[([^\]]*)\]\(\s*(?:<([^>]+)>|([^\s)]+))(?:\s+"[^"]*")?\s*\)''',
      );
      for (final block
          in widget.client.transcripts[widget.session]?.blocks ??
              <TranscriptBlock>[]) {
        if (block.kind == 'image') {
          final href = block.media.isNotEmpty
              ? 'agentdeck-media:${block.media}'
              : block.text;
          if (href.isNotEmpty && seen.add(href)) {
            entries.add(_AttachmentEntry(context.tr('会话图片'), href, true));
          }
        }
        for (final m in links.allMatches(block.text)) {
          final href = m[3] ?? m[4]!;
          if (seen.add(href)) {
            entries.add(
              _AttachmentEntry(
                m[2]!.isEmpty ? context.tr('会话附件') : m[2]!,
                href,
                m[1] == '!' && !mediaHref(href),
              ),
            );
          }
        }
      }
      final visible = entries.where((e) => e.image == images).toList();
      return Scaffold(
        appBar: AppBar(title: Text(context.tr('会话附件'))),
        body: Column(
          children: [
            Padding(
              padding: const EdgeInsets.all(16),
              child: Wrap(
                spacing: 12,
                children: [
                  ChoiceChip(
                    label: Text(context.tr('图片')),
                    selected: images,
                    onSelected: (_) => setState(() => images = true),
                  ),
                  ChoiceChip(
                    label: Text(context.tr('文件与链接')),
                    selected: !images,
                    onSelected: (_) => setState(() => images = false),
                  ),
                ],
              ),
            ),
            Expanded(
              child: visible.isEmpty
                  ? Center(
                      child: Text(
                        images
                            ? context.tr('当前已载入的会话中没有图片')
                            : context.tr('当前已载入的会话中没有文件或链接'),
                      ),
                    )
                  : ListView.builder(
                      itemCount: visible.length,
                      itemBuilder: (context, i) {
                        final e = visible[i];
                        final uri = Uri.tryParse(e.href);
                        final web =
                            uri?.scheme == 'http' || uri?.scheme == 'https';
                        return ListTile(
                          leading: Icon(
                            e.image
                                ? Icons.image_outlined
                                : audioHref(e.href)
                                ? Icons.headphones_rounded
                                : videoHref(e.href)
                                ? Icons.play_circle_outline
                                : web
                                ? Icons.open_in_browser
                                : Icons.description_outlined,
                          ),
                          title: Text(
                            e.image
                                ? context.tr('图片 {0} · {1}', [i + 1, e.label])
                                : e.label,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                          ),
                          trailing: const Icon(Icons.chevron_right),
                          onTap: () {
                            if (e.image) {
                              Navigator.push(
                                context,
                                MaterialPageRoute<void>(
                                  builder: (_) => AttachmentPage(
                                    client: widget.client,
                                    session: widget.session,
                                    href: e.href,
                                  ),
                                ),
                              );
                            } else {
                              openSessionLink(
                                context,
                                widget.client,
                                widget.session,
                                e.href,
                              );
                            }
                          },
                        );
                      },
                    ),
            ),
          ],
        ),
      );
    },
  );
}
