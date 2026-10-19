# Storage

Where matrixclaw keeps its data, and how the storage module handles files
that should outlive a chat turn.

## Files and directories

| Path (default) | What it is |
|---|---|
| `~/.config/matrixclaw/setup.json` | setup (on macOS `~/Library/Application Support/matrixclaw/setup.json`, the OS user config directory; `$XDG_CONFIG_HOME` moves it on Linux, `MATRIXCLAW_SETUP_PATH` moves it); written with mode 0600. A version 3 file from v0.2.0 is read as version 4 and saved as such when the daemon starts or a setup change is saved, the original kept as `setup.json.pre-v4` |
| `matrixclaw/daemon.env` in the same directory | environment for the user service, next to setup.json |
| `~/.local/state/matrixclaw/matrixclaw.db` | SQLite database (`daemon.db_path` in setup.json, or `MATRIXCLAW_DB_PATH`, which wins) |

Next to the database (the data directory):

| Path | What it is |
|---|---|
| `matrixclawd.lock` | held by the running daemon; a second daemon on the same data exits |
| `storage/` | storage module root (below) |
| `sessions/<session id>/tool-output/` | full tool results too large for the model's context (the model gets head, tail and path) |
| `sessions/<session id>/tasks/<task id>.log` | background command output, mode 0600, capped at 20 MB (keeps the first 1 MB and the newest part) |
| `skills/` | skills library: `installed` (trusted), `quarantine`, `drafts` |
| `runtime/context-windows.json` | model metadata learned from provider listings, by provider |
| `checkpoints/<hash>/` | shadow git repository of one work tree, holding its file checkpoints ([CHECKPOINTS.md](CHECKPOINTS.md)) |
| `worktrees/<repo>-<hash>/<task id>/` | git worktree of a worktree subagent, on branch `mc/<task id>`, kept until merged, discarded or 7 days after its task ended ([SUBAGENTS.md](SUBAGENTS.md)) |
| `lsp/lsp-<language>.log` | stderr of the language servers ([LSP.md](LSP.md)), appended |

Session directories are removed with their session; `/clear` removes the tool
outputs of the cleared history.

Under the XDG state directory (`$XDG_STATE_HOME`, else `~/.local/state`),
independent of the database path:

| Path | What it is |
|---|---|
| `matrixclaw/runtime/` | local voice and browser runtimes (`MATRIXCLAW_RUNTIME_DIR`) |
| `matrixclaw/local/` | local voice models (`MATRIXCLAW_LOCAL_DIR`) |
| `matrixclaw/auth/` | ChatGPT (Codex) login tokens and their refresh lock (`MATRIXCLAW_AUTH_DIR`) |

## Database

`internal/store` opens SQLite with WAL, `synchronous = NORMAL`, foreign keys
and a 5 s busy timeout, on one connection that the skills library and
automation share (`SQLiteStore.DB`). A daemon crash loses nothing; a power loss or OS crash
can lose the last commits since the previous checkpoint.

The schema is `internal/store/migrations/001_init.sql` followed by the
numbered migrations listed in `internal/store/schema.go` (`002` adds
`file_checkpoints`; `003` drops `sessions.runtime_id`, which repeated
`kind`, and `external_agent_sessions.approval_policy` and `sandbox`, since an
external agent's permissions follow its session; `004` adds
`session_goals`; `005` adds the subagent columns `tasks.agent_json`,
`output_schema` and `worktree_base`; `006` gives automation jobs
their client's capabilities and delivery address, marking existing Telegram
jobs as receiving deliveries, and drops the unread
`skill_plugin_mcp_candidates`; `007` adds `memories.pending`, marking the
memory suggestions of session reviews); the database records in
`PRAGMA user_version` how many migrations it has applied. Each migration is applied once and in order, to a new
database too. A database newer than the binary is refused.
Upgrades are one-way. Back up before upgrading (the daemon may keep running):

```bash
sqlite3 ~/.local/state/matrixclaw/matrixclaw.db \
  ".backup '$HOME/matrixclaw-backup.db'"
```

### Converting a v0.2.0 database

A database from v0.2.0 or earlier (user_version 0) is converted once, when
the daemon first opens it (`internal/store/legacy_v02.go`):

1. The database is copied to `matrixclaw.db.pre-v1` next to it (an existing
   copy from an interrupted conversion is kept).
2. The upgrade steps v0.2.0 ran on every start run one last time, so any
   v0.1.x database reaches the v0.2.0 shape.
3. In one transaction every table is rebuilt as `001_init.sql` declares it.
   Tables and columns the schema no longer has are dropped: the go-workflows
   tables (`history`, `instances`, `activities`, `attributes`,
   `pending_events`, `schema_migrations`), `web_research_*`, the unused
   `run_checkpoints` columns, `approvals.action` and
   `tasks.result_message_id`. Rows that only older code could read are
   rewritten (client capabilities without `receives_deliveries`, tool results
   with `is_error`, tool results too large for the model's context are cut to
   their head and tail). The version becomes 1.
4. The database is compacted, and `matrixclaw-workflows.db*` and
   `web-research/` next to it are removed.

To roll back, stop the daemon and copy `matrixclaw.db.pre-v1` over
`matrixclaw.db` (removing `matrixclaw.db-wal` and `matrixclaw.db-shm`) and
`setup.json.pre-v4` over `setup.json` (v0.2.0 refuses a version 4 file), then
start the older binary. Delete the copy once the new version works. The next
release drops the conversion, so a database older than v0.2.0 must be opened
by this release first.

Tables:

| Area | Tables |
|---|---|
| Sessions | `sessions`, `messages` (ordered by `seq`), `message_fts` (search), `client_bindings`, `session_inputs` (queued and steer messages), `memories` |
| Runs | `runs`, `run_steps` (one row per model generation), `run_checkpoints`, `run_wakeups` |
| Approvals and rules | `approvals`, `permission_rules` |
| Per-session engine state | `session_budgets`, `session_engine_state`, `session_todos`, `session_goals` |
| Background work | `tasks` (shell commands and subagents) |
| Clients | `client_deliveries` |
| Automation | `automation_jobs`, `automation_fires` |
| External agents | `external_agent_sessions` |
| Skills | `skills`, `skill_fts`, `skill_sessions` |

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
