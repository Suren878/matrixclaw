# Long-running native agent — design

Status: approved in brainstorming on 2026-09-23. Sub-project 1 of 2.

Sub-project 2 (daemon-owned goals that wake themselves on timers/events and
work across many runs, Hermes-style) gets its own spec later and builds on the
`await` / `waiting_events` mechanism defined here.

## Goal

A native MatrixClaw run can work on one assignment for hours and hundreds of
tool calls, the way Claude Code does: it keeps going until the work is done and
verified, survives context growth, restarts and denied approvals, runs
independent work in parallel and in the background, and stops on an explicit
budget with a usable summary instead of failing.

## Problems in the current loop

Baseline: commit `2445681` (generation retry, checkpoints, crash recovery,
terminal stream events).

- `maxRunToolSteps = 32` fails the run when reached (`internal/core/execution.go`).
- The first text-only reply ends the run; there is no completion check.
- Compaction keeps every message of the current run
  (`latestCompactSummaryForRun`), so a long run overflows and fails.
- A denied approval fails the whole run; the model never sees the denial.
- The plan is advanced only by the terminal client (`plan_run.go`).
- 4096 default output tokens; `finish_reason=length` is an error;
  `providers.Response` has no stop reason.
- Tools run strictly sequentially; background bash jobs live in memory only.
- The system prompt changes every turn (plan, memory, status), defeating prompt
  caching; the full session is re-read from SQLite several times per step.
- The prompt tells the model to minimise tool calls.

## Decisions

| Topic | Decision |
|---|---|
| Uncommitted 2026-09-17 work | Committed as the baseline (`2445681`). |
| Scope | Long single task first; goals/wake-ups are sub-project 2. |
| Stop condition | Configurable budget (steps, wall-clock, tokens, cost) with a tool-less final turn; the run is continuable. |
| Approvals | Allow/deny/ask rules; a denial is returned to the model; pending approval parks the run. |
| Planning Mode | Replaced by a run-loop `todo_write` tool; plan tables, tools and the terminal runner are deleted. |
| Architecture | New `internal/agent` engine behind narrow ports; `core` serves it. |

## 1. Boundaries

```
internal/agent/            loop: Engine.Run(ctx, Task) -> Outcome
  budget.go                steps, tokens/cost, wall-clock; final turn
  loopguard.go             repeated-call detector (hash of name+args)
  transcript.go            in-memory run transcript, append-only writes
  context/                 window, tool-result elision, summary, token accounting
  toolsched/               batches: shared calls in parallel, exclusive alone
  permission/              allow/deny/ask rules, mode presets
  todo/                    todo_write tool and state
  prompt/                  stable system prompt, per-step context block
  agenttest/               ScriptedModel and fakes for the ports
```

Ports implemented by `core`:

```go
type Model interface {
	Generate(ctx context.Context, req providers.Request) (providers.Response, error)
}

type Journal interface {
	Load(ctx context.Context, sessionID string) (Window, error) // once: everything after the last compaction boundary
	Append(ctx context.Context, msg Message) error
	Update(ctx context.Context, msg Message) error
	Checkpoint(ctx context.Context, state State) error // budget counters, phase, loop-guard hashes
}

type Tools interface {
	Specs(task Task) []tools.Spec
	Execute(ctx context.Context, call tools.Call) (tools.Result, error)
}

type Approvals interface { Request(ctx context.Context, p Pending) error }
type Inbox interface { Drain(ctx context.Context, runID string) ([]Event, error) } // steer, task_finished, subagent results
type Sink interface { Emit(Event) } // streaming to TUI/Telegram/iOS
```

`Outcome.Status` is one of `completed`, `budget_exhausted`, `waiting_approval`,
`waiting_events`, `canceled`, `failed`. `budget_exhausted` is not a failure.

`core` keeps message acceptance, queue/steer/interrupt inputs, run status,
approvals, checkpoint-based crash recovery and external agents (Claude Code,
Codex — unchanged). `ExecuteRun` becomes: load → `engine.Run` → apply `Outcome`.

Deleted without replacement shims: `execution.go`, `execution_turn.go`,
`execution_request.go`, `execution_conversation.go`, `execution_generation.go`,
`execution_tools.go`, `execution_prompts.go`, `context*.go`, `plan*.go`,
`types_plan.go`, `PlanStore`, `maxRunToolSteps`,
`clients/terminal/chat/runtime/plan_run.go`. Their logic moves into
`internal/agent`; nothing is duplicated.

## 2. Loop

Each step of `Engine.Run`:

1. **Inbox.** Drain steer input and background/subagent completions; each
   becomes a journal message.
2. **Context.** Build the window; compact if over threshold (section 3).
3. **Model.** `Generate` with the existing bounded retry. Branch on `StopReason`:
   - `tool_use` → run the tool batch (section 4), next step.
   - `max_tokens` → keep the partial answer, append "your reply was cut by the
     output limit; continue exactly where you stopped". At most 3 consecutive
     continuations, then `failed`.
   - `end_turn` → completion check (below).
   - `refusal` / `content_filter` → `completed`; the refusal text is shown as is.
4. **Checkpoint.** Persist budget counters, phase and loop-guard hashes.

**Completion check.** If the todo list has `pending`/`in_progress` items and
budget remains, inject once: "Your todo list has N open items: […]. Continue,
or mark them and explain why you are stopping." A second text-only reply
completes the run; the user sees the open items. No repeated nudges.

**Budget.** Per-session settings with daemon-config defaults:

| Limit | Main run | Subagent |
|---|---|---|
| steps | 300 | 100 |
| wall-clock | 4 h | 1 h |
| tokens | 0 = unlimited | 0 = unlimited |
| cost | counted when the model catalog has prices | same |

At 80% of any limit the context block says "about X left, wrap up". At 100% a
**final turn** runs with tools disabled ("briefly: what is done, what remains,
how to continue") and the run ends `budget_exhausted`. `/continue` (TUI) or the
"Continue" button (Telegram) starts a new run with a fresh budget over the same
journal and todo. Counters live in the checkpoint and are not reset by an
approval resume.

**Loop guard.** Hash of `(name, args)`: 3 identical consecutive calls (or 3
identical consecutive errors) → context-block warning "you are repeating X,
change approach"; 5 → final turn, `failed` with reason `loop_detected`.

**Cancel/interrupt** behave as today: partial answer kept, unfinished calls get
a "canceled" result. User input during a run defaults to steer (delivered at
the next step without stopping the run); queue/interrupt unchanged.

## 3. Context

**Token accounting.** Authority is the last response's
`InputTokens + CachedTokens` plus an estimate (runes/4, 1500 per image) for
everything appended since. Without provider usage, fall back to the estimate.
Window size from the model catalog; unknown models use 128k.

**Level 1 — elision** at 60% of the window, no model call. Tool results older
than the last 5 tool rounds and larger than ~1k tokens are replaced in the
request by `[output of read(path=…) hidden, ~12k tokens; call again if needed]`;
images older than 3 steps likewise. The database keeps everything. Elision is
applied to all eligible results at once, not as a sliding window, so the
request prefix changes rarely.

**Level 2 — summary** at 80%, or when elision was not enough. Summarise
everything except the tail (the last ~25% of the window, whole turns, never
splitting a call from its result). The run's original assignment and all steer
messages are kept verbatim. Summary structure: goal, decisions, files changed,
errors and fixes, current state, next step. Todo is not summarised — it is
re-injected by the context block. If the part to summarise does not fit the
model window it is summarised in chunks and merged. The summary is stored as a
compaction boundary with `run_id`; `Journal.Load` starts at the last boundary.
The current-run exception is removed.

**Context-length error from the provider.** Force a summary with half the tail
and retry once; a second overflow is `failed` with a clear reason. If automatic
compaction is disabled after two low-yield attempts (<10% saved) and the window
is still over the threshold, run the final turn and end `failed` with reason
`context_exhausted`.

**Stable prompt and caching.**
- The system prompt changes only between runs: identity, rules, tools, project
  root, skills, memory as of run start.
- The per-step **context block** is rebuilt every step, never journaled, and
  appended as tagged text to the last message: todo, remaining budget, time,
  module status, loop-guard warnings, recovery notice, memory changes made
  during the run.
- Anthropic: `cache_control` on system+tools and on the second-to-last message
  (rolling breakpoint). OpenAI-compatible: `prompt_cache_key = sessionID` where
  accepted. Gemini: implicit caching.
- Cache hit rate from `CachedTokens` is shown in `/status`.

## 4. Tools, permissions, background work, subagents

**Scheduler.** `tools.Spec` gains `Concurrency: shared | exclusive`, defaulting
from `Effect` (`readonly` → shared, `mutation` → exclusive). Consecutive shared
calls run in parallel (max 8); each exclusive call runs alone. Results are
journaled in the model's original order, with a checkpoint per call. Unknown or
invalid calls keep becoming error results. `agent` is shared.

**Permissions.** Rule `{tool, pattern, effect: allow|deny|ask, scope:
session|global}` in table `permission_rules`. The matched subject comes from
the tool's `PermissionParams`: bash → command (`go test:*` prefix syntax),
write/edit → path glob, web_fetch → domain, MCP → `server__tool`. Order: deny →
ask → allow → mode preset → tool default. `default` / `accept_edits` /
`full_auto` stay as named rule presets. Compound bash commands (`&&`, `;`, `|`,
`$(...)`) are parsed with `mvdan.cc/sh/v3` (new pure-Go dependency); allowed
only if every sub-command is allowed, otherwise ask.

Approval prompt offers "Allow", "Always allow" (with a suggested rule such as
`bash: go test:*` or `edit: internal/**` and a session/global choice) and "Deny
with reason". A denial becomes an error result `User denied: <reason>` and the
loop continues; a deny rule yields `Blocked by rule <…>` immediately. Calls in
the batch that need no approval still execute; the run parks in
`waiting_approval` at the end of the batch and resumes when every pending
approval is resolved.

**Background tasks.** Table `background_tasks`: `id, session_id, run_id, kind
(shell|subagent), status, command/goal, pid, output_path, exit_code,
started_at, finished_at`.
- `bash(run_in_background)` starts the process with `setsid`, output to a file
  in the data directory; the process survives a daemon restart. On restart the
  daemon re-attaches by pid and process start time, otherwise marks it `lost`.
- `task_output(id, wait_seconds?, filter?)` and `task_kill(id)` replace
  `job_output` / `job_kill`.
- Completion emits `task_finished` into the session inbox.
- `await(ids?, timeout)` parks the run in `waiting_events` without holding a
  goroutine; the timer is persisted; the run wakes on the first matching event
  or the timeout. Sub-project 2 reuses this wake-up path.

**Subagents.** `delegate_task` and `spawn_subagent` merge into
`agent{description, prompt, background, isolation: shared|worktree}`.
`background:false` runs the child on the same `Engine` inside the call (several
in parallel); `background:true` makes it a `subagent` background task whose
result arrives as an event. Limit: 4 active background subagents per parent,
configurable. Children get todo and background bash; `agent` and `await` are
forbidden. Child approvals are still bridged to the parent.

## 5. Providers, todo, prompt, clients, data

**Providers.** `providers.Request` gains `MaxOutputTokens` (from the model
catalog, default 16k), `CacheHints`, `ToolsEnabled`. `providers.Response`
gains `StopReason` (`end_turn | tool_use | max_tokens | refusal |
content_filter`). All four adapters (openaicompat, anthropiccompat, gemini,
openaicodex) map their native reasons; `finish_reason=length` is no longer an
error. A stream without a terminal event stays an error.

**Todo.** `todo_write{items: [{content, active_form, status}]}` replaces the
whole list; at most one `in_progress`. Stored in `session_todos` (one row per
session: JSON items, `updated_run_id`); survives compaction and `/continue`.
Event `todo_updated`; TUI plan panel becomes a todo panel
(`app_plan_panel.go` → `app_todo_panel.go`); Telegram shows it in the run
status; API `GET/DELETE /sessions/{id}/todo` replaces `/plan`.
Deleted: `internal/core/plan*.go`, `types_plan.go`, `internal/store/sqlite_plan.go`,
`internal/controlplane/plan.go`, `internal/api/plan.go`, TUI `plan_run.go`,
`app_plan_keys.go` and plan events, `/plan` in `commandcatalog`, `PLAN_BLOCKED`.

**Prompt.** "Tool use discipline" is rewritten: track multi-step work in todo
and update it as you go; issue independent reads/searches as parallel calls in
one reply; finish with a verified result (tests, build, real output); report
failures honestly; do not stop at an intermediate step; run long commands in
the background and wait with `await`. "Minimise tool calls" is removed.

**Clients.** `/continue`, `/budget`, `/permissions` (list/delete rules) in
controlplane for TUI and Telegram; "Continue" button for `budget_exhausted`
runs; new approval buttons; run status shows steps/budget, background tasks and
todo. iOS Swift package: new event and status fields only.

**Data.** Schema stays idempotent: new tables via `CREATE TABLE IF NOT EXISTS`;
`DROP TABLE IF EXISTS session_plan_items, plan_runs` (the session goal goes with
them). Existing plans are not migrated; noted in CHANGELOG. Runs in
`waiting_approval` at upgrade time are picked up by the new recovery; old
checkpoints without budget counters start from zero.

## Testing

- `internal/agent` with `agenttest.ScriptedModel`: a 300-step run; budget final
  turn; `max_tokens` continuation; open-todo completion check; loop guard;
  mid-run summary preserving assignment, tail and call/result pairs; elision;
  parallel batch result order; denial with reason; deny rule; compound bash
  command; `await` woken by event and by timer; daemon restart with a live
  background process; recovery with budget counters.
- Existing core integration tests (SQLite, crash recovery) move to the new
  engine; duplicates are deleted.
- Per provider: `StopReason` and `max_tokens` against a local HTTP server.
- Each stage: `go vet ./...`, `go test ./...`, and a manual long task through
  the TUI on the test stand.

## Stages

Each stage builds, passes tests and is committed to `main` on its own; a
release happens on the owner's command.

1. Provider contract: `StopReason`, `MaxOutputTokens`, cache hints.
2. Engine, transcript, budget, final turn, loop guard; `ExecuteRun` switched
   over; old `execution_*` deleted. The completion check is wired but inert
   until stage 5 provides todo.
3. Context: in-run summary, elision, usage-based accounting, stable prompt.
4. Tools: scheduler, permission rules, denial feedback.
5. Todo replaces Planning Mode.
6. Background tasks, `await`, unified `agent` tool.
7. Prompt, clients, docs (`ARCHITECTURE.md`, `PLANNING.md` → todo, `TELEGRAM.md`).
