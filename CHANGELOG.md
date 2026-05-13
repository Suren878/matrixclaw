# Changelog

## v0.1.4

- Added Codex as an external agent runtime with app-server session attachment,
  CLI discovery/start commands, and daemon API support.
- Moved session architecture toward runtime-scoped settings: sessions now carry
  runtime, provider/model, and permission mode state.
- Added runtime-aware session creation for MatrixClaw and Codex in the shared
  controlplane used by TUI and Telegram.
- Moved Provider and Permission Mode out of the top-level menu and into
  session-scoped actions.
- Fixed Telegram provider switching callbacks and DeepSeek/OpenAI-compatible
  reasoning-content handling.

## v0.1.3

- Replaced the repository-hosted README demo GIF with a GitHub attachment link
  and removed the large media file from git history.
- Changed empty-provider setup continuation to use the shared confirmation card.

## v0.1.2

- Fixed macOS installer compatibility by removing a GNU-specific `sed` script
  from latest-release detection.
- Fixed installer cleanup after download failures so network errors do not
  trigger a secondary `tmp: unbound variable` failure.
- Added `matrixclaw tui [WORKDIR]` for opening a terminal session rooted at an
  explicit project directory, including external macOS volumes.
- Improved filesystem tool errors to show the active working directory when a
  requested path is outside the session root.

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
