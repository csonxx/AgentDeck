import 'dart:convert';
import 'package:archive/archive.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:xml/xml.dart';
import 'core/model.dart';
import 'language.dart';

bool officeName(String name) =>
    RegExp(r'\.(doc|docx|xls|xlsx)$', caseSensitive: false).hasMatch(name);
String columnLabel(int column) {
  var result = '';
  while (column > 0) {
    column--;
    result = String.fromCharCode(65 + column % 26) + result;
    column ~/= 26;
  }
  return result;
}

List<XmlElement> elements(XmlNode n, String tag) => n.descendants
    .whereType<XmlElement>()
    .where((e) => e.name.local == tag)
    .toList();
String textRuns(XmlNode n) => elements(n, 't').map((e) => e.innerText).join();
Map<String, dynamic> parseOffice(Map<String, dynamic> input) {
  if (input['mime'] == 'application/vnd.agentdeck.office+json') {
    final bytes = input['bytes'] as Uint8List;
    if (bytes.length > 8 << 20) {
      throw const FormatException('Document exceeds preview bounds');
    }
    final doc = Map<String, dynamic>.from(
      jsonDecode(utf8.decode(bytes)) as Map,
    );
    if (doc['kind'] == 'word') {
      final lines = List<String>.from(doc['lines'] as List);
      if (lines.length > 10000 || lines.join().length > 1 << 20) {
        throw const FormatException('Document exceeds preview bounds');
      }
      return {
        'kind': 'word',
        'lines': lines,
        'truncated': doc['truncated'] == true,
      };
    }
    if (doc['kind'] != 'excel') throw const FormatException('Invalid document');
    final sheets = List<Map<String, dynamic>>.from(doc['sheets'] as List);
    if (sheets.isEmpty || sheets.length > 50) {
      throw const FormatException('Invalid worksheets');
    }
    var cells = 0;
    for (final sheet in sheets) {
      if (sheet['name'] is! String ||
          sheet['columns'] is! int ||
          (sheet['columns'] as int) < 0 ||
          (sheet['columns'] as int) > 100) {
        throw const FormatException('Invalid worksheet');
      }
      final rows = (sheet['rows'] as List)
          .map((r) => List<String>.from(r as List))
          .toList();
      final numbers = List<int>.from(sheet['rowNumbers'] as List);
      if (rows.length > 2000 ||
          numbers.length != rows.length ||
          numbers.any((n) => n < 1)) {
        throw const FormatException('Invalid rows');
      }
      for (final row in rows) {
        cells += row.length;
        if (row.length > 100 ||
            cells > 200000 ||
            row.any((v) => v.length > 8193)) {
          throw const FormatException('Document exceeds preview bounds');
        }
      }
      sheet['rows'] = rows;
      sheet['rowNumbers'] = numbers;
    }
    return {
      'kind': 'excel',
      'sheets': sheets,
      'truncated': doc['truncated'] == true,
    };
  }
  final archive = ZipDecoder().decodeBytes(input['bytes'] as Uint8List);
  if (archive.length > 2000) {
    throw const FormatException('Document exceeds preview bounds');
  }
  var total = 0;
  for (final file in archive) {
    total += file.size;
    if (total > 32 << 20 || file.size > 16 << 20) {
      throw const FormatException('Document exceeds preview bounds');
    }
  }
  XmlDocument xml(String name) {
    final file = archive.findFile(name);
    if (file == null) throw const FormatException('Missing document part');
    final source = utf8.decode(file.content as List<int>);
    if (source.contains('<!DOCTYPE') || source.contains('<!ENTITY')) {
      throw const FormatException('Unsupported XML declaration');
    }
    return XmlDocument.parse(source);
  }

  if ((input['name'] as String).toLowerCase().endsWith('.docx')) {
    final document = xml('word/document.xml');
    final body = elements(document, 'body').first;
    final lines = <String>[];
    for (final node in body.children.whereType<XmlElement>()) {
      if (node.name.local == 'p') lines.add(textRuns(node));
      if (node.name.local == 'tbl') {
        for (final row in elements(node, 'tr')) {
          lines.add(
            elements(row, 'tc')
                .map((cell) => elements(cell, 'p').map(textRuns).join('\n'))
                .join('  |  '),
          );
        }
      }
      if (lines.length > 10000) {
        throw const FormatException('Document exceeds preview bounds');
      }
    }
    return {'kind': 'word', 'lines': lines};
  }
  final book = xml('xl/workbook.xml');
  final rels = xml('xl/_rels/workbook.xml.rels');
  final targets = {
    for (final e in elements(rels, 'Relationship'))
      e.getAttribute('Id'): e.getAttribute('Target'),
  };
  final strings = archive.findFile('xl/sharedStrings.xml') == null
      ? <String>[]
      : elements(xml('xl/sharedStrings.xml'), 'si').map(textRuns).toList();
  final sheets = <Map<String, dynamic>>[];
  for (final sheet in elements(book, 'sheet').take(50)) {
    final id = sheet.attributes
        .where((a) => a.name.local == 'id')
        .firstOrNull
        ?.value;
    final target = targets[id];
    if (target == null) continue;
    final path = Uri.parse(
      'xl/workbook.xml',
    ).resolve(target).path.replaceFirst(RegExp(r'^/'), '');
    if (!path.startsWith('xl/worksheets/')) continue;
    final data = xml(path);
    final rows = <List<String>>[];
    var columns = 0;
    final rowNumbers = <int>[];
    final allRows = elements(data, 'row');
    var truncatedColumns = false;
    for (final row in allRows.take(2000)) {
      final cells = <int, String>{};
      for (final cell in row.children.whereType<XmlElement>().where(
        (e) => e.name.local == 'c',
      )) {
        final ref = cell.getAttribute('r') ?? '';
        var column = 0;
        for (final unit
            in RegExp(r'^[A-Z]+').stringMatch(ref)?.codeUnits ?? <int>[]) {
          column = column * 26 + unit - 64;
        }
        if (column > 100) truncatedColumns = true;
        if (column < 1 || column > 100) continue;
        final type = cell.getAttribute('t');
        var value = elements(cell, 'v').firstOrNull?.innerText ?? '';
        if (type == 's') {
          final index = int.tryParse(value);
          value = index != null && index >= 0 && index < strings.length
              ? strings[index]
              : '';
        }
        if (type == 'inlineStr') value = textRuns(cell);
        if (type == 'b') value = value == '1' ? 'TRUE' : 'FALSE';
        cells[column - 1] = value.length > 4096
            ? '${value.substring(0, 4096)}…'
            : value;
        if (column > columns) columns = column;
      }
      rowNumbers.add(
        int.tryParse(row.getAttribute('r') ?? '') ?? rows.length + 1,
      );
      rows.add(
        List.generate(
          cells.isEmpty ? 0 : cells.keys.reduce((a, b) => a > b ? a : b) + 1,
          (i) => cells[i] ?? '',
        ),
      );
    }
    sheets.add({
      'name': sheet.getAttribute('name') ?? 'Sheet',
      'rows': rows,
      'columns': columns,
      'rowNumbers': rowNumbers,
      'truncated': allRows.length > 2000 || truncatedColumns,
    });
  }
  if (sheets.isEmpty) throw const FormatException('No readable worksheets');
  return {'kind': 'excel', 'sheets': sheets};
}

class OfficeAttachment extends StatefulWidget {
  final SessionAttachment attachment;
  const OfficeAttachment({super.key, required this.attachment});
  @override
  State<OfficeAttachment> createState() => _OfficeAttachmentState();
}

class _OfficeAttachmentState extends State<OfficeAttachment> {
  late final data = compute(parseOffice, {
    'name': widget.attachment.name,
    'mime': widget.attachment.mime,
    'bytes': widget.attachment.bytes,
  });
  int selected = 0;
  @override
  Widget build(BuildContext context) => FutureBuilder<Map<String, dynamic>>(
    future: data,
    builder: (context, snap) {
      if (snap.hasError) {
        return Center(child: Text(context.tr('文档无法预览，文件可能损坏或格式不支持')));
      }
      if (!snap.hasData) {
        return const Center(child: CircularProgressIndicator());
      }
      final doc = snap.data!;
      if (doc['kind'] == 'word') {
        final lines = List<String>.from(doc['lines']);
        return Column(
          children: [
            if (doc['truncated'] == true)
              Padding(
                padding: const EdgeInsets.all(12),
                child: Text(context.tr('文档较大，仅显示部分内容')),
              ),
            Expanded(
              child: SelectionArea(
                child: ListView.builder(
                  padding: const EdgeInsets.all(20),
                  itemCount: lines.length,
                  itemBuilder: (_, i) => Padding(
                    padding: const EdgeInsets.only(bottom: 12),
                    child: Text(lines[i], style: const TextStyle(height: 1.6)),
                  ),
                ),
              ),
            ),
          ],
        );
      }
      final sheets = List<Map<String, dynamic>>.from(doc['sheets']);
      final sheet = sheets[selected];
      final rows = List<List<String>>.from(sheet['rows']);
      final columns = (sheet['columns'] as int).clamp(1, 100);
      final rowNumbers = List<int>.from(sheet['rowNumbers']);
      return Column(
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: DropdownButton<int>(
              isExpanded: true,
              value: selected,
              items: [
                for (var i = 0; i < sheets.length; i++)
                  DropdownMenuItem(
                    value: i,
                    child: Text(sheets[i]['name'] as String),
                  ),
              ],
              onChanged: (v) => setState(() => selected = v!),
            ),
          ),
          if (sheet['truncated'] == true || doc['truncated'] == true)
            Padding(
              padding: const EdgeInsets.all(8),
              child: Text(context.tr('预览最多显示 2000 行、100 列')),
            ),
          Expanded(
            child: SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: SizedBox(
                width: columns * 140.0 + 48,
                child: SelectionArea(
                  child: ListView.builder(
                    itemCount: rows.length + 1,
                    itemBuilder: (context, i) => IntrinsicHeight(
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          SizedBox(
                            width: 48,
                            child: Padding(
                              padding: const EdgeInsets.all(8),
                              child: Text(
                                i == 0 ? '' : '${rowNumbers[i - 1]}',
                                style: const TextStyle(color: Colors.grey),
                              ),
                            ),
                          ),
                          for (var c = 0; c < columns; c++)
                            Container(
                              width: 140,
                              padding: const EdgeInsets.all(8),
                              decoration: BoxDecoration(
                                border: Border.all(
                                  color: Colors.black12,
                                  width: .5,
                                ),
                              ),
                              child: Text(
                                i == 0
                                    ? columnLabel(c + 1)
                                    : c < rows[i - 1].length
                                    ? rows[i - 1][c]
                                    : '',
                                maxLines: 6,
                                overflow: TextOverflow.ellipsis,
                              ),
                            ),
                        ],
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      );
    },
  );
}
