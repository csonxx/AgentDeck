import 'package:agentdeck/core/permissions.dart';
import 'package:agentdeck/permission_control.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'widget_test.dart' show OperableClient;

class PermissionClient extends OperableClient {
  String mode = 'default';
  bool reject = false;
  final edits = <String>[];
  @override
  Future<SessionPermissions> sessionPermissions(String session) async =>
      SessionPermissions.fromJson({
        'mode': mode,
        'label': mode == 'default' ? 'Default' : 'Bypass permissions',
        'source': 'live',
        'version': mode,
        'editable': true,
        'detail': mode,
        'choices': [
          {'id': 'default', 'label': 'Default', 'detail': 'default'},
          {
            'id': 'bypassPermissions',
            'label': 'Bypass permissions',
            'detail': 'bypassPermissions',
          },
        ],
      });
  @override
  Future<SessionPermissions> changePermissions(
    String session,
    String version,
    String value,
  ) async {
    edits.add('$session:$version:$value');
    if (reject) throw const FormatException('NATIVE_POLICY_REJECTED');
    mode = value;
    return sessionPermissions(session);
  }
}

void main() {
  testWidgets(
    'permission picker reads native mode and changes the same session',
    (tester) async {
      final c = PermissionClient();
      addTearDown(c.dispose);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PermissionControl(client: c, session: 'original'),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('Default'), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('session-permission-button')));
      await tester.pumpAndSettle();
      expect(find.text('default'), findsWidgets);
      await tester.tap(
        find.byKey(const ValueKey('permission-choice-bypassPermissions')),
      );
      await tester.pumpAndSettle();
      expect(c.edits, ['original:default:bypassPermissions']);
      expect(
        tester
            .widget<Text>(find.byKey(const ValueKey('permission-current')))
            .data,
        'Bypass permissions',
      );
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
  testWidgets(
    'native rejection remains visible and never claims requested mode applied',
    (tester) async {
      final c = PermissionClient()..reject = true;
      addTearDown(c.dispose);
      tester.view.physicalSize = const Size(320, 600);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      await tester.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: TextScaler.linear(2)),
            child: child!,
          ),
          home: Scaffold(
            body: PermissionControl(client: c, session: 'original'),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('session-permission-button')));
      await tester.pumpAndSettle();
      await tester.ensureVisible(
        find.byKey(const ValueKey('permission-choice-bypassPermissions')),
      );
      await tester.tap(
        find.byKey(const ValueKey('permission-choice-bypassPermissions')),
      );
      await tester.pumpAndSettle();
      expect(find.textContaining('NATIVE_POLICY_REJECTED'), findsOneWidget);
      expect(
        tester
            .widget<Text>(find.byKey(const ValueKey('permission-current')))
            .data,
        'Default',
      );
      expect(c.mode, 'default');
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
}
