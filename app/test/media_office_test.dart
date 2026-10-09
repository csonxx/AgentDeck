import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'package:archive/archive.dart';
import 'package:agentdeck/media_preview.dart';
import 'package:agentdeck/office_preview.dart';
import 'package:agentdeck/attachment_page.dart';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/language.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

Uint8List package(Map<String, String> files) {
  final archive = Archive();
  for (final entry in files.entries) {
    final bytes = utf8.encode(entry.value);
    archive.addFile(ArchiveFile(entry.key, bytes.length, bytes));
  }
  return Uint8List.fromList(ZipEncoder().encode(archive));
}

class RealHttp extends HttpOverrides {}

class DocumentClient extends AgentClient {
  @override
  Future<SessionAttachment> attachment(String session, String href) async =>
      SessionAttachment(
        name: 'readme.md',
        mime: 'text/plain',
        bytes: Uint8List.fromList(
          utf8.encode('# Generated report\n\n**Ready**'),
        ),
      );
}

void main() {
  test(
    'memory video supports decoder ranges and requires private token',
    () async {
      await HttpOverrides.runWithHttpOverrides(() async {
        final bytes = Uint8List.fromList(List.generate(100, (i) => i));
        final server = await MemoryVideoServer.open(bytes, 'video/mp4');
        final client = HttpClient();
        try {
          final req = await client.getUrl(server.uri);
          req.headers.set('Range', 'bytes=10-19');
          final res = await req.close();
          final body = await res.fold<List<int>>([], (a, b) => a..addAll(b));
          expect(res.statusCode, 206);
          expect(res.headers.value('Content-Range'), 'bytes 10-19/100');
          expect(body, bytes.sublist(10, 20));
          final forbidden = await (await client.getUrl(
            server.uri.replace(path: '/wrong-token'),
          )).close();
          expect(forbidden.statusCode, 404);
          await forbidden.drain<void>();
          final bad = await client.getUrl(server.uri);
          bad.headers.set('Range', 'bytes=200-');
          final rejected = await bad.close();
          expect(rejected.statusCode, 416);
          await rejected.drain<void>();
        } finally {
          client.close(force: true);
          await server.close();
        }
      }, RealHttp());
    },
  );
  test('Word paragraphs, rich runs and table cells are readable', () {
    final bytes = package({
      'word/document.xml':
          '<w:document xmlns:w="word"><w:body><w:p><w:r><w:t>Hello </w:t></w:r><w:r><w:t>world</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>Cell A</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Cell B</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>',
    });
    final doc = parseOffice({'name': 'report.docx', 'bytes': bytes});
    expect(doc['lines'], ['Hello world', 'Cell A  |  Cell B']);
  });
  test('Excel sheets preserve shared strings, sparse cells and cached formulas', () {
    final bytes = package({
      'xl/workbook.xml':
          '<workbook xmlns:r="rel"><sheets><sheet name="Summary" r:id="r1"/><sheet name="Details" r:id="r2"/></sheets></workbook>',
      'xl/_rels/workbook.xml.rels':
          '<Relationships><Relationship Id="r1" Target="worksheets/sheet1.xml"/><Relationship Id="r2" Target="worksheets/sheet2.xml"/></Relationships>',
      'xl/sharedStrings.xml': '<sst><si><t>Revenue</t></si></sst>',
      'xl/worksheets/sheet1.xml':
          '<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1"><f>1+2</f><v>3</v></c></row></sheetData></worksheet>',
      'xl/worksheets/sheet2.xml':
          '<worksheet><sheetData><row r="40"><c r="A40" t="inlineStr"><is><t>More</t></is></c></row></sheetData></worksheet>',
    });
    final doc = parseOffice({'name': 'table.xlsx', 'bytes': bytes});
    final sheets = doc['sheets'] as List;
    expect(sheets.length, 2);
    expect(sheets[0]['rows'], [
      ['Revenue', '', '3'],
    ]);
    expect(sheets[1]['name'], 'Details');
    expect(sheets[1]['rowNumbers'], [40]);
    expect(columnLabel(100), 'CV');
  });
  test('Office previews reject XML declarations and malformed archives', () {
    final bytes = package({
      'word/document.xml':
          '<!DOCTYPE doc [<!ENTITY x SYSTEM "file:///private">]><doc/>',
    });
    expect(
      () => parseOffice({'name': 'bad.docx', 'bytes': bytes}),
      throwsFormatException,
    );
    expect(
      () => parseOffice({'name': 'bad.xlsx', 'bytes': Uint8List(4)}),
      throwsA(anything),
    );
  });
  testWidgets(
    'Markdown attachment renders formatting rather than raw markers',
    (tester) async {
      final c = DocumentClient();
      addTearDown(c.dispose);
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: UiStrings.delegates,
          supportedLocales: UiStrings.supportedLocales,
          locale: AppLanguage.english.locale,
          home: AttachmentPage(
            client: c,
            session: 's',
            href: '/fixture/readme.md',
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('Generated report'), findsOneWidget);
      expect(find.text('# Generated report'), findsNothing);
      expect(find.text('Ready'), findsOneWidget);
    },
  );
}
