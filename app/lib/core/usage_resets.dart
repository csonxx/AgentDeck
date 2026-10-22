class CodexResetCard {
  final String id, title, description, status, type;
  final DateTime? granted, expires;
  CodexResetCard(Map<String, dynamic> v)
    : id = v['id'] as String,
      title = v['title'] as String? ?? '',
      description = v['description'] as String? ?? '',
      status = v['status'] as String? ?? 'unknown',
      type = v['reset_type'] as String? ?? 'unknown',
      granted = DateTime.tryParse(v['granted_at'] as String? ?? ''),
      expires = DateTime.tryParse(v['expires_at'] as String? ?? '');
  bool get available =>
      status == 'available' &&
      type == 'codexRateLimits' &&
      (expires == null || expires!.isAfter(DateTime.now()));
}

class CodexResetState {
  final int? available;
  final bool detailsKnown, eligible, eligibilityKnown;
  final String account;
  final List<CodexResetCard> cards;
  CodexResetState(Map<String, dynamic> v)
    : available = (v['available_count'] as num?)?.toInt(),
      detailsKnown = v['details_known'] == true,
      eligible = v['eligible'] == true,
      eligibilityKnown = v['eligibility_known'] == true,
      account = v['account_version'] as String? ?? '',
      cards = [
        for (final x in v['credits'] as List? ?? [])
          CodexResetCard(Map<String, dynamic>.from(x as Map)),
      ];
}
