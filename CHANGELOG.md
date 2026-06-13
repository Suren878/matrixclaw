# Changelog

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
