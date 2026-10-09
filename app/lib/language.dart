import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'core/vault.dart';
import 'ui_catalog.dart';
import 'backend_messages.dart';

enum AppLanguage {
  english('en', 'English', Locale('en')),
  simplifiedChinese(
    'zh-Hans',
    '简体中文',
    Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hans'),
  ),
  traditionalChinese(
    'zh-Hant',
    '繁體中文',
    Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hant'),
  ),
  russian('ru', 'Русский', Locale('ru'));

  final String code, label;
  final Locale locale;
  const AppLanguage(this.code, this.label, this.locale);
  static AppLanguage fromCode(String? code) =>
      values.firstWhere((v) => v.code == code, orElse: () => english);
}

/// A device preference, independent of pairing and never sent to the server.
class LanguageController extends ChangeNotifier {
  final DeviceVault vault;
  AppLanguage _language;
  bool _changed = false, _disposed = false;
  Future<void> _queue = Future.value();
  LanguageController(this.vault, {AppLanguage initial = AppLanguage.english})
    : _language = initial;
  AppLanguage get language => _language;
  Future<void> load() async {
    try {
      final saved = await vault.loadLanguage();
      if (_changed || _disposed) return;
      _language = AppLanguage.fromCode(saved);
      notifyListeners();
    } catch (_) {
      // A missing/unreadable preference uses English, without affecting pairing.
    }
  }

  Future<void> select(AppLanguage value) {
    _changed = true;
    final save = _queue.then((_) async {
      await vault.saveLanguage(value.code);
      if (!_disposed) {
        _language = value;
        notifyListeners();
      }
    });
    _queue = save.catchError((Object _) {});
    return save;
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}

class LanguageScope extends InheritedNotifier<LanguageController> {
  const LanguageScope({
    super.key,
    required LanguageController controller,
    required super.child,
  }) : super(notifier: controller);
  static LanguageController? of(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<LanguageScope>()?.notifier;
}

class UiStrings {
  static final _slots = RegExp(r'\{(\d+)\}');
  final AppLanguage language;
  const UiStrings(this.language);
  static const delegate = _UiDelegate();
  static const delegates = <LocalizationsDelegate<dynamic>>[
    delegate,
    GlobalMaterialLocalizations.delegate,
    GlobalWidgetsLocalizations.delegate,
    GlobalCupertinoLocalizations.delegate,
  ];
  static final supportedLocales = AppLanguage.values
      .map((v) => v.locale)
      .toList(growable: false);

  static final _backendTemplates = backendUiMessages.keys
      .where((key) => _slots.hasMatch(key))
      .map(_MessageTemplate.new)
      .toList(growable: false);
  static final _noticeTemplates = backendNoticeKeys
      .where((key) => _slots.hasMatch(key))
      .map(_MessageTemplate.new)
      .toList(growable: false);

  String _render(String key, List<Object?> args) {
    final row = uiCatalog[key] ?? backendUiMessages[key];
    final template = language == AppLanguage.simplifiedChinese || row == null
        ? key
        : row[switch (language) {
            AppLanguage.english => 0,
            AppLanguage.traditionalChinese => 1,
            AppLanguage.russian => 2,
            _ => 0,
          }];
    // Single pass: user-supplied values containing {0} remain untouched.
    return template.replaceAllMapped(_slots, (m) {
      final index = int.parse(m[1]!);
      return index < args.length ? '${args[index]}' : m[0]!;
    });
  }

  String text(String key, [List<Object?> args = const []]) {
    if (args.isNotEmpty ||
        uiCatalog.containsKey(key) ||
        backendUiMessages.containsKey(key)) {
      return _render(key, args);
    }
    return _match(key, _backendTemplates) ?? key;
  }

  String? _match(String message, List<_MessageTemplate> templates) {
    if (message.length > 8192) return null;
    for (final template in templates) {
      final match = template.pattern.firstMatch(message);
      if (match == null) continue;
      final args = [for (var i = 1; i <= match.groupCount; i++) match[i]!];
      if (template.key == '图片暂不可用：{0}' &&
          (uiCatalog.containsKey(args[0]) ||
              backendUiMessages.containsKey(args[0]))) {
        args[0] = _render(args[0], const []);
      }
      return _render(template.key, args);
    }
    return null;
  }

  /// Agent-authored notices and all message/tool bodies retain their text.
  String notice(String message) => backendNoticeKeys.contains(message)
      ? _render(message, const [])
      : _match(message, _noticeTemplates) ?? message;

  String count(int count, {required bool chats}) {
    final unit = switch (language) {
      AppLanguage.english =>
        chats
            ? (count == 1 ? 'chat' : 'chats')
            : (count == 1 ? 'project' : 'projects'),
      AppLanguage.simplifiedChinese => chats ? '个会话' : '个项目',
      AppLanguage.traditionalChinese => chats ? '個會話' : '個專案',
      AppLanguage.russian => (() {
        final n = count.abs();
        final form = n % 100 >= 11 && n % 100 <= 14
            ? 2
            : (n % 10 == 1 ? 0 : (n % 10 >= 2 && n % 10 <= 4 ? 1 : 2));
        return (chats
            ? ['беседа', 'беседы', 'бесед']
            : ['проект', 'проекта', 'проектов'])[form];
      })(),
    };
    return '$count $unit';
  }

  String error(Object? error) {
    final message = switch (error) {
      FormatException e => e.message,
      FileSystemException e => e.message,
      HttpException e => e.message,
      _ => error.toString(),
    };
    final translated = text(message);
    if (translated != message ||
        uiCatalog.containsKey(message) ||
        backendUiMessages.containsKey(message)) {
      return translated;
    }
    for (final prefix in [
      'FormatException: ',
      'HttpException: ',
      'FileSystemException: ',
    ]) {
      if (message.startsWith(prefix)) {
        final body = message.substring(prefix.length);
        final translatedBody = text(body);
        if (translatedBody != body ||
            uiCatalog.containsKey(body) ||
            backendUiMessages.containsKey(body)) {
          return translatedBody;
        }
      }
    }
    return message;
  }
}

class _MessageTemplate {
  final String key;
  late final RegExp pattern = RegExp(
    '^${key.split(UiStrings._slots).map(RegExp.escape).join(r"([\s\S]*?)")}\$',
  );
  _MessageTemplate(this.key);
}

class _UiDelegate extends LocalizationsDelegate<UiStrings> {
  const _UiDelegate();
  @override
  bool isSupported(Locale locale) =>
      ['en', 'zh', 'ru'].contains(locale.languageCode);
  @override
  Future<UiStrings> load(Locale locale) => SynchronousFuture(
    UiStrings(
      locale.languageCode == 'zh'
          ? (locale.scriptCode == 'Hant'
                ? AppLanguage.traditionalChinese
                : AppLanguage.simplifiedChinese)
          : (locale.languageCode == 'ru'
                ? AppLanguage.russian
                : AppLanguage.english),
    ),
  );
  @override
  bool shouldReload(_UiDelegate old) => false;
}

extension UiTranslation on BuildContext {
  UiStrings get strings =>
      Localizations.of<UiStrings>(this, UiStrings) ??
      const UiStrings(AppLanguage.english);
  String tr(String key, [List<Object?> args = const []]) =>
      strings.text(key, args);
  String trError(Object? error) => strings.error(error);
}
