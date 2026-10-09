import 'package:flutter/material.dart';
import 'design.dart';
import 'language.dart';

class RunningIndicator extends StatelessWidget {
  const RunningIndicator({super.key});
  @override
  Widget build(BuildContext context) => SizedBox(
    width: 16,
    height: 16,
    child: CircularProgressIndicator(
      strokeWidth: 1.6,
      color: DeckColors.accent,
      semanticsLabel: context.tr('执行中'),
    ),
  );
}
