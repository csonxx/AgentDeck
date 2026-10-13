import 'package:flutter/material.dart';

class ReadingScrollController extends ScrollController {
  @override
  ScrollPosition createScrollPosition(
    ScrollPhysics physics,
    ScrollContext context,
    ScrollPosition? oldPosition,
  ) => _ReadingPosition(
    physics: physics,
    context: context,
    oldPosition: oldPosition,
  );

  void rebase(double value) {
    if (hasClients) (position as _ReadingPosition).rebase(value);
  }
}

class _ReadingPosition extends ScrollPositionWithSingleContext {
  _ReadingPosition({
    required super.physics,
    required super.context,
    super.oldPosition,
  });

  // Rebase the coordinate origin before the next layout, preserving an active
  // drag. A jumpTo would stop the user's gesture or clamp to the old bounds.
  void rebase(double value) => correctPixels(value);
}

// Two lazy slivers grow away from a stable reading anchor. Evicting records at
// either end does not change that anchor's location on screen.
class ReadingScrollView extends StatefulWidget {
  final ReadingScrollController controller;
  final List<String> itemKeys;
  final IndexedWidgetBuilder itemBuilder;
  final Key scrollKey;
  const ReadingScrollView({
    super.key,
    required this.controller,
    required this.itemKeys,
    required this.itemBuilder,
    required this.scrollKey,
  });

  @override
  State<ReadingScrollView> createState() => ReadingScrollViewState();
}

class ReadingScrollViewState extends State<ReadingScrollView> {
  final _viewport = GlobalKey();
  final _center = GlobalKey();
  final _anchors = <String, GlobalKey>{};
  String? _pivot;

  void anchorReadingPosition() {
    final viewport = _viewport.currentContext?.findRenderObject();
    if (viewport is! RenderBox || !viewport.hasSize) return;
    final bounds = viewport.localToGlobal(Offset.zero) & viewport.size;
    String? selected;
    Rect? selectedRect;
    var distance = double.infinity;
    for (final entry in _anchors.entries) {
      final box = entry.value.currentContext?.findRenderObject();
      if (box is! RenderBox || !box.attached || !box.hasSize) continue;
      final rect = box.localToGlobal(Offset.zero) & box.size;
      if (!rect.overlaps(bounds)) continue;
      final d = (rect.center.dy - bounds.center.dy).abs();
      if (d < distance) {
        selected = entry.key;
        selectedRect = rect;
        distance = d;
      }
    }
    if (selected == null || selected == _pivot) return;
    widget.controller.rebase(selectedRect!.bottom - bounds.bottom);
    setState(() => _pivot = selected);
  }

  @override
  Widget build(BuildContext context) {
    final keys = widget.itemKeys;
    _anchors.removeWhere((key, _) => !keys.contains(key));
    if (!keys.contains(_pivot)) {
      _pivot = keys.firstOrNull;
      widget.controller.rebase(0);
    }
    final pivot = keys.indexOf(_pivot ?? '');
    Widget item(int index) => KeyedSubtree(
      key: ValueKey(keys[index]),
      child: SizedBox(
        key: _anchors.putIfAbsent(keys[index], GlobalKey.new),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 20),
          child: widget.itemBuilder(context, index),
        ),
      ),
    );
    final indices = {for (var i = 0; i < keys.length; i++) keys[i]: i};
    return SizedBox.expand(
      key: _viewport,
      child: CustomScrollView(
        key: widget.scrollKey,
        controller: widget.controller,
        reverse: true,
        physics: const AlwaysScrollableScrollPhysics(),
        scrollBehavior: ScrollConfiguration.of(
          context,
        ).copyWith(overscroll: false),
        center: _center,
        keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
        slivers: [
          if (pivot > 0)
            SliverList.builder(
              itemCount: pivot,
              findChildIndexCallback: (key) {
                final i = indices[(key as ValueKey<String>).value];
                return i != null && i < pivot ? pivot - i - 1 : null;
              },
              itemBuilder: (_, index) => item(pivot - index - 1),
            ),
          SliverList.builder(
            key: _center,
            itemCount: keys.length - pivot,
            findChildIndexCallback: (key) {
              final i = indices[(key as ValueKey<String>).value];
              return i != null && i >= pivot ? i - pivot : null;
            },
            itemBuilder: (_, index) => item(pivot + index),
          ),
        ],
      ),
    );
  }
}
