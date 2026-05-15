# Changelog

## v0.1.8

- Added session capabilities so Matrixclaw and external-agent sessions expose
  only the controls that apply to their runtime.
- Marked Provider, Permission Mode, and Planning Mode as Matrixclaw-only for
  Codex sessions, with explicit explanations instead of silent no-op behavior.
- Refined the New Session picker copy for Matrixclaw and Codex runtime choices.
- Reworked Codex module options into editable Path and Enabled controls, with
  Enabled opening a standard picker and Path using the standard text prompt.
- Fixed external-agent path updates so changing the Codex binary path does not
  accidentally reset the enabled state.
- Renamed user-facing Goal/Plan labels to Planning Mode.

## v0.1.7

- Added a persistent Planning Mode runner with SQLite-backed checkpoints for
  current item, last run, status, attempts, and errors.
- Changed plan execution from "model runs the whole plan" to one executable
  item at a time, with the daemon selecting the next leaf task.
- Added task/subtask execution semantics: parent items with open children are
  treated as sections and auto-close when all children are terminal.
- Made successful plan-run steps close the checkpointed item in core, reducing
  reliance on the model remembering to call `plan_update_item`.
- Kept blocked plan steps open and recorded blocked runner state instead of
  incorrectly marking work done.
- Improved TUI planning panel behavior, auto-run continuation, and plan summary
  display during multi-item execution.
- Documented Planning Mode architecture in `docs/PLANNING.md`.

## v0.1.6

- Added TUI startup update checks against the latest GitHub Release, with a
  shared confirmation dialog and `matrixclaw update` CLI commands.
- Added update installation through the release installer and a follow-up TUI
  prompt to restart the daemon so the service binary is refreshed.
- Added `/modules -> External Agents` management with enable/disable controls,
  installed state, resolved binary path, mode, and version details.
- Moved external-agent daemon wiring into a built-in registry factory so future
  agents can be added without changing core session logic.
- Fixed Server Status back navigation by giving status info dialogs a return
  command and expanding the generic Info back key handling.
- Documented auto-update and external-agent management flows.

## v0.1.5

- Improved Codex external-agent sessions: restored thread resume handling,
  normalized Codex tool activity into shared tool-call events, and preserved
  streamed `text -> tool -> text` ordering.
- Fixed TUI rendering so assistant text before and after tool calls is shown in
  the correct order without reimplementing Codex edits or diffs.
- Restored mouse-wheel scrolling in the terminal chat while keeping keyboard
  copy support for selected chat blocks.
- Cleaned up external-agent runtime plumbing and documentation so future
  runtimes can reuse the same event path.
- Refined TUI and Telegram command-menu parity.

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
