# Goals

A session goal is a long objective the native agent works on by itself, run
after run, until it is verifiably done: the model reports it complete with
evidence, the goal's check command passes and no todo item is open.

```text
/goal Port the config loader to the typed setup package --check "go test ./internal/setup/..." --max-runs 20
```

## Setting and stopping a goal

| Command | What it does |
|---|---|
| `/goal <objective> [--check "cmd"] [--max-runs N] [--tokens N]` | sets the goal (replacing the session's goal, if any) and starts its first run when the session is idle |
| `/goal` | shows the goal: status, check and its last result, runs and tokens used, the last run, the blocker reported |
| `/goal pause` | stops starting runs; the run working now finishes |
| `/goal resume` | goes on with a paused or blocked goal; when its runs or tokens were used up, with a fresh allowance |
| `/goal clear` | removes the goal |

- The objective is at most 2000 bytes; put long specifications in a file and
  name it. `--max-runs` defaults to 10 and is at most 50. `--tokens` caps the
  tokens of all goal runs together (and the token budget of each goal run at
  what is left). Like any token budget it is checked between steps: the step
  that crosses it still finishes, and one closing turn without tools reports
  what is done. Each step resends the whole prompt (often 10–30k tokens with
  tools and project context), so a cap below that stops after the first step.
- Option values may be quoted; the objective is the text before the first
  option.
- One goal per session, only in native top-level sessions (not external
  agents, not subagents).
- The check command runs without approval in the session's working directory,
  so only the owner sets one (the API answers 403 to others). In a session that
  runs unattended (`full_auto`, external agent) only the owner changes the goal
  at all, as with every write there.
- The model cannot create goals: there is no `goal_create` tool. A goal is the
  user's decision; a tool asking for approval would be one more approval in
  `full_auto` sessions, which approve everything.

API: `GET|PUT|DELETE /v1/sessions/{id}/goal`, `POST
/v1/sessions/{id}/goal/{pause|resume}` (policy `session(id)`; `PUT` with a
`check_command` is owner only). Clients get `goal.updated` events with the
goal (`null` once cleared), and the snapshot carries it.

## Statuses

```text
active ─▶ complete            (goal_update complete, check passed, no open todo)
active ─▶ blocked             (same blocker in 3 consecutive goal runs, or the
                               same check failure 3 times in a row)
active ─▶ paused              (/goal pause, a canceled or failed run, the runs
                               or tokens used up)
paused, blocked ─▶ active     (/goal resume)
```

## How runs continue

When a run of the session ends (`afterEnd`), core:

1. counts it against the active goal, once (`settleGoalRun`): a run the goal
   started (trigger `goal`) uses one of its runs and its tokens. A canceled or
   failed run, or a used-up allowance, pauses the goal, with a notice in the
   session and where its runs are delivered. Runs that started before the goal
   do not count.
2. starts the session's next queued message: the user's input always goes
   first, and the goal waits until that run has ended too.
3. wakes the session for finished background work.
4. starts the goal's next run (`continueGoal`) when the session is still idle.

A goal run's user message says to continue the goal; it continues the run
before it (`continues_run_id`), so its todo list and what summaries keep
verbatim carry over. It is delivered where the session's latest delivered run
went (the first one to the client that set the goal, when that client takes
deliveries). It has the user run budget. No run starts while one waits for an
approval or for events: such a run has not ended. `Core.Recover` continues
active goals of idle sessions at daemon start.

## What the model sees

Runs that start with an active goal get the tools `goal_get` and
`goal_update`, and a goal section in the context note:

- the objective; the check and its last result; goal runs used of the
  allowance (and tokens); how the last run ended; the blocker reported so far;
- a **no-progress check**: classify the previous run as progress, verified
  wait or no progress, and change approach after no progress;
- **fidelity**: work toward the whole objective, never narrow it;
- a **completion audit**: map every requirement to authoritative evidence
  before reporting the goal complete.

`goal_update {status: complete, evidence}` is accepted only with evidence,
without open todo items in the run's chain, and after the check command passes
(it runs inside the call). `goal_update {status: blocked, blocker}` records the
blocker; the goal becomes blocked only when the same blocker (compared without
case and extra spaces) is reported by 3 consecutive goal runs, each continuing
the one before. Until then the model is told to try another approach.

## The verification gate

When a run of a session with an active goal and a check command replies
without tools, the engine asks its `Verifier` port (`coreVerifier`) before it
lets the run end, after the todo and verify nudges and before the Stop hooks:

- The check runs as `bash` in the session's working directory with the
  environment of shell commands (no API keys), stderr joined to stdout, for at
  most 10 minutes.
- **Fails:** the reply is kept and the model gets an engine note with the exit
  code (or timeout) and the last 4 KB of output, and goes on. A run is held
  back at most 5 times this way (`RunState.GoalHolds`).
- **The same failure 3 times in a row** (exit code and output, digits ignored
  so timings do not count; across runs too) blocks the goal; the run ends with
  a visible note.
- **Passes:** a visible note "Verification passed" and the run ends. The goal
  stays active until the model reports it complete, so the next goal run
  starts with the passing check in its context.

Code: `internal/core/goals.go` (status, settling, continuation),
`goal_check.go` (check and Verifier), `goal_tools.go` (tools and context note),
`internal/agent/completion.go` (`goalNote`), table `session_goals`
(migration `004_session_goals.sql`).
