import 'package:agentdeck/reading_scroll_view.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('bounded windows keep the reading anchor in both directions', (
    tester,
  ) async {
    final controller = ReadingScrollController();
    addTearDown(controller.dispose);
    final view = GlobalKey<ReadingScrollViewState>();
    var newest = 300;
    late StateSetter update;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (_, setState) {
              update = setState;
              return ReadingScrollView(
                key: view,
                scrollKey: const ValueKey('scroll'),
                controller: controller,
                itemKeys: [
                  for (var i = newest; i > newest - 100; i--) 'row-$i',
                ],
                itemBuilder: (_, index) {
                  final i = newest - index;
                  return SizedBox(
                    height: 70 + (i % 5) * 35,
                    child: Center(child: Text('row-$i')),
                  );
                },
              );
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    for (var round = 0; round < 4; round++) {
      controller.jumpTo(controller.position.maxScrollExtent - 200);
      await tester.pumpAndSettle();
      final visible = find.textContaining('row-').evaluate().where((e) {
        final rect = tester.getRect(find.byWidget(e.widget));
        return rect.top > 150 && rect.bottom < 500;
      }).first;
      final target = find.byWidget(visible.widget);
      final label = (visible.widget as Text).data!;
      final before = tester.getTopLeft(target).dy;
      view.currentState!.anchorReadingPosition();
      await tester.pumpAndSettle();
      expect(tester.getTopLeft(find.text(label)).dy, closeTo(before, 1));
      update(() => newest -= 40);
      await tester.pumpAndSettle();
      expect(tester.getTopLeft(find.text(label)).dy, closeTo(before, 1));
      expect(find.byType(Text).evaluate().length, lessThan(25));

      controller.jumpTo(controller.position.minScrollExtent + 150);
      await tester.pumpAndSettle();
      final returning = find.textContaining('row-').evaluate().where((e) {
        final rect = tester.getRect(find.byWidget(e.widget));
        return rect.top > 150 && rect.bottom < 500;
      }).first;
      final returnLabel = (returning.widget as Text).data!;
      final returnBefore = tester.getTopLeft(find.text(returnLabel)).dy;
      view.currentState!.anchorReadingPosition();
      await tester.pumpAndSettle();
      update(() => newest += 40);
      await tester.pumpAndSettle();
      expect(
        tester.getTopLeft(find.text(returnLabel)).dy,
        closeTo(returnBefore, 1),
      );
      expect(tester.takeException(), isNull);
    }
  });
}
