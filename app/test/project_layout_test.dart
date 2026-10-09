import 'dart:async';
import 'dart:io';
import 'package:agentdeck/core/client.dart';
import 'package:agentdeck/core/model.dart';
import 'package:agentdeck/core/vault.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

class DelayedLayoutVault extends DeviceVault {
  final List<Completer<void>> writes = [];
  Map<String, dynamic>? stored;
  @override
  Future<void> saveProjectLayout(Map<String, dynamic> value) async {
    final done = Completer<void>();
    writes.add(done);
    await done.future;
    stored = value;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'same project has independent layouts per agent after legacy migration',
    () {
      var layout = ProjectLayout(order: ['/b', '/a'], hidden: ['/a']);
      layout = layout.withScoped(
        'codex',
        layout.scoped('codex').toggleHidden('/a').reordered(['/a', '/b']),
      );
      expect(layout.scoped('codex').hidden, isEmpty);
      expect(layout.scoped('claude').hidden, {'/a'});
      layout = layout.withScoped(
        'claude',
        layout.scoped('claude').toggleCollapsed('/b'),
      );
      final restored = ProjectLayout.fromJson(layout.toJson());
      expect(restored.scoped('codex').order, ['/a', '/b']);
      expect(restored.scoped('codex').collapsed, isEmpty);
      expect(restored.scoped('claude').order, ['/b', '/a']);
      expect(restored.scoped('claude').collapsed, {'/b'});
    },
  );
  test(
    'secure layout storage restores collapsed, expanded and manual ordering',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final vault = DeviceVault();
      final before = ProjectLayout(
        order: ['/b', '/a'],
        collapsed: ['/b'],
        expanded: ['/a'],
        hidden: ['/b'],
      );
      await vault.saveProjectLayout(before.toJson());
      final client = AgentClient(vault: DeviceVault());
      addTearDown(client.dispose);
      await client.loadProjectLayout();
      expect(client.projectLayout.sorted(['/a', '/b']), ['/b', '/a']);
      expect(client.projectLayout.collapsed, {'/b'});
      expect(client.projectLayout.expanded, {'/a'});
      expect(client.projectLayout.hidden, {'/b'});
    },
  );
  test(
    'hidden and new projects retain stable placement without affecting saved layout',
    () {
      final layout = ProjectLayout(
        order: ['/b', '/missing', '/a'],
        collapsed: ['/b'],
      );
      expect(layout.sorted(['/a', '/b', '/new']), ['/b', '/a', '/new']);
      final moved = layout.reordered(['/a', '/b', '/new']);
      expect(moved.order, ['/a', '/missing', '/b', '/new']);
      expect(moved.collapsed, {'/b'});
      expect(
        ProjectLayout.fromJson({
          'version': 0,
          'order': ['/a'],
        }).order,
        isEmpty,
      );
    },
  );
  test(
    'visibility survives other layout changes and existing layouts stay compatible',
    () {
      final layout = ProjectLayout(order: ['/b', '/a']).toggleHidden('/a');
      final moved = layout.toggleCollapsed('/b').toggleExpanded('/b').reordered(
        ['/a', '/b'],
      );
      expect(moved.hidden, {'/a'});
      expect(moved.defaultOrder().hidden, {'/a'});
      expect(
        ProjectLayout.fromJson({
          'version': 1,
          'order': ['/b', '/a'],
        }).hidden,
        isEmpty,
      );
    },
  );
  test(
    'writes serialize and a failed write rolls back but permits the next change',
    () async {
      final vault = DelayedLayoutVault();
      final client = AgentClient(vault: vault);
      addTearDown(client.dispose);
      final one = client.updateProjectLayout(
        ProjectLayout(order: ['/b', '/a']),
      );
      final two = client.updateProjectLayout(
        ProjectLayout(order: ['/a', '/b'], collapsed: ['/a']),
      );
      await Future<void>.delayed(Duration.zero);
      expect(vault.writes.length, 1);
      vault.writes[0].complete();
      await one;
      await Future<void>.delayed(Duration.zero);
      expect(vault.writes.length, 2);
      final failure = expectLater(two, throwsA(isA<FileSystemException>()));
      vault.writes[1].completeError(StateError('fixture storage failure'));
      await failure;
      expect(client.projectLayout.order, ['/b', '/a']);
      expect(client.projectLayout.collapsed, isEmpty);
      final three = client.updateProjectLayout(
        client.projectLayout.toggleCollapsed('/b'),
      );
      await Future<void>.delayed(Duration.zero);
      vault.writes[2].complete();
      await three;
      expect(ProjectLayout.fromJson(vault.stored!).collapsed, {'/b'});
    },
  );
}
