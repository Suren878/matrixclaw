# Changelog

## v0.1.1

- Added `Kimi (Subscription)` provider for Kimi Code members using the
  OpenAI-compatible `https://api.kimi.com/coding/v1` endpoint and stable
  `kimi-for-coding` model.
- Improved provider setup and TUI provider editing: model pickers now open on
  the active model, tool-use pickers no longer show a misleading active marker,
  and provider edit dialogs keep consistent back/save navigation.
- Refreshed README positioning around `matrixclaw` as local personal AI
  infrastructure and moved README media assets under `.github/assets`.

## v0.1.0

- Added daemon-backed terminal and Telegram clients.
- Added SQLite-backed sessions, runs, approvals, files, and client deliveries.
- Added setup flow for providers, daemon settings, Telegram, timezone, and assistant profile.
- Added automation jobs for reminders and scheduled AI tasks.
- Added release-readiness hardening for automation fires, SSE fan-out, Telegram monitoring, and daemon bind safety.
- Simplified setup/provider and storage API contracts to reduce daemon API/client drift.
