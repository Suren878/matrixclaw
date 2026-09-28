# Long-running native agent — design

Status: approved in brainstorming on 2026-09-23, revised after an architecture
review the same day. Sub-project 1 of 2.

Sub-project 2 (daemon-owned goals that wake themselves on timers/events and
work across many runs, Hermes-style) gets its own spec later and builds on the
`await` / `waiting_events` / `run_wakeups` mechanism defined here.

## Goal

A native MatrixClaw run can work on one assignment for hours and hundreds of
tool calls, the way Claude Code does: it keeps going until the work is done and
verified, survives context growth, restarts and denied approvals, runs
independent work in parallel and in the background, and stops on an explicit
budget with a usable summary instead of failing.

## Problems in the current code

Baseline: commit `2445681` (generation retry, checkpoints, crash recovery,
terminal stream events).

- `maxRunToolSteps = 32` fails the run when reached (`internal/core/execution.go`).
- The first text-only reply ends the run; there is no completion check.
- Compaction keeps every message of the current run
  (`latestCompactSummaryForRun`), so a long run overflows and fails. The
  compaction marker is found by an emoji text prefix and appended *after* the
  messages it covers.
- A denied approval fails the run in three places: `ResolveApproval`
  (`tool_approvals.go`), the subagent approval bridge, and crash recovery
  (`run_recovery.go`). The model never sees the denial.
- The plan is advanced only by the terminal client (`plan_run.go`).
- The native Anthropic adapter has tool use disabled
  (`providers/request_normalize.go`, `anthropiccompat/adapter.go`).
- 4096 default output tokens; `finish_reason=length` is an error;
  `providers.Response` has no stop reason; Gemini `finishReason` is not decoded.
  HTTP client timeouts (90–120 s for anthropic/gemini/codex) cap the whole
  response.
- `Usage.CachedTokens` means different things per adapter (included in
  `InputTokens` for openaicompat/codex/gemini; cache read + write and excluded
  for Anthropic).
- Tools run strictly sequentially; background bash jobs live in memory only;
  foreground bash has no timeout.
- Messages are ordered by text `created_at` (RFC3339Nano, not lexically safe);
  the full session is re-read from SQLite several times per step and per tool
  call; per-run usage is rebuilt by a full scan and only counts tool turns.
- Several writers mutate history outside the loop: `core.ExecuteTool` from the
  API, voice and the MCP server; steer text injected into a tool result;
  `updateSubagentResultMessage` rewriting an old result.
- The system prompt changes every turn (plan, memory, status), defeating prompt
  caching. The prompt tells the model to minimise tool calls.
- Telegram sends one message per tool call and loads the whole session per
  delivery; the iOS `RunStatus` enum is strict, so any new status breaks
  shipped apps.

## Decisions

| Topic | Decision |
|---|---|
| Uncommitted 2026-09-17 work | Committed as the baseline (`2445681`). |
| Scope | Long single task first; goals/wake-ups are sub-project 2. |
| Architecture | New `internal/agent` engine behind narrow ports; `core` serves it. |
| Stop condition | Budget (steps, active wall-clock, tokens) with a tool-less final turn; the run ends `completed` with `stop_reason=budget_exhausted` and is continuable. No cost limit until a price source exists. |
| Approvals | Allow/deny/ask rules evaluated for every tool entry point; a denial is returned to the model; a pending approval parks the run. |
| Planning Mode | Replaced by `todo_write`; plan tables, tools and the terminal runner are deleted. |
| Native Anthropic | Gains real tool use, prompt caching and stop reasons (stage 1b). |
| Background processes | Not kept across daemon restarts: on start the daemon kills leftover process groups and marks their tasks `lost`. |
| User input during a run | Steer everywhere by default (TUI and Telegram); queue/interrupt only by explicit command. A message wakes a parked run. |

## 1. Boundaries

```
internal/transcript/       leaf: Message, parts, finish metadata, Origin (moved out of core)
internal/permission/       leaf: rules, subjects, bash command parsing, mode presets
internal/agent/            loop: Engine.Run(ctx, Task) -> Outcome
  budget.go                steps, active wall-clock, tokens; final turn
  loopguard.go             no-progress detector
  journal.go               in-memory transcript over the Journal port
  context/                 window, elision, summary, token accounting
  toolsched/               batches, concurrency keys, approval barrier
  todo/                    todo_write tool and state
  prompt/                  stable system prompt, engine context messages
  agenttest/               ScriptedModel and fakes for the ports
```

`core.Message` and its part types move to `internal/transcript`; `core`,
`store`, `api` and `agent` import it directly (no aliases). This avoids a
`core` ↔ `agent` import cycle.

Ports implemented by `core`:

```go
type Model interface {
	Generate(ctx context.Context, req providers.Request) (providers.Response, error)
}

type Journal interface {
	Load(ctx context.Context, sessionID string) (Window, error) // latest boundary + messages with seq > covers_through_seq
	Append(ctx context.Context, msg transcript.Message) (seq int64, err error)
	FinishStreaming(ctx context.Context, msg transcript.Message) error // the only permitted update: an in-flight assistant message
	Checkpoint(ctx context.Context, state State) error
	RecordStep(ctx context.Context, step Step) error // run_steps row
}

type Tools interface {
	Specs(task Task) []tools.Spec
	Authorize(ctx context.Context, call tools.Call) (Decision, error) // rules + dry-run; shared with core.ExecuteTool
	Execute(ctx context.Context, call tools.Call) (tools.Result, error)
}

type Approvals interface { Request(ctx context.Context, p Pending) error }
type Inbox interface { Peek(ctx context.Context, runID string, kind InputKind) ([]Input, error); Consume(ctx context.Context, runID string, ids []string) error } // steer is consumed only after the tool result carrying it is written
type Sink interface { Emit(Event) }
```

**Single writer.** While a native run is active the engine is the only writer
of that session's transcript. Everything else (steer input, background task
completions, subagent results, approval decisions) arrives through `Inbox` as
new events. Persisted messages are immutable; an async subagent result is a new
message, never a rewrite of the original tool result. `core.ExecuteTool`
remains for run-less calls (API, voice, MCP server) and persists those itself.
Manual `/compact`, `/clear` and model changes are rejected while a run is
active; the model/runtime is fixed at run start.

**Ordering.** Messages get an integer `seq` (per database, monotonic), added
with `ensureColumn` and backfilled from `created_at, rowid`. All ordering and
`Load` use `seq`. The store gains point methods (`GetMessage`,
`HasToolResult`, `ListMessagesAfter(seq)`) so no step rescans the session.

**Outcome.** `Outcome.Status` is `completed` (with `stop_reason` `done`,
`budget_exhausted` or `loop_detected`), `waiting_approval`,
`waiting_events`, `interrupted`, `canceled`, or `failed`. `interrupted` means
the context was cancelled while the daemon keeps running (for example a failed
workflow heartbeat); `core` reschedules it through `startRun` instead of
leaving it `running` until restart.

`core` keeps message acceptance, session inputs, run status, approvals,
checkpoint-based crash recovery, and external agents (Claude Code, Codex —
unchanged). `ExecuteRun` becomes: load → `engine.Run` → apply `Outcome`.

Deleted without replacement shims: `execution.go`, `execution_turn.go`,
`execution_request.go`, `execution_conversation.go`, `execution_generation.go`,
`execution_tools.go`, `execution_prompts.go`, `context*.go`, `plan*.go`,
`types_plan.go`, `PlanStore`, `maxRunToolSteps`, the emoji-prefix marker
parsing, `IsPlanRunPromptMessage`, `clients/terminal/chat/runtime/plan_run.go`,
`clients/telegram/auto_approval.go`.

## 2. Loop

Each step of `Engine.Run`:

1. **Inbox.** Drain steer input and completions; each becomes a journal message.
2. **Context.** Build the window; elide or summarise if over threshold (§3).
3. **Model.** `Generate` with the existing bounded retry, under a daemon-wide
   provider semaphore. Branch on the response:
   - tool calls present (regardless of `StopReason`; gateways often send
     `stop` with tool calls) → tool batch (§4), next step;
   - `max_tokens` with truncated tool-call JSON, or with empty text because
     reasoning used the budget → drop the partial call, raise
     `MaxOutputTokens` once (×2, capped by the model), retry;
   - `max_tokens` with text → keep the partial answer, append an engine
     message "your reply was cut by the output limit; continue exactly where
     you stopped"; at most 3 consecutive continuations, then `failed`;
   - `end_turn` → completion check (below);
   - `refusal` / `content_filter` → `completed`, refusal shown as is.
4. **Record.** `run_steps` row and checkpoint (budget counters, phase,
   loop-guard state).

**Completion check.** If the todo list of the current run chain has
`pending`/`in_progress` items and budget remains, inject once: "Your todo list
has N open items: […]. Continue, or mark them and explain why you are
stopping." A second text-only reply completes the run; the user sees the open
items. A run chain is a run plus its `/continue` successors
(`continues_run_id`); todo from an unrelated earlier chain never triggers the
check.

**Budget.** Per-session settings over daemon-config defaults:

| Trigger | steps | active wall-clock | tokens |
|---|---|---|---|
| User message | 300 | 4 h | 0 = unlimited |
| Subagent | 100 | 1 h | 0 = unlimited |
| Automation / auto-wake | 50 | 30 min | 0 = unlimited |

- Active wall-clock excludes time parked in `waiting_approval` and
  `waiting_events`.
- Tokens are prompt + output from normalised usage; children's usage counts
  toward their own budget and is reported (not charged) to the parent.
- Auto-woken runs (task/subagent completions, timers) form a chain limited to
  20 consecutive runs without a user message; beyond that the session only
  notifies the user.
- At 80% of any limit an engine message says "about X left, wrap up".
- At 100% a **final turn** runs with `tool_choice: none` (tools stay defined so
  the request prefix and cache survive): "briefly: what is done, what remains,
  how to continue". The run ends `completed` with
  `stop_reason=budget_exhausted`.
- `/continue` (TUI, Telegram button shown by `stop_reason`) creates a user
  message "Continue" and a run with `continues_run_id` and a fresh budget over
  the same journal and todo.
- Counters live in the checkpoint; an approval resume does not reset them.

**Loop guard.** Hash of `(name, args, result)` — it detects *no progress*, not
repetition, so polling tools (`task_output`, `git status`) that return new
output are not flagged. 3 identical consecutive triples → engine message "you
are repeating X with the same result, change approach"; 5 → final turn and
`completed` with `stop_reason=loop_detected`.

**Engine messages** (nudges, continuations, warnings, recovery notices, the
context message of §3) carry `origin: engine` in `transcript.Message`. Clients
render them as system notes; they replace the old text-prefix hacks.

**Cancel** cascades: running children and the run's background tasks are
cancelled; completed results of a parallel batch are kept, the rest get a
"canceled" result. **User input** during a run is steer by default in every
client and is delivered at the next step; it also wakes a run parked in
`waiting_events`. Queue/interrupt remain as explicit commands.

## 3. Context

**Normalised usage.** `providers.Usage` becomes `PromptTokens` (entire input,
cached or not), `CacheReadTokens`, `CacheWriteTokens`, `OutputTokens`,
`ReasoningTokens`. Every adapter maps into it.

**Token accounting.** Authority is the last response's `PromptTokens` plus an
estimate for messages appended since. After elision, a summary, or a model
change the estimate is authoritative until the next response. The estimate is
runes/4 for Latin text and runes/2.5 for other scripts (Cyrillic), 1500 per
image. **Effective window** = model window − `MaxOutputTokens` − 5% reserve;
unknown models use 128k. The old 80k floor is removed.

**Large tool outputs.** A result over ~8k tokens is stored in full in a file
under the session data directory; the model gets the path plus head and tail.
This replaces the 12k-rune truncation in the provider view.

**Level 1 — elision** at 60% of the effective window, no model call. Tool
results older than the last 5 tool rounds and larger than ~1k tokens are
replaced in the request by `[output of read(path=…) hidden, ~12k tokens; call
again or read <file> if needed]`; images older than 3 steps likewise. The
database keeps everything. All eligible results are elided at once, so the
request prefix changes rarely.

**Level 2 — summary** at 80%, or when elision was not enough. The summary
request reuses the main request prefix (same system prompt, tools and
messages) with a trailing instruction and `tool_choice: none`, so it hits the
cache; an optional `compact_model` setting may name a cheaper model instead.
Everything except the tail is summarised; the tail is the last ~25% of the
window in whole turns, never splitting a call from its result. The run's
assignment and all steer messages are kept verbatim. Summary structure: goal,
decisions, files changed, errors and fixes, current state, next step. Todo is
not summarised; it is re-sent by the context message. Parts that do not fit are
summarised in chunks and merged.

The boundary is a structured part `{summary, covers_through_seq, run_id,
tokens_before, tokens_after}`. `Journal.Load` returns the latest boundary plus
messages with `seq > covers_through_seq`, so the tail written before the
boundary is kept. The low-yield backoff (two summaries saving <10%) reads
`tokens_before/after` instead of parsing text; when it trips and the window is
still over threshold, the final turn runs and the run ends `failed` with reason
`context_exhausted`.

**Context-length error from the provider.** Force a summary with half the tail
and retry once; a second overflow is `failed` with `context_exhausted`.

**Stable prompt and caching.**
- The system prompt changes only between runs: identity, rules, tool guidance,
  project root, skills, memory as of run start.
- Changing state (todo, budget warnings, loop warnings, recovery notice,
  memory changes during the run, module status changes) is journaled as an
  `origin: engine` **context message**, only when it changed. It becomes part
  of the immutable history, so earlier prefix bytes never change.
- Anthropic: `cache_control` on system + tools and on the latest message
  (rolling breakpoint, max 4). Stage 1b marks only the tools breakpoint; the
  system and message breakpoints arrive in stage 3 together with the stable
  prompt, since before that the cache writes would not be read back. OpenAI-compatible: `prompt_cache_key =
  sessionID` where accepted; Claude models through OpenRouter get
  `cache_control` in content parts. Gemini: implicit caching.
- Cache read/write per step is stored in `run_steps` and shown in `/status`.

### Implementation notes (as built, stage 3)

Where the build differs from the text above:

- **Boundary** is a message field `compaction` (`summary`, `kept`,
  `covers_through_seq`, `run_id`, `tokens_before/after`, `cleared`), not a
  part. `kept` holds the assignment and steer texts verbatim (head and tail
  past ~2k tokens); `/clear` writes a boundary with `cleared` and removes the
  session's kept tool-output files, except ones a later message refers to.
  Messages are indexed by `(session_id, seq)` where `compaction_json <> ''`.
- **Summary request** reuses the step's request unchanged, `tool_choice`
  included, since changing it would cost the cached prefix; tool calls in the
  reply are dropped. When that (or the compact model) fails for any reason, the
  run's model summarises in chunks; chunk requests cap their output at 8k
  tokens or the chunk size.
- **Elision** hides results before the last 5 tool rounds, or fewer when they
  outgrow 30% of the effective window (the newest round stays), and images
  before the last 3 replies. Hysteresis: once moved, it moves again only when 5
  more rounds or replies become eligible, or when a summary is due. The
  watermark is checkpointed, and a session's next run starts from the last
  run's watermark (`session_engine_state`), so its first request looks like
  the previous run's last one.
- **Effective window** is floored at half the model window. The model window
  is capped by `daemon.context_window_cap` (default 200k; set it to the
  model's window to disable), for runs, summaries and `/context` alike, which
  shares the engine's computation. A provider overflow teaches the run its
  real prompt room (90% of the rejected prompt), checkpointed so tails,
  elision and summary chunks shrink for the rest of the run. Rate-limit errors
  (tokens per minute) are not overflows.
- **Signed reasoning** written before a history edit (elision move or
  boundary) is not replayed; plain reasoning is. The newest boundary counts as
  an edit even when no checkpoint recorded it.
- **Context note** is found by reading the newest note in the history, not
  tracked in counters, so restarts and summaries stay consistent.
- **Large outputs**: error results spill to files too.
- **Usage**: `/usage` shows the cache hit share.
- **Deferred to stage 5**: todo in the context note.

## 4. Tools, permissions, background work, subagents

**Scheduler.** `tools.Spec` gains `ConcurrencyKey(args) string`: empty means
freely parallel (read-only tools); otherwise calls with the same key are
serialised by a daemon-level keyed mutex, which also serialises different runs
and sessions. Defaults: mutating filesystem/shell tools → the working
directory; MCP tools → `mcp.<server>` (so one shared browser is never driven
twice at once). Up to 8 calls run concurrently. Results are journaled in the
model's original order. Unknown or invalid calls keep becoming error results.

**Approval barrier.** Calls are authorised in order. A mutating call that needs
approval is a barrier: calls after it in the batch are journaled as `deferred`
and executed after the decision; calls before it and independent read-only
calls run. The run parks in `waiting_approval` once the runnable part of the
batch is done and resumes when every pending approval is resolved. Crash
recovery treats `deferred` calls as not started (no "retry after restart"
approval).

**Checkpoints.** Only the engine goroutine writes the checkpoint; the phase is
`tool_batch{call_ids, deferred_ids}`. Tool goroutines never touch it. The
per-call checkpoint writes in `tool_call_prepare.go` / `tool_call_finish.go`
are removed from the engine path.

**Permissions** (`internal/permission`, evaluated by `Tools.Authorize` and by
`core.ExecuteTool`, so API, voice, MCP server and runs all obey the same
rules):
- Every executor provides `PermissionSubject(args) (string, error)`: bash →
  the command; read/write/edit/glob/grep → absolute path; web_fetch → domain;
  MCP → `server__tool`. Read-only tools are subject to `deny` rules too
  (`deny read ~/.ssh/**`).
- Rule `{tool, pattern, effect: allow|deny|ask, scope: session|global}` in
  `permission_rules`. Globs are resolved to absolute paths when the rule is
  saved. Order: deny → ask → allow → mode preset → tool default.
  `default` / `accept_edits` / `full_auto` stay as named presets.
- Bash commands are parsed with `mvdan.cc/sh/v3` (new pure-Go dependency).
  Allowed only if every simple command is allowed; output redirection to files,
  leading `VAR=…` assignments, command/process substitution, and
  `-exec`/`-toolexec`-style flags always fall back to ask.
- Approval prompt: "Allow", "Always allow" (suggested rule such as
  `bash: go test:*` or `edit: /abs/internal/**`, with session/global choice)
  and "Deny with reason". Global rules can be created only by the owner (TUI,
  or the Telegram owner chat), never by Telegram guests.
- Children inherit the parent session's rules; "Always allow" on a bridged
  child approval writes the rule to the parent session.
- **Denial** becomes an error result `User denied: <reason>` and the loop
  continues. This replaces the fail paths in `ResolveApproval`, the subagent
  bridge (the child continues with the denial; the parent keeps waiting) and
  crash recovery. A deny rule yields `Blocked by rule <…>` immediately.

**Bash.** Foreground commands get a default timeout of 10 minutes (argument up
to 60). A foreground command still running after 2 minutes is moved to the
background automatically (`AutoBackgroundAfter`, already declared and unused)
and the model receives its task id.

**Background tasks.** One table `tasks` replaces `subagent_tasks` (rows copied
with `INSERT OR IGNORE … SELECT`, then the old table is dropped):
`id, session_id, run_id, parent_tool_call_id, kind (shell|subagent), status,
command_or_goal, runtime, model, isolation, pid, pgid, output_path, exit_code,
output_cursor, child_session_id, child_run_id, started_at, finished_at`.
- `bash(run_in_background)` starts the command in its own process group;
  stdout/stderr go to a file (mode 0600) capped at 20 MB, keeping the first
  1 MB and a rolling tail. Files are removed with the session.
- `task_output(id, wait_seconds?, filter?)` returns output since the last read
  (cursor) plus status; `task_kill(id)` stops the group: SIGTERM, then SIGKILL
  after 3 s if it still runs (timeouts, session deletion and canceled runs stop
  commands the same way). They replace `job_output` / `job_kill`.
- On daemon start, leftover `running` shell tasks have their process group
  killed and are marked `lost`; the model sees that in the next context message.
- Completion emits `task_finished` into the session inbox.

**`await` and `waiting_events`.**
- `await(ids?, timeout)` ends the step; the engine parks the run in
  `waiting_events` under the session gate after re-checking the inbox, so an
  event that arrived meanwhile is not lost.
- `waiting_events` counts as active in `GetActiveRunBySession` /
  `ListActiveRuns`, is handled by `prepareClaimedRun` and by the subagent
  terminal-status check.
- Timers live in `run_wakeups(run_id, wake_at)`; a ticker in `core` wakes due
  runs through the normal `startRun`; timers are re-armed at daemon start.
- A matching event, the timeout, or any user message (as steer) wakes the run.
- A `task_finished` in an idle session starts an auto-wake run (like subagent
  completions today), subject to the chain limit in §2, and is delivered to
  Telegram.

**Subagents.** `delegate_task` and `spawn_subagent` merge into
`agent{description, prompt, background, isolation: shared|worktree, readonly,
runtime, model}`; `runtime`/`model` keep delegation to Codex and Claude Code.
- `readonly:true` children get only read-only tools and run in parallel freely.
- Mutating children with `isolation: shared` use the working-directory
  concurrency key, so they run one at a time; with `worktree` they are parallel.
- `background:false` runs the child on the same `Engine` inside the call; the
  parent resumes only when every child call of the batch is finished and no
  approval is pending. A repeated call for a child that is still running parks
  again instead of finishing it with a partial summary.
- `background:true` makes it a `subagent` task whose result arrives as an event.
- The 24-hour in-memory resume watcher is replaced by the inbox/wake path.
- Limit: 4 active background subagents per parent, configurable. Children get
  todo and background bash; `agent` and `await` are forbidden.

### Implementation notes (as built, stage 4a)

- **Denial** reaches the engine as a decided approval (`InputDecided` with
  `Denied` and `Reason`); the engine writes `User denied: <reason>` (or
  `User denied.`) as an error result. Calls made outside a run get the same
  result from `ResolveApproval`. The reason is kept in `approvals.reason`.
- **Resume**: `ResolveApproval` starts a run only when it is parked
  (`waiting_approval`) and none of its approvals is pending. A run still
  finishing its step when the last decision arrives re-checks after parking;
  both checks run under the session gate, so exactly one of them starts it.
- **Barrier**: `Tools.Authorize` marks mutating tools (`Decision.Barrier`); a
  barrier waiting for approval defers every later call of the batch, read-only
  ones included (`ToolCallPart.Deferred`, no `tool.requested` until it starts).
  A read-only call waiting for approval does not hold the batch back.
- **Bridged approvals**: either decision goes to the child's approval with its
  reason; the parent's delegate call resumes when the child finishes. A missing
  pending child approval while the child is still `waiting_approval` means its
  decision is on the way (fixes the `not found` race of granting a bridge).
- **Clients** deny with a reason through the hidden `/approval deny <id>
  [reason]` command behind a prompt (TUI key `r`, Telegram button).

### Implementation notes (as built, stage 4b)

- **Subjects** come from the optional `tools.SubjectProvider`
  (`PermissionSubject(call) permission.Subject` with a kind: file, directory,
  command, domain, name); tools without one match only whole-tool rules. Rules
  name MCP tools as `mcp` and multiedit as `edit`.
- **Bash** lines are parsed against an allowlist of plain syntax (commands,
  lists, pipes, groups, if/while/for over plain words). Anything else is risky
  and never allowed by a pattern rule: writes, assignments, substitutions,
  `$VAR`, `[[ ]]`, arithmetic, brace or flag-shaped glob expansion, quoted or
  escaped command names, wrappers (`sudo`, `env`, `xargs`, `sh -c`, …), flags
  that run programs (`-exec=…`, `git -c`, `tar -I`, `rsync -e`, …) and lines
  over 16 KB (not parsed). A whole-tool allow (`bash`, `full_auto`) allows a
  risky line only while no deny or ask rule names bash; otherwise it asks.
  Deny and ask rules match a command by its base name (`/bin/rm` is `rm`).
- **Interpreters**: deny rules see commands, not the code an interpreter runs
  (`python -c`, `node -e`, `awk`, `sed e`, make and npm scripts, test
  runners). Allowing such a command is allowing arbitrary code, and under
  accept_edits the agent can write that code first.
- **Paths**: a search (grep, glob, ls) meets the deny and ask rules whose
  pattern may match below the searched directory; read rules cover searches,
  and grep does not read through links below the searched path.
  Domains are compared lower case, without a trailing dot, in punycode.
- **URL fetches**: web_research and web_research_ask share the rules of
  web_fetch (a `web_fetch` rule, or one added for either, covers all three).
  Their calls have no single subject; every URL they fetch, search results and
  redirects included, meets the rules through `tools.Call.Recheck`, and a
  guarded call runs synchronously without the browser fallback; its job is
  marked so a restart cannot resume it unchecked. web_search and the OSM tools
  call fixed provider hosts; MCP browser tools follow `mcp` rules only.
- **One check**: `core.checkPermission`, asked by `Tools.Authorize` (deny),
  whose verdict `Execute` reuses, and by `executeToolWithGrant` in every other
  pipeline. Every ask verdict becomes an `ask_rule` approval. A tool applies
  the rules to subjects it reaches later through `tools.Call.Recheck`
  (web_fetch redirects); `tools.Call.Guarded` marks a call whose tool a deny
  or ask rule may catch (one for the tool, or a `*` rule with a pattern), and a
  guarded web_fetch skips the browser fallback.
- **Suggestions** are stored with the approval (`approvals.suggestion_json`),
  recovery approvals included; "Always allow" is `{"approved": true, "always":
  "session"|"global"}`. Only commands with a known subcommand get a prefix
  (`go test:*`); others, interpreters and `go run` get their exact line; paths
  outside the working directory, `/` and home get the exact path.
- **Rules** live in `permission_rules` (global: `session_id NULL`); API
  `GET/POST /v1/sessions/{id}/permission-rules`, `DELETE
  /v1/permission-rules/{id}`; `/permissions add|delete`. Path patterns (also of
  `*` rules) become absolute when saved; a rule for one MCP tool stores
  `server__tool`. Global rules, permission modes and external agent sessions
  (which start in full_auto) are changed or started only by the TUI and the
  Telegram owner chat, and Telegram guests change no rules (client-side checks).
  Other clients may not bind to or send into a session that runs tools without
  asking (`core.RunsUnattended`: external agent or full_auto): controlplane
  refuses `/session use`, `/continue` and AI skill creation, and their messages
  carry `restricted`, which core refuses with 403 on every resolution path.
- **Delegated agents**: Codex and Claude Code children run their own tools;
  matrixclaw rules govern only the delegate call, not what the child does.

### Implementation notes (as built, stage 4c)

- **Keys** come from `tools.Spec.ConcurrencyKey(call)` (the call carries the
  working directory): none for read-only tools, `mcp.<server>` for every MCP
  tool, `dir:<working dir>` for mutating filesystem and shell tools and
  `tool:<namespace>` for other mutating tools. An executor may name its own key
  (`tools.ConcurrencyKeyProvider`): `delegate_task` takes `subagents:<dir>`, so
  delegations into one directory run one at a time while the child's own calls
  take `dir:<dir>`; one shared key would deadlock parent and child. The engine
  gets the key in `Decision.Key`.
- **Batch**: calls are authorized and journaled in call order, then up to 8 run
  at once (`internal/agent/toolsched`); calls sharing a key run one at a time in
  call order and hold the daemon-wide lock of the key, so other runs and
  sessions wait too. Results, loop-guard counting, `Tools.Finish` and
  `tool.finished` follow call order (a finished call waits for the calls before
  it); `tool.requested` follows call order as calls start; the two streams
  interleave. Calls of one reply are independent: a read next to an edit of the
  same file may see it before or after the edit.
- **Barrier**: a mutating call is a barrier unless a rule or the mode allows it
  (it then cannot ask); `delegate_task` always is, as its child may ask. The
  calls after a barrier are journaled `deferred` at once and start when it
  returns without asking; if it asks they stay deferred as in stage 4a. So
  delegations never run in parallel: two in one reply take turns behind the
  barrier, and those of other runs into one directory wait for
  `subagents:<dir>`. A call refused by `Authorize` is journaled together with
  its error, in call order.
- **Checkpoints** are written by the engine goroutine only: `model` before each
  generation, `tool_batch{call_ids, deferred_ids}` (column
  `run_checkpoints.tool_batch`) whenever calls of a batch start, after they are
  journaled, and `model` when the run parks. Approval requests of engine runs
  and delegations no longer touch the run's checkpoint; run-less `ExecuteTool`
  and crash recovery keep their writes.
- **Recovery**: a call is completed (result), in flight (journaled without a
  result: read-only calls replay, mutating ones ask again, every call of the
  batch and not only the first, asked ones keep waiting for their decision) or
  not started (`deferred`, or named in `deferred_ids` of the last `tool_batch`
  checkpoint, which marks it deferred again: it runs when the run resumes). A
  call still waiting for its key or a slot counts as in flight.
- **Stop**: results of calls finished before the run's context stopped are
  journaled; what a call returns afterwards is dropped. A canceled run answers
  every other call of the batch `Canceled by user.` (asked ones included), a
  failed run (a tool or journal error) answers them with the failure; an
  interrupted run leaves them to recovery. A panicking tool becomes an error
  result.
- **Model slots**: `daemon.model_concurrency` (default 4) bounds the model
  requests of all native runs, subagents and summaries included. A run holds a
  slot only inside `Generate`, so a parent blocked in `delegate_task` holds none.

### Implementation notes (as built, stage 6a)

- **Table**: `tasks` also keeps what subagents need (`description`,
  `agent_name`, `background`, `summary`, `error`, `result_message_id`) and
  `readonly` for stage 6c. `subagent_tasks` rows are copied on open (columns an
  old database lacks take their defaults; rows of deleted sessions are
  skipped) and the table is dropped. Blocking/async subagents are
  `background` 0/1.
- **Events**: a finished background task with `delivered_at` NULL is the
  event; `delivered_run_id` names the run that read it. Only
  `MarkTasksDelivered` writes these columns. A task the parent already knows
  about (blocking subagents, `task_kill`, a subagent canceled with its parent,
  `task_output` after the task ended) is delivered when it ends. Queued
  subagent completions replace `completion_queued_at/delivered_at`.
- **Engine**: `Inbox.Peek(InputEvent)` returns the session's undelivered
  tasks in finish order; each step starts by journaling them as `origin:
  engine` notes (exit code, command, the last 2000 bytes of output and the
  file path; for subagents the result) and consumes them. Running background
  tasks are a section of the context note. Subagent completions reach a
  running parent the same way; an idle parent still gets a triggered run.
- **Processes** (`internal/shelltask`): `bash -lc` in its own process group,
  stdout and stderr through a pipe into `<session files>/<session>/tasks/
  <task>.log` (0600). Past 20 MB the file is rewritten as the first 1 MB, a
  fixed-width marker with the dropped byte count, and the newest half of the
  room; reads use output offsets, so a cursor survives the rewrite.
  Foreground commands write the same file, removed when they finish unless
  their output was longer than the call returns (30 000 bytes; the result then
  names the file). Once the shell of a foreground command exits, the call
  waits 2 s for commands it left behind to close the output and then stops
  reading it; a command in the background, started there or moved there,
  keeps its output open until they close it.
- **Bash**: `timeout` and `auto_background_after` are seconds (default 600 and
  120; timeout at most 3600). A command moved to the background keeps its
  timeout; one started with `run_in_background` has none. A session runs at
  most `daemon.background_tasks` (default 8) background commands: past that
  `run_in_background` fails and a slow command stays in the foreground.
  `task_output{id, wait_seconds ≤ 600, filter}` returns at most 30 000 bytes
  from the task's cursor (a filter keeps matching lines; the cursor still
  advances); it waits for a subagent task too, and calls for one task take
  turns (concurrency key `task:<id>`);
  `task_kill{id}` asks for approval like `job_kill` did and also cancels a
  subagent task. The shell tools are registered with the core as their
  `tools.ShellTasks`.
- **Restart**: the daemon holds an exclusive `flock` on
  `matrixclawd.lock` in its data directory for its lifetime, so a second
  daemon on the same data exits with an error. `RecoverTasks` runs before the
  workflow worker starts; a leftover group is killed only when its leader
  still runs in the same boot (`/proc/sys/kernel/random/boot_id`, macOS
  `kern.boottime`) with the start recorded at spawn (`/proc/<pid>/stat`
  starttime, macOS `kern.proc.pid`); a leaderless group or one that cannot be
  checked is only marked lost. Lost tasks do not start a run; the next run
  reads them, and `RecoverActiveRuns` (after the run starter exists) wakes a
  run waiting for them. Deleting a session kills its tasks without waking
  anything in it.
- **`/tasks`** lists the bound session's background tasks above the scheduled
  tasks (`/tasks bg <id>` shows the output tail, `stop` asks first); API
  `GET /v1/sessions/{id}/tasks`, `GET /v1/tasks/{id}`,
  `POST /v1/tasks/{id}/cancel`. A task the user stops is an event for the
  session.

### Implementation notes (as built, stage 6b)

- **await** is a core tool (`await{ids?, timeout_seconds ≤ 3600, default
  600}`, category automation, so children lack it). Its result is written at
  once ("Waiting up to … for …"); `tools.Result.Await` puts `{task_ids, until}`
  into `Counters.Await`, so the park survives an approval or a restart. Named
  tasks that already finished are reported (alone when none still runs);
  with no running background task it says so. It needs a run. Task ids come
  from the results of the calls that start the tasks, so its description asks
  for it after they returned: in the same reply it may run before them.
- **Park**: each step starts with `resumeAwait`: it clears `Counters.Await`
  when a steer is pending (written as a user message, since no tool result can
  carry it), an event for an awaited task (any task without ids) is pending,
  or `until` passed (an engine note says the await timed out); otherwise the
  step checkpoints and the run ends `waiting_events`. Events for other tasks
  stay pending. Core stores `run_wakeups(run_id, session_id, wake_at ms,
  task_ids_json)` and the status; a woken run that finds nothing parks again
  without a model call.
- **Wake**: `wakeWaitingRun` runs under the session gate and starts the run
  when its timer ran out, a steer for it is pending or an awaited task
  finished unseen; it deletes the wakeup once the run is started, so a failed
  start is retried. It is called when a task finishes,
  when a message arrives, by `WakeDueRuns` (a one-second ticker; overdue
  timers fire on its first tick after a restart), at startup for every
  waiting run, and after the parking run is no longer active
  (`resumeParkedRun`), which closes the race with an event that arrived while
  it parked. `prepareClaimedRun` accepts a waiting run. A background
  subagent's approval is asked in its parent's session (Telegram shows it
  while the parent waits for events) but never parks the parent's run; a run
  found waiting for approval with a wakeup loses the wakeup and resumes once
  nothing is pending. Canceling a run drops its wakeup and stops the
  background commands and subagents it started; canceling a parked run
  starts the next queued message, and tasks of a canceled run wake nothing.
- **Input**: a message without a busy mode steers (Telegram, the iOS app and
  controlplane skill prompts send none; the TUI sends steer unless `/queue`
  or `/busy` picks another mode); a queued
  message for a waiting run steers it too, and messages queued while a run
  worked become steers that wake it once it waits.
- **Idle sessions**: a finished background task (a command that exited by
  itself, or any subagent) in an idle top-level session starts a run with
  trigger `wake` (automation budget) whose user message says background work
  finished; the events are journaled after it as notes and delivered to it
  under the session gate before it starts, so no run reads them twice. It gets the
  client, capabilities and delivery address of the newest run whose delivery a
  later message still reaches (a Telegram chat, never a guest or inline
  query), so Telegram receives its reply. Twenty wake runs in a row that took
  no user message (a steer into one ends the chain; automation runs neither
  count nor end it) stop the chain: the session shows a system message and
  the same chat gets a `notice` delivery per finished task. A wake run that failed or was
  canceled stops the chain until the user writes; a failed one tells the user
  once, the same way. Stopped and lost commands wait
  for the next run. Subagent completion runs are wake runs now; their prompt,
  trigger IDs and `ListPendingSubagentCompletionTasks` are gone.
- **Clients**: the TUI counts `waiting_events` as busy ("Waiting for background
  tasks"); Telegram shows progress, no typing, and "Waiting for background
  tasks..."; the iOS package decodes it as `.unknown`.

## 5. Providers

- `providers.Request` gains `MaxOutputTokens` (priority: provider config →
  model catalog → 16k), `CacheHints`, and `ToolChoice` (`auto | none`).
- `providers.Response` gains `StopReason` (`end_turn | tool_use | max_tokens |
  refusal | content_filter`); usage is normalised as in §3.
- All adapters use idle (between-chunk) stream timeouts instead of whole-request
  timeouts.
- openaicompat: `length` maps to `max_tokens`; `tool_choice: none`.
- anthropiccompat (stage 1b): real `tool_use` / `tool_result` blocks, parallel
  tool results in one user message, `cache_control` on tools (more in stage 3),
  `stop_reason` mapping,
  `tool_choice: {type: none}`, thinking blocks passed back unchanged.
- gemini: decode `finishReason` (`MAX_TOKENS`, `SAFETY`,
  `MALFORMED_FUNCTION_CALL` → retry once, …); send all function responses of a
  batch in one content; pass thought signatures back; `mode: NONE` for the
  final turn.
- openaicodex: `incomplete` with `max_output_tokens` maps to `max_tokens`
  instead of an error; request `reasoning.encrypted_content` and send it back
  so reasoning survives between steps with `store:false`.
- Until stage 2a lands, the old loop keeps treating `max_tokens` as an error.

## 6. Todo, prompt, clients, observability, data

**Todo.** `todo_write{items: [{content, active_form, status}]}` replaces the
whole list; at most one `in_progress`. Stored in `session_todos` (one row per
session: JSON items, `chain_run_id`, `updated_run_id`). Event `todo_updated`;
the TUI plan panel becomes a todo panel; Telegram shows it in the run status
message; API `GET/DELETE /sessions/{id}/todo` replaces `/plan`. Deleted:
`internal/core/plan*.go`, `types_plan.go`, `internal/store/sqlite_plan.go`,
`internal/controlplane/plan.go`, `internal/api/plan.go`, TUI `plan_run.go`,
`app_plan_keys.go`, plan events, `/plan` in `commandcatalog`, `PLAN_BLOCKED`.

**Prompt.** Guidance is added together with the feature it describes (todo in
stage 5, parallel calls in 4c, background and `await` in 6a/6b). The final
text: track multi-step work in todo and update it as you go; issue independent
reads/searches as parallel calls in one reply; finish with a verified result
(tests, build, real output); report failures honestly; do not stop at an
intermediate step; run long commands in the background and wait with `await`.
"Minimise tool calls" is removed in stage 2b.

**Clients.**
- Each stage ships the client handling for what it introduces.
- iOS Swift package: `RunStatus` gets tolerant decoding (unknown values map to
  `.unknown(String)`) in stage 0, before any new status exists.
- Telegram: one editable run-status message per run (step n/limit, todo,
  current tool, background tasks) instead of one message per tool call;
  deliveries load messages with `ListMessagesAfter(seq)`; "Continue" button by
  `stop_reason`; approval buttons "Allow / Always allow / Deny with reason".
- controlplane (TUI + Telegram): `/continue`, `/budget`, `/permissions`
  (list/delete rules), `/tasks` (background tasks).

**Observability.** `run_steps(run_id, step, model, prompt_tokens,
cache_read_tokens, cache_write_tokens, output_tokens, stop_reason,
latency_ms, tool_calls, created_at)` replaces the full-scan `saveRunUsage`
rebuild; per-run usage and budget are sums over it, so continuations, final
turns, nudges and summaries are counted. Each step emits a `run_step` event
and one structured log line.

**Data.** Schema stays idempotent: `ensureColumn` for `messages.seq` (with
backfill), `runs.stop_reason`, `runs.continues_run_id`; `CREATE TABLE IF NOT
EXISTS` for `run_steps`, `run_wakeups`, `permission_rules`, `session_todos`,
`tasks`; `subagent_tasks` copied into `tasks` and dropped; `DROP TABLE IF
EXISTS session_plan_items, plan_runs`. Existing plans are not migrated (noted
in CHANGELOG). Runs in `waiting_approval` at upgrade time are picked up by the
new recovery; checkpoints without budget counters start from zero.

### Implementation notes (as built, stage 5)

- **Where it lives**: `internal/agent/todo` holds the items, their checks and
  their text; `todo_write` is a core tool (`core/todo.go`); the engine asks a
  `Todos` port for the open items of a chain and knows nothing else of todo.
- **Tool**: `content` and `status` are required, `active_form` optional; at
  most 50 items and one `in_progress`; an invalid list is not saved and the
  model reads `Todo list not saved: <what to correct>.` The tool is read-only
  (it replaces the agent's own list, so recovery replays it without asking)
  and allowed for subagents explicitly. Its calls take the concurrency key
  `todo:<session>`, so parallel `todo_write` calls of one session replace the
  list one after another.
- **Chain**: `chain_run_id` is the first run of the writer's `/continue`
  chain (followed back up to 32 runs); a run's chain holds the list when it
  contains `chain_run_id` or `updated_run_id`.
- **Context note**: the list is a section of the note whenever it has items,
  whichever chain wrote it, in subagents too. Without the plan section a run
  with no status, memory change, recovery notice or todo sends no note.
- **Completion check**: the reply before the nudge is kept (finish
  `end_turn`); the nudge is an `engine_model` note, sent once per run
  (`Counters.TodoNudged`), not when a limit is reached; a list that cannot be
  read holds nothing back.
- **Clients**: event `todo.updated` (named like the other events); the TUI
  panel is read-only, shown while items are open and toggled with `ctrl+n`;
  `/todo` and `/todo clear` replace `/plan`; Telegram edits one silent message
  per run built from the run's last successful `todo_write` call. `DELETE
  /sessions/{id}/todo` answers with the emptied list. The iOS package is
  unchanged: no API type it decodes referred to plans.
- **Removed**: the plan runner prompts are no longer filtered from provider
  requests, so old ones in a history reach the model as user messages;
  `Conversation` and `TextOnlyConversation` lost their run ID parameter.
  `session_goals`, `session_plan_items` and `plan_runs` are dropped on open.

## Testing

- `internal/agent` with `agenttest.ScriptedModel`: a 300-step run; budget
  final turn with `tool_choice: none`; `max_tokens` continuation and truncated
  tool JSON; open-todo completion check scoped to the chain; no-progress loop
  guard ignoring polling with new output; mid-run summary preserving
  assignment, tail and call/result pairs across `Load`; elision; parallel batch
  result order; concurrency keys across two runs; approval barrier with
  `deferred` calls; denial with reason (direct, bridged, after restart); deny
  rule on a read-only tool; compound bash commands; `await` woken by event, by
  timer, by user message, and the park/event race; `interrupted` rescheduling;
  cancel cascade; recovery with budget counters.
- `internal/permission`: table tests for subjects, rule order and bash parsing.
- Existing core integration tests (SQLite, crash recovery) move to the new
  engine; duplicates are deleted.
- Per provider, against a local HTTP server: stop reasons, `max_tokens`,
  normalised usage, `tool_choice: none`, idle timeout; Anthropic tool round
  trip; Gemini batched function responses.
- iOS package: decoding of an unknown run status.
- Each stage: `go vet ./...`, `go test ./...`, and a manual long task through
  the TUI on the test stand.

## Stages

Each stage builds, passes tests, keeps the app working, and is committed to
`main` on its own; a release happens on the owner's command.

| Stage | Content |
|---|---|
| 0 | Preparation: `internal/transcript` move, `messages.seq` + point store methods, normalised `Usage`, idle stream timeouts, tolerant iOS `RunStatus`, `run_steps`. |
| 1 | Provider contract: `StopReason`, `MaxOutputTokens`, `ToolChoice`, Gemini `finishReason`, Codex incomplete/encrypted reasoning. Old loop still treats `max_tokens` as an error. |
| 1b | Native Anthropic tool use, `cache_control`, stop reasons. |
| 2a | Engine extraction with unchanged behaviour (32 steps), single-writer journal, `ExecuteRun` switched over, old `execution_*`/`context*` deleted, `interrupted` rescheduling. |
| 2b | Budget, final turn, loop guard, `stop_reason`, `/continue` + Telegram button, engine messages. Default step limit stays 32. |
| 3 | Context: `seq` boundary, large outputs to files, elision, cache-friendly summary, stable prompt and context messages, caching hints. Defaults raised to 300 steps / 4 h. |
| 4a | Denial returned to the model in all three places; approval barrier and `deferred`. |
| 4b | `internal/permission`, rules in `core.ExecuteTool`, approval UI, `/permissions`; delete Telegram in-memory auto-approval. |
| 4c | Scheduler: concurrency keys, parallel batches, engine-only checkpoints, provider semaphore. |
| 5 | Todo replaces Planning Mode (can follow 2b directly). |
| 6a | `tasks` table, durable shell tasks, bash timeout and auto-background, `/tasks`. |
| 6b | `await`, `waiting_events`, `run_wakeups`, auto-wake chains, steer wakes. |
| 6c | Unified `agent` tool (readonly, runtime, model), parent resume on whole batch, cancel cascade. |
| 7 | Docs: `ARCHITECTURE.md`, `PLANNING.md` → todo, `TELEGRAM.md`, CHANGELOG. |

## Deferred

File checkpoints/rollback, fallback model on overload, deferred MCP tool
loading (tool search), user hooks, re-attaching background processes after a
restart, cost budgets (needs a price source).
