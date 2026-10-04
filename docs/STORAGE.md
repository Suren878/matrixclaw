# Storage

Where matrixclaw keeps its data, and how the storage module handles files
that should outlive a chat turn.

## Files and directories

| Path (default) | What it is |
|---|---|
| `~/.config/matrixclaw/setup.json` | setup (`MATRIXCLAW_SETUP_PATH` moves it); written with mode 0600 |
| `~/.config/matrixclaw/daemon.env` | environment for the user service, next to setup.json |
| `~/.local/state/matrixclaw/matrixclaw.db` | SQLite database (`daemon.db_path` in setup.json, or `MATRIXCLAW_DB_PATH`, which wins) |

Next to the database (the data directory):

| Path | What it is |
|---|---|
| `matrixclawd.lock` | held by the running daemon; a second daemon on the same data exits |
| `storage/` | storage module root (below) |
| `sessions/<session id>/tool-output/` | full tool results too large for the model's context (the model gets head, tail and path) |
| `sessions/<session id>/tasks/<task id>.log` | background command output, mode 0600, capped at 20 MB (keeps the first 1 MB and the newest part) |
| `skills/` | skills library: `installed`, `quarantine`, `archive`, `plugins`, `drafts` |
| `runtime/context-windows.json` | cached model context windows |

Session directories are removed with their session; `/clear` removes the tool
outputs of the cleared history.

Under the XDG state directory (`$XDG_STATE_HOME`, else `~/.local/state`),
independent of the database path:

| Path | What it is |
|---|---|
| `matrixclaw/runtime/` | local voice and browser runtimes (`MATRIXCLAW_RUNTIME_DIR`) |
| `matrixclaw/local/` | local voice models (`MATRIXCLAW_LOCAL_DIR`) |
| `matrixclaw/auth/` | ChatGPT (Codex) login tokens (`MATRIXCLAW_AUTH_DIR`) |

## Database

`internal/store` opens SQLite with WAL, `synchronous = NORMAL`, foreign keys
and a 5 s busy timeout. A daemon crash loses nothing; a power loss or OS crash
can lose the last commits since the previous checkpoint.

The schema is `internal/store/migrations/001_init.sql` plus idempotent upgrade
steps in `internal/store/schema*.go`, applied on every open. Upgrades are
one-way: an older binary cannot use an upgraded database. Back up before
upgrading (the daemon may keep running):

```bash
sqlite3 ~/.local/state/matrixclaw/matrixclaw.db \
  ".backup '$HOME/matrixclaw-backup.db'"
```

Tables:

| Area | Tables |
|---|---|
| Sessions | `sessions`, `messages` (ordered by `seq`), `message_fts` (search), `client_bindings`, `session_inputs` (queued and steer messages), `memories` |
| Runs | `runs`, `run_steps` (one row per model generation), `run_checkpoints`, `run_wakeups` |
| Approvals and rules | `approvals`, `permission_rules` |
| Per-session engine state | `session_budgets`, `session_engine_state`, `session_todos` |
| Background work | `tasks` (shell commands and subagents) |
| Clients | `client_deliveries` |
| Automation | `automation_jobs`, `automation_fires` |
| External agents | `external_agent_sessions` |
| Skills (`internal/skills`) | `skills`, `skill_fts`, `skill_sessions`, `skill_plugin_mcp_candidates` |

## Storage module

The storage root is `storage/` next to the database; its metadata lives in
`storage/.matrixclaw/`. Clients reach it through the daemon API, the model
through tools.

### Stored files

Durable files with a relative path, title, MIME type, tags, size and
timestamps. Manage them with `/modules storage` in the TUI or Telegram. Model
tools: `storage_save`, `storage_list`, `storage_read` (text files),
`storage_update_metadata`, and `storage_delete`, which asks for approval.

### Temporary files

Uploads and attachments that may not be worth keeping, under
`storage/temporary/`. Defaults: auto-cleanup on, 7-day TTL, 5 GB total cap.
Keep one with `/modules storage` -> Temporary Files -> Save, or let the model
call `storage_save_temp`; both copy it into stored files and remove the
temporary entry.

### Size limits

Each stored or temporary file is limited to 25 MB; temporary files also obey
the total cap. Large project files should stay in the workspace and be read by
path.

## Telegram uploads

Telegram files get a local storage path before the model sees them:

- **Images** (photos and JPEG, PNG, GIF or WebP documents) are saved as
  temporary files under `telegram/images/`, and the session receives an image
  part that references that path. Other image formats, SVG included, are saved
  under `telegram/files/` with a notice that they cannot be opened as images;
  no run starts for them.
- **Documents** are saved as temporary files under `telegram/files/`; the reply
  names the path and points to `/modules storage` -> Temporary Files.
- **Voice and audio** (up to 25 MB) go through the configured speech-to-text
  provider; the transcript is shown in Telegram and sent to the session as the
  user's message.
- **Generated speech** (`/tts` and the TTS tool) is sent back as a voice
  message and archived in stored files under `telegram/audio/` with the tags
  `telegram`, `generated`, `audio`, `tts`.
