# Architecture

MatrixClaw is daemon-first. `matrixclawd` owns all durable state (SQLite,
session files, setup.json) and serves it over a local HTTP API. Clients render
that state and send commands; exiting the TUI or restarting Telegram loses no
session, run or approval.

```mermaid
flowchart LR
    TUI[Terminal TUI] --> API[HTTP API]
    TG[Telegram worker] --> API
    IOS[iOS package] --> API
    GW[Telephony gateway] --> API
    API --> CORE[core]
    CORE --> STORE[(SQLite)]
    CORE --> ENGINE[agent engine]
    CORE --> EXT[external agents]
    ENGINE --> PROVIDERS[LLM providers]
    ENGINE -- ports --> CORE
    CORE --> SET[modules.Set]
    SET --> MODS[storage, voice, web, browser, MCP, skills, telephony, ...]
```

## Repository map

| Path | What it holds |
|---|---|
| `cmd/matrixclaw` | CLI entry: setup, TUI, `service`, `providers`, `agents`, `mcp serve`, `skills`, `update`, `doctor` (code in `internal/clientcmd`) |
| `cmd/matrixclawd` | daemon entry (code in `internal/daemoncmd`) |
| `cmd/matrixclaw-telephony-gateway` | optional Asterisk/SIP to realtime voice bridge (`internal/telephony/gateway`) |
| `clients/terminal` | setup wizard (`setup`), chat TUI (`chat/runtime`), read model (`chat/readmodel`), widgets (`ui`), colour tokens (`theme`) |
| `clients/telegram` | Telegram Bot API worker: commands, deliveries, uploads, inline and guest mode, voice |
| `clients/ios` | Swift package for the HTTP/SSE API |
| `internal/daemoncmd` | composition root: bootstrap, data-dir lock, module wiring, `supervisor` (reload, restart, stop) |
| `internal/api` | HTTP API: one route table, auth, roles |
| `internal/daemonclient` | Go client of the API, used by every Go client |
| `internal/controlplane` | slash commands and screens for the terminal and Telegram |
| `internal/core` | sessions, messages, runs, approvals, tasks, subagents, deliveries, memory, external-agent runs; the engine's port adapters |
| `internal/agent` | the native agent engine (below) |
| `internal/transcript` | message, part, finish and origin types shared by core, store, api and agent |
| `internal/tools` | tool contract and the built-in file, shell and task tools |
| `internal/permission` | permission rules, subjects, bash parsing, mode presets |
| `internal/glob` | the one glob matcher (`**`, classes, braces) and `.gitignore` rules, for path rules and the search tools |
| `internal/shelltask` | background shell processes and their output files |
| `internal/webtools` | `web_search` and `web_fetch` |
| `internal/modules` | module lifecycle, settings contract, and the modules: `storage`, `delivery`, `geo`, `web`, `voice` (+ `voice/realtime`), `telephony`, `browser`, `mcp`, `skills`; `localruntime` holds local voice/browser runtimes |
| `internal/procsup` | supervised local helper processes |
| `internal/providers` | provider adapters (`ai/*`), catalog, model metadata, wire quirks |
| `internal/sessionllm` | the provider runtimes sessions pick from |
| `internal/externalagents` | external agent registry and adapters (`claudecode`, `codexapp`) |
| `internal/mcp` | MCP client and the `matrixclaw mcp serve` stdio server |
| `internal/automation` | reminders and scheduled AI tasks |
| `internal/skills` | skills library (files plus its own tables) |
| `internal/setup` | typed `setup.json`, provider config, service files |
| `internal/store` | SQLite persistence (see [STORAGE.md](STORAGE.md)) |
| `internal/toolview` | how a tool call is shown, shared by the TUI and Telegram |
| `internal/updater`, `internal/version` | self-update and build stamps |
| `internal/safego`, `internal/textutil`, `internal/ids`, `internal/xdg` | leaf helpers |

## Daemon start

`daemoncmd.Run`: load setup, take `matrixclawd.lock` next to the database (a
second daemon on the same data exits), open the store, build `core.Core` and
every module once (`daemoncmd/modules.go`), bind the listener, apply the setup
(`supervisor.ApplyBootstrap`), then serve. After that, in the background: the
automation scheduler, release of held deliveries, and `Core.Recover`. Shutdown
cancels the daemon lifetime, which interrupts executing runs (they are kept for
recovery), waits for them, then closes external agents and modules.

## HTTP API

- Routes are Go 1.22 `ServeMux` patterns in one table in
  `internal/api/server.go`; `api.New(Deps)` takes every service once.
  `internal/api/routes_test.go` lists every request the clients send.
- The daemon refuses a non-loopback bind unless `MATRIXCLAW_ALLOW_REMOTE_HTTP=1`,
  and then only with an API token.
- Auth: `Authorization: Bearer <token>` (constant-time compare). The token is
  `daemon.api_token` in setup.json (setup generates it) or
  `MATRIXCLAW_API_TOKEN`. `GET /v1/health` is open.
- Roles: a client asserts who it acts for in `X-Matrixclaw-Role` (`owner`,
  `member`, `guest`; absent means owner). Every caller holds the token, so the
  daemon trusts the header as it trusts the token. Telegram sets it from the
  chat an update came from, never from message text.
- Every route in the table has a policy, enforced by one middleware
  (`internal/api/role.go`; `policy_test.go` lists them): `open`; `owner`
  (admin reload/restart/stop, the session permission mode, writes to setup
  providers, external agents, module settings, MCP and the skills library,
  storage deletes and temporary-storage cleanup and settings); or `session`,
  for every write that acts in a session (named by the path, by the run, task,
  approval or automation job it names, or by the JSON body's `session_id` or
  client binding): members and guests are refused (403) there when the session
  runs unattended (`core.RunsUnattended`: full_auto or external agent). Reads
  stay open. Only the owner creates a session with an external agent or a
  permission mode, global permission rules are owner-only, and guests keep no
  rules. Core knows nothing of roles. Clients only hide what the daemon would
  refuse.
- `writeError` is the one error answer: the status of the first sentinel the
  error wraps (`core.ErrNotFound` 404, `core.ErrInvalidInput` 400, ...), else
  the status a handler gave it with `withStatus`, else 500.

## Live events and the terminal read model

- `GET /v1/events` is a per-session SSE stream: `message.created|updated`,
  `run.updated`, `tool.updated`, `approval.requested|resolved`,
  `input.updated`, `task.updated`, `todo.updated`, `context.updated` (context
  size the engine measured after each step). Replay with `?after=` covers only
  a 256-event ring shared by all sessions.
- Each subscriber gets events in ID order. One that falls 512 events behind
  is closed, never skipped silently; its client reconnects and reloads. An
  event that cannot be encoded is logged and ends the stream the same way. An
  open stream sends a `: ping` comment every 20 s.
- So the terminal subscribes first, then loads `GET /v1/snapshot`, and drops
  events at or below the snapshot's `event_id`. After that it changes only
  through events (`clients/terminal/chat/readmodel`), reloading on reconnect
  (also after the daemon closed the stream), session switch and commands that
  ask for it (`ReloadSnapshot`).
- Chat rows are reconciled by ID and revision, so expanded output stays
  expanded. Context usage in the header comes only from the daemon.
- `internal/toolview` (`Describe`, `FileChangeOf`) gives a tool call's title,
  progress verb, main parameter and file change to the TUI and Telegram alike.

## Commands and screens

`internal/controlplane` turns slash commands into screens. A `Dispatcher` holds
one `*daemonclient.Client` (client name, external key, role) and answers with a
`Result`: text, picker, form, prompt, text edit, confirm or info. Clients render
pickers themselves (Telegram buttons and paging, terminal menus); a picker
carries the command that shows it again. Module settings are drawn by one
generic screen, `/modules <id> [open|confirm|set] <path>`; external agents,
skills, MCP and storage have screens of their own. Provider forms are kept by
an opaque id (`/provider form <id> ...`) so the API key never travels inside a
command string.

## Runs

A message is accepted by `core` as a run (`accepted`) and executed on its own
goroutine by `Core.ExecuteRun`: `claim` (under the session gate) makes it
`running`; external-agent sessions go to their adapter, native ones build an
`agent.Engine` and call `Engine.Run(ctx, Task)`. Its `Outcome` (`completed`
with a stop reason, `waiting_approval`, `waiting_events`, `interrupted`,
`canceled`, `failed`) is applied by core.

Run status has one writer, `transition` (`internal/core/run_lifecycle.go`),
with one edge table (`runEdges`):

```text
accepted ─▶ running ─▶ completed
               │  ▲
               ▼  │
   waiting_approval / waiting_events
any status that has not ended ─▶ failed | canceled
```

`transition` commits, then follows up. The commit checks the edge, sets
`finished_at`, saves the wakeup of a run parking in `waiting_events` and seals
the reply in the same write (`store.SealRun`; an ended run stays ended,
`ErrRunEnded`); only its failure is returned. After it every step runs and
their errors are joined and logged: drop a wakeup the run no longer has,
publish `run.updated`, sync the subagent task the run works for, and for an
ended run clear its checkpoint, reject its open approvals, cancel the blocking
subagents of a failed run and tell the user of a failed wake run. Callers
waiting for the end (a blocking `agent` call) are woken in any case.

Session gates never nest: ending a subagent's run takes its parent session's
gate (the finished task wakes the parent), so no subagent run ends while a
gate is held.

`afterRun` runs when the executor lets go of a run (or from `CancelRun` for a
parked one): it starts the session's next queued message, wakes the session
for finished background work, or resumes a parked run whose decision or event
arrived meanwhile. Every step is idempotent.

**Cancel.** `CancelRun` writes `canceled`, then cancels the run's context with
the cause `agent.ErrCanceled`. Any other stop (shutdown, release) cancels
without that cause and counts as an interrupt. Canceling a run also stops the
shell tasks and subagents it started.

**Recovery.** A run found `running` without an executor was interrupted.
`claim` counts the recovery; the count resets once a step finishes, and more
than 8 recoveries in a row without one fail the run. It builds a plan for
every call left without a result (`agent.Task.Interrupted`, `Settle`): `rerun`
read-only and deferred calls, `ask` again for a mutating call, `answer` an
unknown tool with an error. A mutating call never reruns without a fresh
approval. `Core.Recover`, called once at daemon start, marks leftover shell
tasks `lost`, resumes each active run once, syncs subagent tasks whose child
run already ended, and settles idle sessions (next queued message or a wake
run). External-agent runs keep their own recovery in core.

## Agent engine

`internal/agent` is the loop: steps, budget (steps, active time, tokens) and a
final turn without tools, loop guard, output-limit continuation, completion
checks, context notes, summaries. It knows nothing of SQLite, HTTP or clients
and talks to core through ports (`internal/agent/ports.go`):

| Port | Core adapter | Purpose |
|---|---|---|
| `Model` | `providers.Runtime` | one generation |
| `Journal` | `coreJournal` | load the window from the latest boundary, append and stream messages, checkpoint, record `run_steps` |
| `Tools` | `coreTools` | specs, `Authorize` (rules, concurrency key, barrier), `Execute` (result or approval request) |
| `Approvals` | `coreApprovals` | record an approval request, report open ones |
| `Inbox` | `coreInbox` | steer input, decided approvals, finished background tasks |
| `Sink` | `coreSink` | live events to clients |
| `Prompts` | `corePrompts` | fixed system prompt per run; changing state as a context note |
| `Todos` | `coreTodos` | open todo items of the run's `/continue` chain |

What the engine remembers between steps (`agent.RunState`) is checkpointed with
the phase, so a parked or restarted run keeps it. How a step can go wrong:

- **Generation failures** go through one `withRetry`, shared with summaries:
  up to 6 attempts, doubling jittered waits (from 2 s for rate limits),
  Retry-After honoured, at most 2 minutes of waiting in all. A reply that fails
  while streaming is sealed (finish reason `daemon_restart`, never replayed) and
  generated again. A malformed tool call goes back to the model as a note (three
  in a row fail the run); a call whose arguments are not JSON is journaled with
  `{}` and answered with an error.
- **Output limit:** a cut reply is kept and continued, with the limit raised
  once; a cut tool step runs its complete calls and tells the model a cut call
  was dropped. The fourth cut in a row turns the next step into the final turn (stop reason
  `budget_exhausted`).
- **Context:** old bulky results are elided at 60% of the usable window, and
  one tool round's results share 30% of it; at 80% the older history becomes
  a summary boundary that keeps the run's user messages verbatim. Every
  summary takes one path (`run.compact`), whether a run's or `/compact`'s
  (`Engine.Compact`): compact model or else the run's, a model slot, retries,
  `run_steps` rows with stop reason `compact` (for `/compact`, on the
  session's latest run). After a summary the current text of the five files
  edited last is re-attached (at most 8k tokens, a tenth of a smaller window),
  and the context note with the todo list is sent again. `/context` estimates
  the system prompt and tools a run would get, so its hint matches the 80%.
- **Loop guard:** among the last 20 calls, the most repeated call with the
  same result, or the same call failing whatever its error says, warns at 3
  and stops the run at 5, whatever the order inside a batch; so does the same
  reply text of 60 runes or more. Two calls taking turns with the same results
  over the last 6 calls stop the run.
- **Completion checks:** a reply without tools is held back once while the
  run's todo list has open items, and once when the run changed files (edit
  tools, or a bash command that writes files) after its last successful
  build, test, lint or type-check command; only while budget remains and the
  run has bash.
- **Reasoning text:** a reasoning block the reply opens with, or a closed one
  starting a later line, is stripped as the reply streams; code fences are left
  alone.

While a native run is active the engine is the only writer of its session's
transcript; everything else reaches it through the inbox. Messages are ordered
by an integer `seq` and never rewritten. `Core.ExecuteTool` serves run-less
calls only (tools API, voice, MCP server).

Subpackages: `context` (token estimates, conversation building, elision,
summary boundaries), `toolsched` (parallel batches of up to 8 calls, daemon-wide
keyed locks, the model-request semaphore `daemon.model_concurrency`), `prompt`
(fixed texts and tool guidance), `todo` (items, validation, rendering),
`agenttest` (scripted model and port fakes for tests).

## Providers

`internal/providers` holds the catalog (`ProviderSpec` per built-in provider,
type defaults for custom ones), the adapters in `ai/*` (OpenAI-compatible
chat, Codex Responses, Anthropic Messages, Gemini) and `factory`, which picks
the adapter by provider type. `internal/sessionllm` keeps one runtime per
provider and model.

- Model metadata (window, output limit, tools, images, reasoning): the
  provider's own live listing, kept by provider ID and listed model ID in
  `runtime/context-windows.json`, then static rules, then 128k.
  `SessionLLMRegistry.ContextWindow` applies a manual window only to the
  provider's configured model.
- Errors: adapters return `*providers.APIError` (status, `Kind`,
  `RetryAfter` from `retry-after-ms`, `Retry-After` or the body);
  `providers.ClassifyError` gives any error a kind: `rate_limit`,
  `overloaded`, `transient` (retryable), `auth`, `quota`,
  `context_overflow`, `permanent`.
- A base URL on a loopback address needs no API key.
- Codex OAuth: every refresh holds a flock on `<auth store>.lock`, since the
  daemon and the CLI share the single-use refresh token; a 401 refreshes once
  and retries, then fails with `providers.ErrReauthRequired`.
- The stream idle timeout is 120 s, 5 min at high and 10 min at xhigh
  reasoning effort.

## Tasks, subagents, await

- Background work is one task model (`core.Task`, table `tasks`, one
  `task.updated` event) with two kinds. `shell`: a command run with `bash -c`
  in its own process group (`internal/shelltask`) with a size-capped output
  file; `bash` moves a command still running after 120 s to the background.
  `subagent`: a child run started by the `agent` tool. A child that cannot be
  started leaves no session, run or worktree behind.
- Commands get the environment of a login shell, read once at the first
  command (`shelltask.LoginEnvironment`), without API key variables and
  `MATRIXCLAW_*` (`setup.ChildEnvironment`, with the setup applied now). MCP
  stdio servers get the daemon's environment filtered the same way, plus
  their `env`.
- A finished foreground command's result always states its exit code (or its
  timeout); output over 30 KB becomes its head and a longer tail, with the
  path of the kept log, which core does not cut again. Processes it left in
  its group are killed. Exit 1 with no output from a search, `test` or
  process lookup is neutral, not an error.
- Shutdown stops the background commands (`Core.StopShellTasks`); the next
  start marks their tasks `lost`. Compaction deletes the logs of ended tasks
  that no later message names.
- A read-only subagent gets `bash` as a read-only tool that never asks
  (`tools.ReadOnlySpec`): only plain lines of read-only commands
  (`permission.CheckReadOnly`) whose paths stay inside its working directory,
  away from secrets and not in or above the home directory, run.
- Shell task: `running` → `completed`/`failed` by exit code, `canceled`, or
  `lost` after a restart. Subagent task follows its child run inside
  `transition`: `running` ⇄ `waiting_approval` → `completed`/`failed`/`canceled`.
- A blocking `agent` call waits for the child's run to end and returns its
  summary; the parent stays `running` in that call, holds no model slot, and
  its time there is not counted. A background call returns the task; its end
  is an event for the parent session. A replayed call finds its task by
  `(run_id, parent_tool_call_id)`.
- A child's approval is its own (`approvals.task_id`), listed and announced in
  the parent's session and sent as an approval delivery to the parent's chat.
  A read-only child's approval is stored already rejected, without asking.
- `await` parks a run in `waiting_events` with a `run_wakeups` row; a finished
  task, the user's message or the timer (`Core.RunWakeups` ticker) wakes it.
  Finished work in an idle session starts a `wake` run.

## Tools and approvals

- A tool is a `tools.Executor` (`Spec`, `Execute`). `Spec.Effect`
  (`readonly`/`mutation`) drives concurrency, read-only subagents and recovery;
  `Spec.Asks` says whether a call no rule decides waits for the user.
- Core alone decides (`core.checkPermission`): session and global rules, then
  the mode preset (`default`, `accept_edits`, `full_auto`); deny wins over ask
  over allow. A rule names its tool or a tool listed in `Spec.CoveredBy`
  (`read` rules cover `grep`, `glob` and `ls`). `permission.Builtin` (`memory: list` = allow) is in every preset
  and cannot be deleted; a stored ask/deny rule on `memory` still wins. The
  same check covers runs, the tools API, voice and the MCP server.
- A call that asks runs only with a grant: its newest approval is approved and
  it has no result yet, read from the store per run, so a grant runs it once.
  Executors never see approvals.
- What the approval shows comes from the tool's optional `tools.Previewer`
  (side-effect free: a `tools.FileChange` diff, the command and its
  directory), built on the call's goroutine under its concurrency key; a
  preview error answers the call before anyone is asked. Tools without one
  show "Run <tool>" and the arguments. An ask rule adds "Asked by rule ...".
- A mutating call that waits for approval is a barrier: later calls of the
  same reply wait, earlier ones run.
- Bash lines are parsed (`permission.ParseLine`). A redirection to a file,
  substitution, wrapper (`sudo`, `env`, `xargs`, `bash -c`), package runner
  (`npx`, `pnpm exec`, `uv run`, `go run`, `docker run`, ...), a flag that runs
  a program, or an in-place edit (`sed -i`, `perl -i`, awk that redirects)
  makes a line risky: no pattern rule allows it, and it asks when a deny or
  ask rule may catch it. The commands a wrapper or runner may run are matched
  by deny and ask rules too, so `deny rm:*` denies `pnpm exec rm -rf x`.
- `tools.Result.Status` (`success`/`error`/`neutral`, empty = success) is the
  only status field. Results over about 8k tokens go to
  `sessions/<id>/tool-output/`, and the model gets head, tail and path.
- A search (`grep`, `glob`, `ls`) meets the rules by its directory: a deny or
  ask rule matching the directory decides the whole call. Below it, core passes
  `Call.Skip` (`permission.SearchSkip`: the deny and ask rules that did not
  match the directory) and the walkers drop matching paths, saying how many
  they left out. Places that hold keys are dropped the same way unless the
  search starts inside one.
- Searches list files with `git ls-files -co --exclude-standard` inside a git
  repository; otherwise, or when git lists nothing (an ignored directory named
  in `path`), they walk the tree following `.gitignore` files (and outside a
  repository skip `node_modules`, `vendor`, `dist` and similar). Hidden entries
  are searched only when the pattern or `include` names them. Results are
  ordered most recently modified first, then by path, and cut after sorting.
- `read`, `grep` and the file-changing tools refuse credential files: the
  places their owners register at daemon start with `tools.ProtectCredentials`
  (`setup`: setup.json and `daemon.env`; `openaicodex`: the auth directory and
  the Codex CLI's auth.json; `claudecode`: the Claude Code login), everything
  inside them and their backups. A read-only call on a place where keys
  usually live (`tools.HoldsSecrets`: `~/.ssh`, `~/.aws`, ...) asks when no
  rule decides.
- Every tool path goes through `tools.ResolveFilesystemPath`: relative to the
  call's working directory, symlinks resolved for permission rules and writes.

### File tools

- `read` pages a file: at most 2000 lines and about 24 KB a call, well under
  the result cut, ending with the offset to read on. Lines over 2000
  characters are cut; a file with a NUL byte in its first 8 KB is refused as
  binary.
- File state (`tools/file_ledger.go`): one in-memory ledger shared by `read`,
  `write`, `edit` and `multiedit` holds, per session, the xxh3 hash of each
  file the session read (any part) or wrote. Changing an existing file the
  session has not seen, or that changed since, is refused, in the preview too,
  so nobody is asked. New files need no read. The ledger is not persisted:
  after a daemon restart the model reads a file again before changing it.
  Calls without a session are not checked.
- Edits (`tools/editmatch.go`) match byte for byte, then ignoring indentation
  (the new text moves to the file's indentation), surrounding whitespace, and
  whitespace with typographic quotes and dashes, then all of that with doubled
  escapes (`\n`) undone. A fallback must find exactly one place; `replace_all`
  is byte for byte only. CRLF line ends and a byte order mark are kept. A miss
  shows the closest lines with their numbers; an edit whose new text is
  already there answers neutral, as does a change that changes nothing.
- Writes go to the symlink-resolved path through a temporary file and a
  rename, keeping the file's mode. The result shows the changed lines
  numbered like `read`; its metadata (`tools.FileChange`) keeps only the
  changed regions, at most 16 KB a side, while an approval shows a file up to
  64 KB whole.

## Web tools

`internal/webtools` offers two tools, registered by the `web` module.
`web_search {query, limit}` asks the configured provider (Tavily, Serper,
SearXNG) and falls back to DuckDuckGo. `web_fetch {url}` fetches through an
SSRF-safe transport (public addresses only, every redirect re-validated and
checked against the call's permission rules), reads up to 5 MB, converts HTML
to markdown with readability, and returns at most 400,000 characters; it never
drives a browser and notes when a page needs JavaScript. Deep research runs
through read-only subagents.

## Setup config

`setup.json` is one typed `setup.Config` owned by `internal/setup`:

- Loading is strict: a key the struct does not declare, or a stored value
  outside its closed list (provider type, tool use mode, runtime modes,
  browser and voice providers, external agent ids, web search provider, MCP
  transport), fails with an error naming the key. A file is never
  reinterpreted on read; there are no value aliases. A version 3 file (v0.2.0)
  is converted to version 4 once on load (`internal/setup/legacy_v3.go`): the
  original is kept as `setup.json.pre-v4`, the keys v0.2.0 still wrote are
  dropped and alias values become canonical.
- Every edit is `Service.Update(func(*Config) error)`: load, change,
  canonicalize, `Config.Validate` (structure only: no network, no env), save
  atomically under one lock. Provider edits, the session model switch and
  module settings change only their own part.
- The file stores the user's choices, not built-in defaults.
  `ProviderConfig.Effective` fills catalog defaults; `Runtime` adds the API key
  from the file or the environment. `CheckProvider` (a usable key) runs when a
  provider is saved, not in `Validate`.
- The terminal wizard edits a `Config` in memory and saves with
  `Service.Apply`, the only place that checks the Telegram token; it copies
  only the sections the wizard owns.
- Update requests use pointer fields: absent leaves a value, empty clears or
  resets it. Clients see secrets only as masked previews.

## Modules

Storage, delivery, maps, web, TTS, STT, realtime voice, telephony, browser,
MCP and skills implement `modules.Module`:

```go
ID() string
Apply(ctx, setup.Config) error // at start and after every setup change; cheap when unchanged
Tools() []tools.Executor       // offered now; none while off or unconfigured
Context() string               // system prompt paragraph
Status(ctx) Status             // enabled, ready, state, facts; no network I/O
Close() error
```

- `modules.Set` owns them and is core's tool executor: the base tools (files,
  shell, tasks, todo, await, memory, agent, reminders) plus each module's
  current tools, in a registry swapped atomically after each `Apply`.
- `supervisor.Reload` (`POST /v1/admin/reload`, the wizard, every API handler
  after a setup write) loads setup.json and, under one lock, applies external
  agents, the module set, session LLMs, the assistant profile with module
  paragraphs, and Telegram. Each part does nothing when its settings did not
  change; MCP reconnects only changed servers, and one failing server does not
  disable the others.
- `GET /v1/modules` lists statuses; the model's runtime note is built from
  them (without `state`, so it stays stable).
- A module with settings implements `modules.Configurable`: `Settings` returns
  typed items (toggle, choice, text, secret, action, page, info) and `Change`
  runs a change (install an engine, download a model) and returns the
  setup.json edit, whether to reload, and the page to show next. Served at
  `GET|POST /v1/settings/{module}`; the API saves the edit and reloads.
- Local voice servers run under one `procsup.Supervisor` owned by
  `localruntime.Runtime`: one process per provider, readiness probed outside
  the lock, stopped when deselected and on shutdown, and with `Pdeathsig` on
  Linux (one-shot commands get it through `procsup.Prepare`). The browser's
  Playwright MCP server is owned by its MCP session.
- Realtime voice providers are `realtime.ProviderSpec`s with a codec over one
  websocket session engine.

## Deliveries

Clients that cannot hold an event stream fetch work from a delivery queue
(`client_deliveries`; `GET /v1/client-deliveries`, then `ack` or `fail`).
Types: `run` (a run's reply), `document` (`send_file`), `notice`, `approval`
(a subagent's approval sent to the parent's chat). A run gets a delivery only
when its client sets `ReceivesDeliveries` in its capabilities (Telegram,
automation); the terminal follows events instead. `reply_once` marks a target
that takes a single reply (Telegram inline and guest). Restart notices are
`held` until the next start releases them.

## Planned after v0.2.0

- Drop the session runtime aliases (`codex`, `assistant`, ...) in
  `internal/core/external_agents.go`, then the `sessions.runtime_id` column.
- Delete the one-time v0.2 conversions, `internal/store/legacy_v02.go` (the
  database) and `internal/setup/legacy_v3.go` (setup.json), one release after
  they shipped.
