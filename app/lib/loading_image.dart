import 'package:flutter/material.dart';
import 'design.dart';
import 'language.dart';

class ImageLoading extends StatelessWidget {
  final double? width, height;
  const ImageLoading({super.key, this.width, this.height});
  @override
  Widget build(BuildContext context) => SizedBox(
    key: const ValueKey('image-loading'),
    width: width,
    height: height ?? 120,
    child: Center(
      child: SizedBox(
        width: 22,
        height: 22,
        child: CircularProgressIndicator(
          strokeWidth: 2,
          color: DeckColors.accent,
          semanticsLabel: context.tr('正在加载图片…'),
        ),
      ),
    ),
  );
}

/// Bytes being available is not the same as a decoded frame being paintable.
/// Keep the loading indicator until ImageStream delivers its first frame.
class LoadingImage extends StatefulWidget {
  final ImageProvider image;
  final double? width, height;
  final BoxFit? fit;
  final VoidCallback? onRetry;
  final Color? errorColor;
  final ImageErrorWidgetBuilder? errorBuilder;
  const LoadingImage({
    super.key,
    required this.image,
    this.width,
    this.height,
    this.fit,
    this.onRetry,
    this.errorColor,
    this.errorBuilder,
  });
  @override
  State<LoadingImage> createState() => _LoadingImageState();
}

class _LoadingImageState extends State<LoadingImage> {
  int attempt = 0;
  Future<void> retry() async {
    await widget.image.evict();
    if (!mounted) return;
    if (widget.onRetry case final retry?) {
      retry();
    } else {
      setState(() => attempt++);
    }
  }

  @override
  Widget build(BuildContext context) => Image(
    key: ValueKey(attempt),
    image: widget.image,
    width: widget.width,
    height: widget.height,
    fit: widget.fit,
    frameBuilder: (_, child, frame, synchronouslyLoaded) =>
        synchronouslyLoaded || frame != null
        ? child
        : ImageLoading(width: widget.width, height: widget.height),
    errorBuilder:
        widget.errorBuilder ??
        (context, _, _) => TextButton.icon(
          key: const ValueKey('image-retry'),
          style: TextButton.styleFrom(foregroundColor: widget.errorColor),
          onPressed: retry,
          icon: const Icon(Icons.refresh, size: 18),
          label: Text(context.tr('{0}，点击重试', [context.tr('图片无法解码')])),
        ),
  );
}
