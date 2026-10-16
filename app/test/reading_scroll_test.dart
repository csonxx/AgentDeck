import 'package:agentdeck/reading_scroll_view.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('older boundary fills the viewport after rebasing near its end', (
    tester,
  ) async {
    final controller = ReadingScrollController();
    addTearDown(controller.dispose);
    final view = GlobalKey<ReadingScrollViewState>();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ReadingScrollView(
            key: view,
            controller: controller,
            scrollKey: const ValueKey('boundary-history'),
            itemKeys: [for (var i = 0; i < 30; i++) 'row-$i'],
            itemBuilder: (_, i) => SizedBox(height: 110, child: Text('row-$i')),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    controller.jumpTo(controller.position.maxScrollExtent);
    await tester.pumpAndSettle();
    view.currentState!.anchorReadingPosition();
    await tester.pumpAndSettle();
    controller.jumpTo(controller.position.maxScrollExtent);
    await tester.pumpAndSettle();
    expect(tester.getTopLeft(find.text('row-29')).dy, closeTo(0, 1));
    expect(tester.takeException(), isNull);
  });
  testWidgets('image resizing and pivot changes leave no holes in history', (
    tester,
  ) async {
    final controller = ReadingScrollController();
    addTearDown(controller.dispose);
    final view = GlobalKey<ReadingScrollViewState>();
    var newest = 160;
    var loaded = false;
    late StateSetter update;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (_, setState) {
              update = setState;
              return ReadingScrollView(
                key: view,
                controller: controller,
                scrollKey: const ValueKey('image-history'),
                itemKeys: [
                  for (var i = newest; i > newest - 60; i--) 'item-$i',
                ],
                itemBuilder: (_, index) {
                  final i = newest - index;
                  return SizedBox(
                    key: ValueKey('paint-$i'),
                    height: i % 3 == 0 ? (loaded ? 320 : 120) : 60,
                    child: ColoredBox(
                      color: Colors.blue,
                      child: Text('item-$i'),
                    ),
                  );
                },
              );
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    void noHoles() {
      final viewport = tester.getRect(find.byType(ReadingScrollView));
      final rects = <Rect>[];
      for (var i = newest; i > newest - 60; i--) {
        final row = find.byKey(ValueKey('paint-$i'));
        if (row.evaluate().isEmpty) continue;
        final rect = tester.getRect(row);
        if (rect.overlaps(viewport)) rects.add(rect.intersect(viewport));
      }
      rects.sort((a, b) => a.top.compareTo(b.top));
      expect(rects, isNotEmpty);
      expect(rects.first.top, closeTo(viewport.top, 1));
      expect(rects.last.bottom, closeTo(viewport.bottom, 1));
      for (var i = 1; i < rects.length; i++) {
        expect(rects[i].top, closeTo(rects[i - 1].bottom, 1));
      }
    }

    for (var round = 0; round < 8; round++) {
      controller.jumpTo(
        controller.position.minScrollExtent +
            (controller.position.maxScrollExtent -
                    controller.position.minScrollExtent) *
                .55,
      );
      await tester.pumpAndSettle();
      noHoles();
      view.currentState!.anchorReadingPosition();
      await tester.pumpAndSettle();
      update(() {
        loaded = !loaded;
        newest += round.isEven ? -15 : 15;
      });
      await tester.pumpAndSettle();
      noHoles();
      expect(tester.takeException(), isNull);
    }
  });
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
  testWidgets('evicting the pivot keeps another visible record in place', (
    tester,
  ) async {
    final controller = ReadingScrollController();
    addTearDown(controller.dispose);
    final view = GlobalKey<ReadingScrollViewState>();
    var keys = [for (var i = 0; i < 30; i++) 'row-$i'];
    late StateSetter update;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (_, setState) {
              update = setState;
              return ReadingScrollView(
                key: view,
                scrollKey: const ValueKey('test-window'),
                controller: controller,
                itemKeys: keys,
                itemBuilder: (_, index) => SizedBox(
                  height: 110,
                  child: Center(child: Text(keys[index])),
                ),
              );
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    controller.jumpTo(700);
    await tester.pumpAndSettle();
    final visible = keys.where((key) {
      final finder = find.text(key);
      return finder.evaluate().isNotEmpty &&
          tester.getCenter(finder).dy > 100 &&
          tester.getCenter(finder).dy < 500;
    }).toList();
    final pivot = visible.reduce(
      (a, b) =>
          (tester.getCenter(find.text(a)).dy - 300).abs() <
              (tester.getCenter(find.text(b)).dy - 300).abs()
          ? a
          : b,
    );
    final survivor = visible.firstWhere((key) => key != pivot);
    view.currentState!.anchorReadingPosition();
    await tester.pumpAndSettle();
    final before = tester.getTopLeft(find.text(survivor)).dy;
    update(() => keys = keys.where((key) => key != pivot).toList());
    await tester.pumpAndSettle();
    expect(find.text(survivor), findsOneWidget);
    expect(tester.getTopLeft(find.text(survivor)).dy, closeTo(before, 1));
    expect(tester.takeException(), isNull);
  });
}
