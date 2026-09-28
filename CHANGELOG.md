# Changelog

## Unreleased

Native runs now use a new agent engine built for long tasks. Design and
as-built notes:
[docs/superpowers/specs/2026-09-23-long-running-agent-design.md](docs/superpowers/specs/2026-09-23-long-running-agent-design.md).

- Moved the native loop into a new engine (`internal/agent`). A run keeps
  going until the work is done. It stops on a budget of steps, active time and
  tokens: 300 steps / 4 h for user messages, 100 / 1 h for subagents, and
  50 / 30 min for automation and wake runs. Tokens are unlimited by default. At
  80% of any limit the model is told to wrap up. At the limit, a final turn
  without tools says what is done and what remains. The old 32-step failure is
  gone.
- Runs now report a stop reason: `done`, `budget_exhausted`, `loop_detected`
  or `context_exhausted`. A loop guard warns after 3 identical calls with the
  same result and stops the run after 5. Polling that returns new output does
  not count. Replies cut by the output limit are continued, up to 3 times in a
  row.
- Added `/continue`, which starts a fresh-budget run after one that stopped
  early (the TUI shows a hint; Telegram shows a Continue button). Added
  `/budget [steps N | time 2h | tokens N | tokens off | reset]` for
  per-session overrides. Engine notes appear as system notes in the TUI and
  Telegram.
- Reworked context handling. History is cut at structured boundaries ordered
  by a per-database message `seq` instead of emoji text markers. Old bulky tool
  results are hidden from the request at 60% of the window. At 80% the older
  history is summarised with the run's own request prefix, so the summary is
  served from the cache; the assignment and steer messages are kept verbatim.
  The system prompt stays fixed during a run, and changing state (todo list,
  warnings, recovery notices) arrives as context notes instead. Tool results
  over about 8k tokens are saved to a file in the session directory, and the
  model gets the head, the tail and the path.
- Every model's context window is capped at 200k tokens by default
  (`daemon.context_window_cap`). An optional `daemon.compact_model` names a
  cheaper model for summaries. A provider's context-overflow error now forces
  a summary and one retry instead of failing the run.
- Added prompt caching on native Anthropic (system prompt, tools and the latest
  turns) and on Claude models through OpenRouter, plus `prompt_cache_key` for
  OpenAI and Codex. `/usage` shows runs, steps, prompt, cache read/write,
  output and reasoning tokens, and the cache hit rate. Every model generation
  is recorded in `run_steps`.
- Updated the provider contract: every adapter reports a normalised stop reason
  and normalised usage. Output limits come from the provider config, then the
  model catalog when it is lower, then a 16k default. A limit is raised once
  when it cuts a reply off before any text or in the middle of a tool call.
  Whole-request timeouts were replaced by a 120 s stream idle timeout.
  OpenAI-compatible streams request usage. Gemini streams over SSE, converts
  tool schemas to its Schema subset, batches function responses and returns
  thought signatures. Codex requests encrypted reasoning and sends it back
  between steps. Native Anthropic gained real tool use, thinking replay and
  stop reasons.
- Changed approvals: a denial no longer fails the run. The model receives
  `User denied: <reason>` and continues; the TUI (`r`) and Telegram
  ("Deny with reason") can attach a reason. In one reply, a mutating call that
  waits for approval holds back every call after it, while the calls before it
  still run. The run resumes once every approval is decided.
- Added permission rules (`allow`, `ask`, `deny`, per session or global) for
  bash commands, paths, domains and MCP tools. They apply to runs, the tools
  API, voice and the MCP server alike. Approval prompts offer "Always allow"
  with a suggested rule such as `bash: go test:*`. `/permissions add|delete`
  manages rules. Global rules, the permission mode and external agent sessions
  can be changed or started only from the TUI and the Telegram owner chat.
  Other chats cannot use sessions that run tools unattended. Telegram's
  in-memory auto-approval was removed.
- The tool calls of one reply now run in parallel, up to 8 at once. Calls that
  touch the same working directory or MCP server take turns, across runs too.
  `daemon.model_concurrency` (default 4) bounds model requests across all
  runs.
- Replaced Planning Mode with a todo list: `todo_write`, a TUI side panel
  (`ctrl+n`), the Telegram run status message and `/todo`. A run that stops
  with open items is asked once to finish them. `/plan` and the plan tools are
  gone. See [docs/TODO.md](docs/TODO.md).
- Added durable background tasks. `bash` has a timeout (default 600 s, at most
  3600 s) and moves commands still running after 120 s to the background.
  `run_in_background` starts one there directly. Output goes to size-capped
  files, is read with `task_output` and stopped with `task_kill` (replacing
  `job_output` / `job_kill`). `await` parks a run in the new `waiting_events`
  status until its tasks finish, a timer fires or the user writes.
  Finished work in an idle session starts a wake run (at most 20 in a row
  without a user message) and reaches Telegram. `/tasks` lists and stops the
  session's background tasks above the scheduled ones. A session runs at most
  `daemon.background_tasks` (default 8) background commands. After a daemon
  restart, leftover commands are stopped and marked `lost`. Stopping a command
  sends SIGTERM, then SIGKILL after 3 s.
- Replaced `delegate_task`, `spawn_subagent`, `list_subagents` and
  `read_subagent_result` with one `agent` tool: `description`, `prompt`,
  `background`, `isolation` (`shared` or `worktree`), `readonly`, `runtime` and
  `model`. Read-only, worktree and background children run in parallel.
  Read-only Codex and Claude Code children cannot write. A session runs at most
  `daemon.background_agents` (default 4) background subagents. A child's
  background commands stop when it finishes, and a blocking child's time does
  not count against the parent's budget. The TUI shows agent calls as subagent
  cards.
- Messages sent while a run is active now steer it by default (TUI, Telegram,
  the iOS app and the API without `busy_mode`). They also wake a run that is
  waiting for events. `/queue` and `/busy` still pick another mode in the TUI.
- Canceling or interrupting a run now also stops the background commands and
  subagents it started.
- The daemon's startup no longer rebuilds the message search index on every
  start, which took about 30 s on large databases. The index is now keyed by
  message `seq`, and search rows of deleted messages are removed.
- Made native turns and Telegram delivery sturdier. Generation retries happen
  before any output or tool dispatch. Run checkpoints and crash recovery are
  more thorough, and interrupted runs are rescheduled while the daemon runs.
  Telegram draft streaming, flood waits and chunk delivery are retry-safe.
- Telegram shows one silent, edited status message per run (state, step
  n/limit, running tool, background tasks, todo list) instead of one message
  per tool call, and loads only new run messages by `seq` on each delivery.
- Fixed module context (storage, MCP, skills) dropping out of the assistant
  prompt after a daemon reload.
- The iOS package decodes unknown run statuses as `.unknown`. It also decodes
  stop reasons, continuations, run triggers and message origin.

New `setup.json` keys under `daemon` (all optional; 0 or absent keeps the
default):

```json
{
  "daemon": {
    "budgets": {
      "user":       {"steps": 300, "active_time": "4h",  "tokens": 0},
      "subagent":   {"steps": 100, "active_time": "1h",  "tokens": 0},
      "automation": {"steps": 50,  "active_time": "30m", "tokens": 0}
    },
    "context_window_cap": 200000,
    "compact_model": {"provider": "", "model": ""},
    "model_concurrency": 4,
    "background_tasks": 8,
    "background_agents": 4
  }
}
```

### Upgrade notes (breaking)

- **Back up the database before upgrading.** The migrations are one-way, and
  an older binary cannot use the migrated database. Downgrading is not
  supported. The daemon can keep running during the backup (use your
  `daemon.db_path` if it is not the default):

  ```bash
  sqlite3 ~/.local/state/matrixclaw/matrixclaw.db \
    ".backup '$HOME/matrixclaw-before-upgrade.db'"
  ```

- The first start after the upgrade migrates the database once: message
  `seq` backfill, search index rekey, and table moves. This can take tens of
  seconds on a large database.
- Only one daemon may use a data directory. The daemon holds
  `matrixclawd.lock` next to the database, and a second daemon on the same data
  exits with an error.
- Tables removed on open: `session_plan_items`, `plan_runs` and
  `session_goals` (existing plans are not migrated); `run_usage` (moved to
  `run_steps`); and `subagent_tasks` (copied into `tasks`). The message search
  table is rebuilt. New tables: `run_steps`, `run_checkpoints`, `run_wakeups`,
  `permission_rules`, `session_todos`, `session_budgets`,
  `session_engine_state` and `tasks`.
- The store opens SQLite with `synchronous = NORMAL` under WAL. A daemon crash
  loses nothing, but a power loss or OS crash can lose the last commits since
  the previous checkpoint.
- Removed tools: `delegate_task`, `spawn_subagent`, `list_subagents`,
  `read_subagent_result`, `job_output`, `job_kill` and the plan tools. Removed
  commands: `/plan`. Old plan-runner prompts in a history now reach the model as
  ordinary user messages.
- API changes:
  - Usage records and summaries replace `input_tokens`, `total_tokens` and
    `cached_tokens` (and record `id`, `message_id`, `created_at`) with `steps`,
    `prompt_tokens`, `cache_read_tokens`, `cache_write_tokens` and
    `updated_at`.
  - `GET` and `DELETE /v1/sessions/{id}/todo` replace the `/plan` endpoints.
    The `todo.updated` event replaces `plan.updated`.
  - Runs gain `trigger`, `continues_run_id`, `stop_reason` and the
    `waiting_events` status. `GET /v1/runs/{id}/steps` lists a run's
    generations; `GET /v1/runs/{id}/progress` returns its budget steps used,
    its step limit and the session's running background tasks.
  - Messages gain `seq`, `origin` and `compaction`. `GET /v1/messages` accepts
    `after_seq`, and a message with `continue: true` continues the latest run.
  - `POST /v1/sessions/{id}/clear` clears the context.
  - `GET` and `PUT /v1/sessions/{id}/budget` read and set the budget overrides.
  - `GET` and `POST /v1/sessions/{id}/permission-rules` and
    `DELETE /v1/permission-rules/{id}` manage permission rules.
  - Approval resolution accepts `reason` and `always` (`session` or `global`),
    and approvals carry `reason` and `suggestion`.
  - `GET /v1/sessions/{id}/tasks`, `GET /v1/tasks/{id}` and
    `POST /v1/tasks/{id}/cancel` handle background tasks. The `task.updated`
    event reports their changes.
  - Compact and clear requests during an active run return 409. Messages from
    restricted clients into sessions that run tools unattended return 403.
- Optional: once the upgrade has run, reclaim the space freed by the dropped
  tables. With the daemon stopped, run
  `sqlite3 ~/.local/state/matrixclaw/matrixclaw.db 'VACUUM;'`. On one real
  database this went from 65.8 MB to 12.8 MB.

## v0.1.19

- Improved provider selection in Telegram and the shared control plane: choosing
  a configured provider now immediately offers its model catalog when multiple
  models are available, and provider edits keep the updated session selection.
- Made image delivery model-aware so text-only OpenAI-compatible models no
  longer receive unsupported `image_url` message parts. Live model modalities
  and conservative static rules now determine image support, while unavailable
  images remain visible as attachment notices instead of aborting later runs.
- Increased the OpenAI-compatible request timeout for slower inference servers
  and isolated workflow-engine SQLite state from the main message database to
  prevent workflow pollers from blocking normal session writes.
- Blocked model file tools from reading MatrixClaw setup files, daemon
  environment files, legacy provider/client credential files, and their backup
  copies; local runtime/config files are also excluded from Git.
- Added regression coverage for provider/model switching, text-only image
  payloads, long-running requests, workflow database isolation, and credential
  file protection. CI and release verification now run the complete Go test
  suite before building artifacts.

## v0.1.18

- Added OpenAI Realtime speech-to-speech support with `gpt-realtime-2.1`,
  streaming audio, server VAD and interruption handling, transcripts, tool
  calls, provider setup, and transparent 16 kHz to 24 kHz input resampling.
- Made Telegram attachments resilient: unsupported image formats are preserved
  as temporary files with a clear supported-format notice, oversized downloads
  are bounded, and missing or expired images no longer abort later conversation
  runs.
- Improved telephony audio startup and diagnostics with an outbound caller-audio
  gate, playback preroll, optional bounded debug WAV capture, safer debug-file
  permissions, and persistent playback-worker error reporting.
- Hardened web fetching against DNS rebinding and redirects to private targets,
  expanded blocked IPv4 and IPv6 ranges, and bounded fetched response bodies.
- Fixed terminal selection across multiple chat items and isolated subagent
  worktrees for repositories that share the same directory name.
- Split daemon execution, realtime voice, telephony tools, and Skills control
  plane code into smaller focused modules, while cleaning up obsolete helpers
  and normalizing wrapped-error handling.
- Updated the Go baseline to 1.26.5 and refreshed networking, rendering,
  telemetry, image, and supporting dependencies.

## v0.1.17

- Hardened the telephony gateway runtime by splitting call lifecycle,
  ARI/event-hub, RTP bridging, playback, recording, inbound/outbound, cleanup,
  reporting, and MatrixClaw API code into smaller focused modules.
- Fixed telephony realtime playback and made call shutdown safer with root
  context cancellation, active-call pruning, setup-aware tool visibility, shared
  phone-number normalization, and stronger recording format validation.
- Improved call recording finalization by retrying stored-recording downloads
  with backoff, preserving ARI stop/download diagnostics, and logging nearby
  stored-recording candidates when Asterisk does not expose the expected file.
- Guarded managed browser setup from unsafe shell execution and made browser
  runtime startup use the managed Chromium executable consistently.
- Improved long-running session reliability by failing orphaned running runs,
  routing background workers through `safego`, recovering remaining background
  panics, refreshing Telegram typing indicators during active runs, and
  surfacing external runtime and subagent aftermath store errors.
- Split realtime voice and local voice control-plane flows into smaller option,
  setup, status, provider-selection, runtime, and action modules.
- Added architecture, browser, Telegram, testing, and refactoring documentation
  that records the current module boundaries and cleanup decisions.
- Reset the legacy broad test suite and added focused coverage for run
  recovery, subagent lifecycle, Telegram typing, managed browser guards,
  realtime/voice setup inputs, telephony phone normalization, and recording
  retry behavior.

## v0.1.16

- Added provider-neutral realtime voice setup for Gemini Live and Grok Voice,
  including provider selection, API key validation, model/voice/language
  controls, and provider-specific status in the control plane.
- Added xAI Grok Voice Agent support for realtime speech-to-speech sessions,
  including language hints, manual audio turn commits, tool-call routing, and
  transcript handling tuned for cumulative Grok transcription events.
- Refactored control-plane navigation around consistent Back/Close behavior so
  menus, pickers, status views, and action dialogs return to their parent
  surface instead of leaking stale pickers or collapsing the menu stack.
- Added the optional `matrixclaw-telephony-gateway` binary for self-hosted
  Asterisk/SIP deployments, bridging ARI `externalMedia` RTP audio into
  MatrixClaw realtime voice sessions.
- Added approval-gated `telephony_call` tooling, outbound call objectives,
  inbound caller allowlists, phone-specific prompts, final call transcripts,
  post-call reports, and temporary MP3 call recording plumbing.
- Improved telephony runtime stability with faster inbound answering, a single
  long-lived ARI app listener, hangup-extension filtering, safer RTP/VAD turn
  handling, and cleanup for realtime close races.
- Updated release packaging, installer, uninstall script, local release build,
  and Homebrew template to include `matrixclaw-telephony-gateway`.
- Documented realtime voice providers, telephony gateway boundaries, SIP/PBX
  deployment assumptions, environment variables, and privacy considerations.

## v0.1.15

- Reworked Telegram around normal private-chat sessions again: `/new`,
  `/sessions`, session use, and session deletion no longer depend on forum
  topics or `message_thread_id` routing.
- Added Telegram inline and guest delivery support so the bot can be invoked
  from other chats, edit the inline placeholder with the assistant answer, and
  answer guest-mode requests through the new Bot API flow.
- Improved Telegram live delivery with draft previews, a compact thinking
  placeholder, clearer running states, persistent inline request recovery, and
  fewer duplicate or stale delivery updates.
- Fixed Telegram voice/TTS routing so generated audio follows the originating
  chat or inline message, while the model only sends extra written text when it
  is actually useful.
- Added Telegram inline location handling so geolocation attached by Telegram is
  passed into the assistant request instead of being lost.
- Added browser module plumbing and managed browser MCP configuration paths,
  including daemon/client/control-plane APIs for browser provider state.
- Expanded storage APIs with base64 byte reads for durable and temporary files,
  and made storage not-found handling use typed errors instead of text matching.
- Made voice and session-LLM error handling rely on typed errors, removing more
  phrase-based diagnostics and locale-sensitive behavior.

## v0.1.14

- Added smart web research with `web_research`, `web_research_ask`, and
  `web_research_status`, returning compact answers, facts, sources, warnings,
  next actions, and reusable `research_id` values.
- Added the shared `internal/work` storage layer with `work_jobs`,
  `work_artifacts`, and `work_facts` for heavy assistant jobs and large runtime
  artifacts.
- Moved new web research persistence onto the shared work tables, leaving raw
  page text, HTML, browser snapshots, and extraction artifacts out of the main
  provider prompt by default.
- Added deterministic web extraction for titles, snippets, page text chunks,
  schema-style facts, ratings, and review counts, with browser fallback support
  for dynamic or sparse pages.
- Updated `web_fetch` compatibility behavior: task mode now routes through web
  research extraction, while plain URL fetch returns compact diagnostics and
  artifact references instead of long raw excerpts.
- Kept `web_search` as a compact compatibility search tool and updated runtime
  guidance to prefer `web_research` for current, source-backed answers.
- Added MCP browser adapter wiring so web research can use a configured browser
  MCP server as its fallback renderer.
- Mirrored subagent lifecycle state into the shared work layer and made
  `read_subagent_result` return compact job summaries and refs instead of child
  transcript excerpts.
- Refactored web tool wiring around a single injected web service adapter,
  removing hidden global state between `web_fetch`, `web_search`, and
  `web_research`.

## v0.1.13

- Added live Terminal subagent cards for `delegate_task` and `spawn_subagent`,
  with Matrix-style codenames, running/completed/failed/canceled states,
  expandable task text, and metadata previews.
- Added async subagent state merging so spawned background agents keep updating
  the original tool card after the spawn result is returned.
- Added queued busy input behavior: Enter queues while the assistant is busy,
  with `/queue`, `/steer`, `/interrupt`, and `/busy` commands for explicit
  control.
- Reworked the TUI status line to show the main model phase first and append
  active subagent or queued-input details.
- Added `/context clear`, clear markers, compact markers, context blocks, and
  corrected header token estimates based on effective post-clear context.
- Added persistent session input storage for queued/steered/interrupted user
  messages.
- Refined chat scrolling, viewport restoration, command pickers, permission
  rendering, and subagent/tool previews.
- Split local voice runtime management into Piper, Supertonic, and Whisper.cpp
  drivers with install/status coverage.
- Added README release highlights for the live-subagents/context release.

## v0.1.12

- Added MatrixClaw subagents through `delegate_task`, with native child-session
  runs and external Codex/Claude Code runtime options.
- Added model-facing subagent guidance so assistants know when to delegate
  bounded work and which runtimes are available.
- Added subagent task persistence, parent/child session links, result delivery,
  and terminal rendering for delegated work.
- Added durable memory and assistant-facing `memory` tools, plus API,
  daemon-client, and controlplane support.
- Added session model/title improvements and external-agent runtime discovery
  updates.
- Added TUI self-restart support after daemon updates.

## v0.1.11

- Refactored the Terminal UI stack by moving shared command-menu components into
  reusable surface components.
- Reworked command, picker, prompt, confirm, form, and info dialogs for more
  consistent rendering and navigation.
- Added dialog occlusion handling and simplified controlplane picker
  presentation.
- Tightened setup screen rendering, provider editing, storage/temp views, and
  module picker behavior.
- Updated CI lint configuration for Go 1.26 and limited golangci-lint to new
  issues.

## v0.1.10

- Added daemon-first local voice modules: Text to Speech now supports Piper and
  Supertonic 3, while Speech to Text supports Whisper.cpp through the same
  shared module UI used by Terminal and Telegram.
- Added local voice runtime installation with `install.sh --voice-runtime` and
  `scripts/install_voice_runtime.sh` for Piper, Supertonic, Whisper.cpp, and
  ffmpeg.
- Added Run Per Task and Always Running modes for local voice providers. Run Per
  Task is the memory-saving default; Always Running keeps Piper, Supertonic, or
  Whisper.cpp warm for lower startup latency.
- Added online catalog-backed voice/model selection: Piper voices by language,
  Supertonic voice styles and language modes, and Whisper.cpp model tiers from
  `tiny` through `large-v3`.
- Added local Whisper.cpp speech-to-text execution through `whisper-cli` and
  `whisper-server`, with STT request limits sized for Telegram voice/audio
  uploads.
- Added voice status screens with installed storage, selected provider/model,
  runtime mode, and live RAM usage for managed local processes.
- Fixed local Piper text-to-speech so longer responses are generated without
  returning only the first chunk.
- Added Telegram voice delivery for TTS tool results and `/tts`, with generated
  audio saved into Matrixclaw Storage under `telegram/audio/`.
- Fixed Telegram TTS/STT daemon calls to use the long voice-runtime timeout
  instead of the short JSON timeout.
- Added storage/temp file documentation and kept Telegram-downloaded files in
  Matrixclaw storage with collision-safe names.
- Documented daemon-first architecture, local voice run modes, storage/temp
  files, Telegram voice/file flow, and open-source voice runtime installation.

## v0.1.9

- Added OpenAI Codex subscription OAuth provider support and provider-login CLI
  plumbing.
- Added Telegram image/document upload handling backed by Matrixclaw storage,
  including temporary files and explicit save/delete controls.
- Added Telegram voice/audio transcription and text-to-speech delivery flows.
- Added daemon API and controlplane support for local voice modules.
- Added MCP, storage, automation, provider, and module command refinements.
- Added daemon stop controls, Piper runtime management, and process status
  helpers for local runtime processes.
- Improved Ubuntu install/runtime discovery, automation delivery fan-out to
  Telegram, and voice runtime activation guards.
- Documented Storage and Voice modules.

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
