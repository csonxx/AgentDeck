import 'language.dart';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// Native Flutter visual language, informed by the ZEEKR consumer app's
/// restrained type, light surfaces and small orange accents. No brand assets.
abstract final class DeckColors {
  static const ink = Color(0xFF191C20);
  static const paper = Color(0xFFF5F6F8);
  static const panel = Color(0xFFEFF1F4);
  static const muted = Color(0xFF777E87);
  static const line = Color(0xFFE7EAEE);
  static const accent = Color(0xFFEF633C);
  static const online = Color(0xFF238263);
}

ThemeData deckTheme() {
  final family = Platform.isIOS || Platform.isMacOS ? 'PingFang SC' : null;
  final shape = RoundedRectangleBorder(borderRadius: BorderRadius.circular(16));
  return ThemeData(
    useMaterial3: true,
    fontFamily: family,
    scaffoldBackgroundColor: DeckColors.paper,
    colorScheme: const ColorScheme.light(
      primary: DeckColors.ink,
      onPrimary: Colors.white,
      secondary: DeckColors.accent,
      surface: Colors.white,
      onSurface: DeckColors.ink,
      outline: DeckColors.line,
      error: Color(0xFFBA3F38),
    ),
    appBarTheme: AppBarTheme(
      backgroundColor: Colors.white,
      surfaceTintColor: Colors.transparent,
      foregroundColor: DeckColors.ink,
      elevation: 0,
      scrolledUnderElevation: 0,
      centerTitle: false,
      toolbarHeight: 56,
      titleTextStyle: TextStyle(
        fontFamily: family,
        fontSize: 16,
        fontWeight: FontWeight.w600,
        color: DeckColors.ink,
      ),
      iconTheme: const IconThemeData(size: 22, color: DeckColors.ink),
      systemOverlayStyle: SystemUiOverlayStyle.dark,
    ),
    textTheme: const TextTheme(
      bodyMedium: TextStyle(color: DeckColors.ink, fontSize: 14, height: 1.55),
      bodyLarge: TextStyle(color: DeckColors.ink, fontSize: 16, height: 1.55),
      titleMedium: TextStyle(
        color: DeckColors.ink,
        fontSize: 16,
        fontWeight: FontWeight.w600,
      ),
      headlineLarge: TextStyle(
        color: DeckColors.ink,
        fontSize: 22,
        fontWeight: FontWeight.w600,
        letterSpacing: -.8,
      ),
    ),
    iconTheme: const IconThemeData(size: 22, color: DeckColors.ink),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: const Size(0, 48),
        padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
        shape: shape,
        textStyle: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        minimumSize: const Size(0, 48),
        shape: shape,
        side: const BorderSide(color: DeckColors.line),
        textStyle: const TextStyle(fontSize: 14, fontWeight: FontWeight.w500),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        minimumSize: const Size(44, 44),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
        textStyle: const TextStyle(fontSize: 13, fontWeight: FontWeight.w500),
      ),
    ),
    iconButtonTheme: IconButtonThemeData(
      style: IconButton.styleFrom(
        minimumSize: const Size(44, 44),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    navigationBarTheme: NavigationBarThemeData(
      backgroundColor: Colors.white,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      height: 64,
      indicatorColor: Colors.transparent,
      labelTextStyle: WidgetStateProperty.resolveWith(
        (states) => TextStyle(
          fontSize: 11,
          color: states.contains(WidgetState.selected)
              ? DeckColors.ink
              : DeckColors.muted,
          fontWeight: states.contains(WidgetState.selected)
              ? FontWeight.w600
              : FontWeight.w400,
        ),
      ),
      iconTheme: WidgetStateProperty.resolveWith(
        (states) => IconThemeData(
          size: 23,
          color: states.contains(WidgetState.selected)
              ? DeckColors.ink
              : DeckColors.muted,
        ),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: DeckColors.paper,
      hintStyle: const TextStyle(color: DeckColors.muted, fontSize: 14),
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 16),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide.none,
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: BorderSide.none,
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(14),
        borderSide: const BorderSide(color: DeckColors.muted, width: .8),
      ),
    ),
    bottomSheetTheme: const BottomSheetThemeData(
      backgroundColor: Colors.white,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(26)),
      ),
      clipBehavior: Clip.antiAlias,
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: Colors.white,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(24)),
    ),
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      shape: shape,
    ),
    dividerTheme: const DividerThemeData(color: DeckColors.line, thickness: .7),
    listTileTheme: const ListTileThemeData(
      iconColor: DeckColors.muted,
      contentPadding: EdgeInsets.symmetric(horizontal: 20, vertical: 4),
    ),
  );
}

class AgentGlyph extends StatelessWidget {
  final String agent;
  final double size;
  const AgentGlyph(this.agent, {super.key, this.size = 28});
  @override
  Widget build(BuildContext context) => Container(
    width: size,
    height: size,
    decoration: BoxDecoration(
      color: DeckColors.panel,
      borderRadius: BorderRadius.circular(size * .3),
    ),
    child: Icon(
      agent == 'claude' ? Icons.blur_on_rounded : Icons.terminal_rounded,
      size: size * .56,
      color: DeckColors.ink,
    ),
  );
}

class ConnectionDot extends StatelessWidget {
  final bool connected;
  const ConnectionDot({super.key, required this.connected});
  @override
  Widget build(BuildContext context) => Container(
    width: 6,
    height: 6,
    decoration: BoxDecoration(
      color: connected ? DeckColors.online : DeckColors.muted,
      shape: BoxShape.circle,
    ),
  );
}

/// Two quiet selection surfaces: no rectangular Material splash on the labels.
class DeckViewSwitch extends StatelessWidget {
  final bool recent;
  final ValueChanged<bool> onChanged;
  const DeckViewSwitch({
    super.key,
    required this.recent,
    required this.onChanged,
  });
  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.all(3),
    decoration: BoxDecoration(
      color: DeckColors.panel,
      borderRadius: BorderRadius.circular(13),
    ),
    child: Row(
      children: [
        for (final entry in {
          false: context.tr('项目'),
          true: context.tr('最新会话'),
        }.entries)
          Expanded(
            child: Semantics(
              selected: recent == entry.key,
              child: TextButton(
                onPressed: () => onChanged(entry.key),
                style:
                    TextButton.styleFrom(
                      padding: EdgeInsets.zero,
                      minimumSize: const Size(44, 44),
                      overlayColor: Colors.transparent,
                      splashFactory: NoSplash.splashFactory,
                      shape: RoundedRectangleBorder(
                        borderRadius: BorderRadius.circular(10),
                      ),
                    ).copyWith(
                      side: WidgetStateProperty.resolveWith(
                        (states) => states.contains(WidgetState.focused)
                            ? const BorderSide(
                                color: DeckColors.muted,
                                width: .8,
                              )
                            : BorderSide.none,
                      ),
                    ),
                child: AnimatedContainer(
                  duration: const Duration(milliseconds: 180),
                  curve: Curves.easeOutCubic,
                  constraints: const BoxConstraints(minHeight: 44),
                  alignment: Alignment.center,
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 9,
                  ),
                  decoration: BoxDecoration(
                    color: recent == entry.key
                        ? Colors.white
                        : Colors.transparent,
                    borderRadius: BorderRadius.circular(10),
                    boxShadow: recent == entry.key
                        ? const [
                            BoxShadow(
                              color: Color(0x09000000),
                              blurRadius: 4,
                              offset: Offset(0, 1),
                            ),
                          ]
                        : const [],
                  ),
                  child: Text(
                    entry.value,
                    maxLines: 1,
                    style: TextStyle(
                      fontFamily: Theme.of(
                        context,
                      ).textTheme.bodyMedium?.fontFamily,
                      fontSize: 14,
                      fontWeight: FontWeight.w500,
                      color: recent == entry.key
                          ? DeckColors.ink
                          : DeckColors.muted,
                    ),
                  ),
                ),
              ),
            ),
          ),
      ],
    ),
  );
}

class DeckBrandMark extends StatelessWidget {
  final double size;
  const DeckBrandMark({super.key, this.size = 26});
  @override
  Widget build(BuildContext context) => Image.asset(
    'assets/brand/agentdeck-mark.png',
    width: size,
    height: size,
    filterQuality: FilterQuality.high,
    excludeFromSemantics: true,
  );
}
