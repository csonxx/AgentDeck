class UsageWindow {
  final String label;
  final double used;
  final int minutes;
  final DateTime? resets;
  UsageWindow(Map<String, dynamic> json)
    : label = json['label'] as String? ?? '',
      used = (json['used_percent'] as num).toDouble(),
      minutes = (json['window_minutes'] as num?)?.toInt() ?? 0,
      resets = DateTime.tryParse(json['resets_at'] as String? ?? '');
}

class AccountUsage {
  final String source;
  final bool stale;
  final DateTime? asOf;
  final List<UsageWindow> windows;
  AccountUsage(Map<String, dynamic> json)
    : source = json['source'] as String? ?? '',
      stale = json['stale'] == true,
      asOf = DateTime.tryParse(json['as_of'] as String? ?? ''),
      windows = [
        for (final row in json['windows'] as List? ?? [])
          UsageWindow(Map<String, dynamic>.from(row as Map)),
      ];
}
