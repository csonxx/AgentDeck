# AgentDeck

This is a Go + Flutter project. On 2026-10-10 the owner changed the repository to git@gitlab.ttyuyin.com:azeroth/agentdesk.git, superseding the previous GitHub destination. Keep origin pointed at this GitLab repository. Commit and push only when requested; the owner authorized cmp for this repository migration and README rewrite.

The app is a trusted-owner workstation companion. On 2026-10-08 the owner authorized project-scoped Codex/Claude message sending, new conversations, interruption and explicit tool approval. Keep the public HTTPS protocol separate from the owner-only Unix admin socket. Never expose shell execution or silently bypass certificate/public-key checks. All persistent event/device/media values and phone cache data must remain encrypted.

Use `scripts/verify.sh` for Go race/vet, Flutter analysis, wire compatibility, synthetic live HTTPS integration and UI tests. Preserve distinct claims: log flush synchronization, native event deltas, build success, installed app, and actual public deployment. Demo screenshots must be identified as demo content.

Android/iOS/macOS live integration is native; the current client uses dart:io and is not a web app. New agent adapters should normalize presentation records and exclude system/context/authentication records. Never print pairing tokens, private keys or user session contents in logs or test reports.
