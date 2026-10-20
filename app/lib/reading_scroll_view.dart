import 'dart:math' as math;
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';

class ReadingScrollController extends ScrollController {
  double? Function()? _centerExtent;
  bool correcting = false;
  @override
  ScrollPosition createScrollPosition(
    ScrollPhysics physics,
    ScrollContext context,
    ScrollPosition? oldPosition,
  ) => _ReadingPosition(
    physics: physics,
    context: context,
    oldPosition: oldPosition,
    centerExtent: () => _centerExtent?.call(),
  );

  void rebase(double value) {
    if (hasClients) (position as _ReadingPosition).rebase(value);
  }

  void revealPagingEdge({required bool older}) {
    if (!hasClients) return;
    correcting = true;
    try {
      (position as _ReadingPosition).reveal(
        older ? position.maxScrollExtent : position.minScrollExtent,
      );
    } finally {
      correcting = false;
    }
  }
}

class _ReadingPosition extends ScrollPositionWithSingleContext {
  final double? Function() centerExtent;
  _ReadingPosition({
    required super.physics,
    required super.context,
    super.oldPosition,
    required this.centerExtent,
  });

  @override
  bool applyContentDimensions(double minScrollExtent, double maxScrollExtent) {
    // A centered viewport clamps its maximum to zero. After rebasing near the
    // oldest row, that permits scrolling past the content into an empty area.
    // The actual oldest boundary can be negative in the rebased coordinates.
    final extent = centerExtent();
    if (extent != null && extent.isFinite) {
      maxScrollExtent = math.max(minScrollExtent, extent - viewportDimension);
    }
    return super.applyContentDimensions(minScrollExtent, maxScrollExtent);
  }

  // Rebase the coordinate origin before the next layout, preserving an active
  // drag. A jumpTo would stop the user's gesture or clamp to the old bounds.
  void rebase(double value) => correctPixels(value);
  // Correct a completed folded-page seek without cancelling the user's drag.
  void reveal(double value) => forcePixels(value);
}

// The center row grows upwards in a reverse viewport. Compensate its size
// change during layout, before paint, so decoding an image or streaming a long
// answer cannot move the text being read. Changing the pivot starts a new
// measurement; ordinary scrolling still belongs to ScrollPosition.
class _ReadingSliverList extends SliverList {
  final String pivot;
  final bool preserve;
  final double? measuredHeight;
  const _ReadingSliverList({
    super.key,
    required super.delegate,
    required this.pivot,
    required this.preserve,
    this.measuredHeight,
  });

  @override
  RenderSliverList createRenderObject(BuildContext context) => _ReadingSliver(
    childManager: context as SliverMultiBoxAdaptorElement,
    pivot: pivot,
    preserve: preserve,
    measuredHeight: measuredHeight,
  );

  @override
  void updateRenderObject(BuildContext context, _ReadingSliver renderObject) {
    renderObject.configure(pivot, preserve, measuredHeight);
  }
}

class _ReadingSliver extends RenderSliverList {
  String pivot;
  bool preserve;
  double? _height;
  _ReadingSliver({
    required super.childManager,
    required this.pivot,
    required this.preserve,
    double? measuredHeight,
  }) : _height = measuredHeight;

  void configure(String value, bool enabled, double? measuredHeight) {
    if (pivot != value) {
      _height = measuredHeight;
    } else if (preserve != enabled) {
      _height = null;
    }
    pivot = value;
    preserve = enabled;
    markNeedsLayout();
  }

  @override
  void performLayout() {
    super.performLayout();
    if (geometry?.scrollOffsetCorrection != null) return;
    final child = firstChild;
    if (child == null || indexOf(child) != 0) {
      _height = null;
      return;
    }
    final height = paintExtentOf(child);
    final previous = _height;
    _height = height;
    if (preserve && previous != null && (height - previous).abs() > .01) {
      geometry = SliverGeometry(scrollOffsetCorrection: height - previous);
    }
  }
}

// Two lazy slivers grow away from a stable reading anchor. Evicting records at
// either end does not change that anchor's location on screen.
class ReadingScrollView extends StatefulWidget {
  final ReadingScrollController controller;
  final List<String> itemKeys;
  final Set<String>? anchorKeys;
  final IndexedWidgetBuilder itemBuilder;
  final Key scrollKey;
  const ReadingScrollView({
    super.key,
    required this.controller,
    required this.itemKeys,
    this.anchorKeys,
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
  double? _pivotHeight;

  double? _centerExtent() {
    final sliver = _center.currentContext?.findRenderObject();
    return sliver is RenderSliver ? sliver.geometry?.scrollExtent : null;
  }

  @override
  void initState() {
    super.initState();
    widget.controller._centerExtent = _centerExtent;
  }

  @override
  void dispose() {
    widget.controller._centerExtent = null;
    super.dispose();
  }

  @override
  void didUpdateWidget(covariant ReadingScrollView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller) {
      oldWidget.controller._centerExtent = null;
      widget.controller._centerExtent = _centerExtent;
    }
    if (_pivot != null && !widget.itemKeys.contains(_pivot)) {
      // The old render tree still exists here. Move the origin to another
      // retained row before disposing the removed anchor, without jumping.
      _anchor(widget.itemKeys.toSet(), allowOffscreen: true);
    }
  }

  void anchorReadingPosition() {
    if (_anchor(widget.itemKeys.toSet())) setState(() {});
  }

  bool _anchor(Set<String> candidates, {bool allowOffscreen = false}) {
    final viewport = _viewport.currentContext?.findRenderObject();
    if (viewport is! RenderBox || !viewport.hasSize) return false;
    final bounds = viewport.localToGlobal(Offset.zero) & viewport.size;
    String? selected;
    Rect? selectedRect;
    var distance = double.infinity;
    for (final entry in _anchors.entries) {
      if (!candidates.contains(entry.key) ||
          widget.anchorKeys != null &&
              !widget.anchorKeys!.contains(entry.key)) {
        continue;
      }
      final box = entry.value.currentContext?.findRenderObject();
      if (box is! RenderBox || !box.attached || !box.hasSize) continue;
      final rect = box.localToGlobal(Offset.zero) & box.size;
      if (!rect.overlaps(bounds) && !allowOffscreen) continue;
      final d = (rect.center.dy - bounds.center.dy).abs();
      if (d < distance) {
        selected = entry.key;
        selectedRect = rect;
        distance = d;
      }
    }
    if (selected == null || selected == _pivot) return false;
    widget.controller.rebase(selectedRect!.bottom - bounds.bottom);
    _pivot = selected;
    _pivotHeight = selectedRect.height;
    return true;
  }

  @override
  Widget build(BuildContext context) {
    final keys = widget.itemKeys;
    _anchors.removeWhere((key, _) => !keys.contains(key));
    if (!keys.contains(_pivot)) {
      _pivot = keys.firstOrNull;
      _pivotHeight = null;
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
          _ReadingSliverList(
            key: _center,
            pivot: _pivot ?? '',
            preserve: widget.anchorKeys?.contains(_pivot) ?? pivot > 0,
            measuredHeight: _pivotHeight,
            delegate: SliverChildBuilderDelegate(
              (_, index) => item(pivot + index),
              childCount: keys.length - pivot,
              findChildIndexCallback: (key) {
                final i = indices[(key as ValueKey<String>).value];
                return i != null && i >= pivot ? i - pivot : null;
              },
            ),
          ),
        ],
      ),
    );
  }
}
