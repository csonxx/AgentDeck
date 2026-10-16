import 'package:flutter/services.dart' show appFlavor;

// Flutter supplies this at compile time through --flavor. Tests and existing
// non-Android builds retain the full feature set when no flavor is specified.
const appEdition = appFlavor == 'lite' ? 'lite' : 'voice';
const hasVoiceInput = appEdition == 'voice';
