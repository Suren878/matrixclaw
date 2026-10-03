# Core run lifecycle — design

Status: built (Phase C #1 of the 2026-10-03 audit cleanup; core report
items 4, 12, 14, 15, 16, 17 and the area verdict).

## Current shape and what is wrong with it

- **Run status is written from about ten places**: `setRunStatus`
  (`run_status.go`), `completeAssistantTurn` (`run_outcome.go`), `parkRun`
  (`run_wakeups.go`), `prepareNativeRun` (`run_execute.go`),
  `tryExecuteExternalAgentRun` and `touchExternalRunActivity`
  (`external_agent_execution.go`), `cancelRunRecords` (`runs.go`),
  `prepareRunAfterCrash` (`run_recovery.go`), `mirrorPendingSubagentApproval`
  (`subagents.go`) and `syncBlockingSubagentTaskAfterRun`
  (`subagents_lifecycle.go`). Which status may follow which exists only in
  comments. What happens once a run stops is spread over four defers in
  `ExecuteRun` (`afterRunExecution`, `resumeParkedRun`,
  `rescheduleInterruptedRun`, `noticeFailedWakeRun`) plus `CancelRun` and
  `healApprovalPark`; wakeups are saved in `parkRun` and deleted in three
  other places.
- **Two Go views of one `tasks` row**: `SubagentTask` (`types_subagent.go`,
  `sqlite_subagents.go`, an 8-method `SubagentTaskStore`, event
  `subagent.updated`) and `Task` (`types_task.go`, `sqlite_tasks.go`, event
  `task.updated`) name the same columns differently (`Goal`/`Command`,
  `DisplayName`/`Description`, `ParentSessionID`/`SessionID`,
  `Mode`/`Background`). `syncAsyncSubagentTaskAfterRun` re-reads the row as
  the other type.
- **Subagent approvals are copied into the parent** as a second approval row
  whose link to the child is JSON in `params_json`
  (`source: subagent_approval_bridge`, decoded by
  `decodeSubagentApprovalBridge` in 8 places, `subagentBridgeApprovalForChild`
  decodes every approval of the session). A blocking child that asks parks the
  parent in `waiting_approval`; when the child ends, core replays the parent's
  `agent` call through `ExecuteTool` (`replayInterruptedTool`), i.e. core
  writes a parked run's transcript, and then restarts the parent. Supporting
  code: `finishOrBridgeSubagentTask`, `waitForSubagentStep`,
  `resumeParentForSubagentStatus`, `runWaitsForBlockingChild`,
  `bridgedCallWaitsForSubagent`, `backgroundSubagentApproval`,
  `rejectChildApprovalCopies`, `passDecisionToSubagent`.
- **Recovery writes the transcript itself** (`deferInterruptedCall`,
  `finishUnknownInterruptedTool`, `markLatestPartialAssistantInterrupted`,
  `replayInterruptedTool`) and has two entry paths
  (`prepareInactiveRunForRecovery` at startup, `prepareClaimedRun` at claim).
  The daemon calls five recovery functions in `daemoncmd/run.go`;
  `RecoverSubagentTasks` starts child runs `RecoverActiveRuns` just started
  (only the 30 s lease stops it) and stops at 200 tasks.
- **Cancel is signalled twice**: `CancelRun` cancels the run's context, and the
  engine also reads the run row every 750 ms (`Inbox.Canceled`) and in each
  batch; the external-agent loop polls the same way.
- **Core parses Telegram's delivery address** (`wakeTargetOf`: `kind`,
  `guest_query_id`, `inline_message_id`).
- **File versions are write-only**: `saveFileVersionSnapshot` writes
  `file_snapshots` and publishes `file.versioned`; nothing reads either (the
  iOS `EventType.fileVersioned` case only decodes the name).

## Target shape

### 1. One run lifecycle (`internal/core/run_lifecycle.go`)

```go
// runEdges are the status changes a run may make.
var runEdges = map[RunStatus][]RunStatus{
	RunStatusAccepted:        {RunStatusRunning},
	RunStatusRunning:         {RunStatusWaitingApproval, RunStatusWaitingEvents, RunStatusCompleted},
	RunStatusWaitingApproval: {RunStatusRunning},
	RunStatusWaitingEvents:   {RunStatusRunning},
} // plus: every non-terminal status may become failed or canceled

type runChange struct {
	To         RunStatus
	Err        string
	Stop       agent.StopReason
	Reply      *transcript.Message // sealed in the same transaction
	ReplySaved bool
	Await      *tools.Await // waiting_events
}

func (c *Core) transition(ctx context.Context, run *Run, change runChange) error
func (c *Core) afterRun(ctx context.Context, runID string) // idempotent
```

`transition` is the only writer of `runs.status`. It checks the edge, sets
`Status/Error/StopReason/UpdatedAt` and `FinishedAt` for every terminal status,
persists run and reply in one transaction (`store.SealRun`, replacing
`CompleteRun`; the store keeps its "an ended run stays ended" guard and returns
`ErrRunEnded`), saves the wakeup on `→ waiting_events` and deletes it when the
run leaves `waiting_events`, clears the checkpoint and rejects the run's pending
approvals on a terminal status, publishes `run.updated` (and the reply's message
event), syncs the subagent task whose child run this is (§2), wakes waiters of
the run's end (§3), and calls `afterRun` when the run has no executor.

`afterRun` is what follows a run that no longer executes, run by the executor's
release (`ExecuteRun`'s single defer) or by `transition` for an inactive run
(cancel of a parked run). It reads the run and: terminal → stop a subagent
session's shell tasks, requeue steers aimed at it, start the next queued input
or else wake the session for finished tasks, notice a failed wake run;
`waiting_approval` → start it if nothing is pending; `waiting_events` → wake
check; `running` (interrupted, kept) → reschedule while the daemon lives.
Every step is idempotent and gate-protected, so a double call is harmless.

`ExecuteRun` = claim → executor (engine or external agent) → apply outcome via
`transition`. `claim` holds the session gate: terminal → skip; `waiting_approval`
with a pending approval → skip; `running` (no executor, so interrupted) →
increment `RecoveryCount` (over 8 → `failed`) and build the recovery plan (§4);
then `→ running`. `touchExternalRunActivity` becomes `store.TouchRun` (only
`updated_at`). Deleted: `setRunStatus`, `failRun`, `failAcceptedRun`,
`failOrphanedRun`, `completeAssistantTurn`, `persistAssistantError`,
`completeExternalAgentRun`, `parkRun`, `healApprovalPark`, `resumeParkedRun`,
`rescheduleInterruptedRun`, `afterRunExecution`, `prepareClaimedRun`,
`prepareInactiveRunForRecovery`, `prepareRunAfterCrash`, `runNeedsCrashRecovery`.

**Run status transitions, today → after**

| Edge | Today (writer) | After |
|---|---|---|
| — → accepted | `createAcceptedRun` | same (creation, not a transition) |
| accepted → running | `prepareNativeRun`, `tryExecuteExternalAgentRun` | `claim` |
| running → waiting_approval | outcome (`setRunStatus`); interrupted outcome that reached it | outcome (`transition`) |
| running/waiting_events → waiting_approval | `mirrorPendingSubagentApproval` (blocking child asked) | **removed**: the parent stays `running` in its call (§3) |
| running → waiting_events | `parkRun` | outcome (`transition`, saves the wakeup) |
| waiting_approval → running | `prepareNativeRun` after `prepareClaimedRun` | `claim` |
| waiting_events → running | same; wakeup deleted in `wakeWaitingRun` | `claim`; wakeup deleted by `transition` |
| waiting_approval → accepted | `syncBlockingSubagentTaskAfterRun`, `prepareRunAfterCrash` | **removed** |
| running → accepted | `prepareRunAfterCrash` (then start) | **removed**: `claim` recovers in place (`running` stays) |
| running → running (interrupted, kept) | `preserveRunForRecovery` (checkpoint) | outcome seals the reply; status untouched; `afterRun` reschedules |
| running → completed | `completeAssistantTurn`, external `completeExternalAgentRun` | `transition` with `Reply` |
| any → failed | `failRun`, `persistAssistantError`, `failAcceptedRun`, `failOrphanedRun`, recovery limit | `transition` |
| any → canceled | `cancelRunRecords` (FinishedAt fixed in phase A) | `transition`; children first, as today |

### 2. One task model

`SubagentTask`, `SubagentTaskMode`, `SubagentTaskFilter`, `SubagentTaskStore`,
`sqlite_subagents.go`, `subagents_persistence.go` and `EventSubagentUpdated` go.
`Task` gains `Runtime`, `Model`, `Isolation`, `Readonly`; `TaskFilter` gains
`RunID`, `ParentToolCallID`, `ChildRunID`, `ChildSessionID`. The store has one
scanner, one column list and these task methods: `CreateTask`, `GetTask`,
`ListTasks`, `SetTaskStatus` (running/waiting_approval of an unfinished task),
`FinishTask(ctx, id, TaskEnd{Status, ExitCode, Summary, Error, Delivered, At})`
(once), `SetTaskCursor`, `MarkTasksDelivered`. One event `task.updated` for
both kinds. `result_message_id` is no longer written (column left for phase D).

Subagent task status follows its child run inside `transition`: child
`running` → task `running`, `waiting_approval` → `waiting_approval`, terminal →
`FinishTask` with the child's reply as summary (a canceled child is `canceled`;
today a canceled blocking child ends `failed`). A blocking task is delivered when
it ends (its parent reads it as the call's result); a background one becomes an
event for the parent session (`taskFinished`, unchanged). A parent's cancel
first finishes its children's tasks delivered ("Subagent canceled with its parent
run."), then cancels their runs, as today. New: a parent that fails cancels its
still-running blocking children.

Task status transitions (unchanged for shell tasks): created `running` →
`completed`/`failed` by exit code, `canceled` (task_kill, user, run canceled,
subagent finished, session deleted), `lost` (restart). Subagent: created
`running` ⇄ `waiting_approval` → `completed`/`failed`/`canceled`. `pending` is
never written (as today).

### 3. Subagent approvals are the child's own; a blocking parent waits in its call

`approvals.task_id` (new column) links a child's approval to its task. There
are no copies: `requestApproval` for a subagent run sets `TaskID`, publishes
`approval.requested`/`approval.resolved` with the **parent** session in the
event envelope, and queues an approval delivery to the chat the parent session
answers in (today only for background children). A read-only child's approval
is stored already rejected (`read-only subagent cannot run <tool>`).
`ListApprovals(session)` also returns the approvals of the session's subagent
tasks (`LEFT JOIN tasks`), with `AgentName` filled from the task. "Always allow"
on such an approval keeps the rule for the parent session (`task.SessionID`).
`ResolveApproval` has no bridge branch: a decision resumes the child run.

`RunAgent` = reserve, create child session/run/task, `startRun(child)`; a
background call returns the task, a blocking call waits for the child's end
(`waitRunEnd`, a channel registry notified by `transition`) and returns the
task's summary. A repeated call (replay after a restart) finds the task by
`(run_id, parent_tool_call_id)` and waits again or returns the result. So a
blocking parent stays `running` in its `agent` call while the child works or
waits for the user; it holds no model slot, its time there is not its own
(`Decision.Delegated`), and it keeps the `subagents:<dir>` key as it does while
the child works today. The engine writes the call's result; core never writes a
parent's transcript.

Deleted: `subagentApprovalBridgeParams`, `decodeSubagentApprovalBridge`,
`subagentApprovalRequest`, `bridgeSubagentApproval`,
`finishOrBridgeSubagentTask`, `waitForSubagentStep`, `pendingApprovalForRun`,
`mirrorPendingSubagentApproval`, `subagentBridgeApprovalForChild`,
`resumeParentForSubagentStatus`, `subagentTaskTerminal`,
`syncAsync/BlockingSubagentTaskAfterRun`, `runWaitsForBlockingChild`,
`bridgedCallWaitsForSubagent`, `backgroundSubagentApproval`,
`rejectChildApprovalCopies`, `passDecisionToSubagent`,
`recordSubagentResultMessage`, `publishSubagentToolUpdate`,
`replayInterruptedTool`.

### 4. Recovery reaches the run through the engine

`claim` of an interrupted native run computes a plan for each call of the run
without a result that is not deferred (run-scoped queries): an approval for it
still pending or denied → nothing (the engine parks or reads the denial); named
in the checkpoint's `DeferredIDs`, read-only, or an `agent` call whose task exists
(re-attach) → **rerun**; unknown tool → **answer** with the "daemon restarted"
error; other mutating calls (and an `agent` call without a task) → **ask**
(`retry_after_daemon_restart`, with a suggested rule) — unchanged policy, so a
mutating call is never re-run without a fresh approval.

```go
// agent.Task
Recovering bool              // seal the run's unfinished reply (daemon_restart)
Interrupted []InterruptedCall
type InterruptedCall struct {
	ToolCallID string
	Settle     Settle // SettleRerun | SettleAsk | SettleAnswer
	Request    tools.ApprovalRequest
	Result     tools.Result
}
```

The engine applies the plan before its first step: seals the unfinished reply,
marks rerun calls deferred (they then go through `Authorize` once nothing is
pending), requests approvals through `Approvals.Request`, and journals answers.
Then the loop parks or goes on as usual. External-agent runs keep their own
recovery in core (core is their only writer). `ExecuteTool` becomes run-less only:
`ExecuteToolInput.RunID`, its checkpoint writes and the API's "run in progress"
check go. Decided approvals stay `InputDecided`, background results `InputEvent`.

`Core.Recover(ctx)` replaces the five recovery calls: (1) shell tasks left
running → `lost`; (2) each non-terminal run once: `waiting_events` → wake check,
`waiting_approval` → resume if nothing pending, else `startRun` (claim recovers);
(3) active subagent tasks whose child run already ended → task sync (crash
between the two writes); (4) sessions with pending inputs or undelivered task
events and no active run → `afterRun`-style settle (next input or wake). The
daemon calls it once after `ApplyBootstrap`.

### 5. Cancellation by context cause

`activeRunContext` uses `context.WithCancelCause`; `CancelRun` cancels with
`agent.ErrCanceled` after the status write; release and the daemon lifetime
cancel without it. The engine's `canceled(ctx)` is
`errors.Is(context.Cause(ctx), ErrCanceled)`; the external loop checks the same.
`Inbox.Canceled`, the 750 ms row poll, `isRunCanceled` polling and
`checkExternalRunCanceled` go. Exactness: the cause is set only by `CancelRun`,
after the row says `canceled`; an interrupt (shutdown) never carries it; when
both happen the outcome path still re-reads the row (`ErrRunEnded`), as today.
`Tools.Finish` goes too: after §3 and §6 nothing implements it.

### 6. Reply-once deliveries, file versions

`HandleMessageInput.ReplyOnce` (`reply_once`) is set by the client for a target
that answers once (Telegram inline and guest); core copies it to the run's
`ClientDelivery.ReplyOnce` and to a queued `SessionInput.ReplyOnce`;
`wakeTargetOf` skips such deliveries. Core no longer reads the address.
`FileSnapshot`, `FileSnapshotStore`, `EventFileVersioned`,
`saveFileVersionSnapshot` and the table go; `tools.Result.FileVersion` stays
(the MCP server returns it; C2 owns the tool result contract).

## Contract changes (all clients checked)

- `snapshot.subagents[]`, the `agent` tool result `metadata` and the event
  payload become `Task` JSON. Renamed: `parent_session_id`→`session_id`,
  `parent_run_id`→`run_id`, `goal`→`command`, `display_name`→`description`,
  `created_at`→`started_at`, `mode: blocking|async`→`background: bool`.
  Removed: `result_message_id`. Added: `kind: "subagent"`. Unchanged: `id`,
  `agent_name`, `isolation`, `readonly`, `parent_tool_call_id`,
  `child_session_id`, `child_run_id`, `runtime`, `model`, `status`, `summary`,
  `error`, `delivered_at`, `delivered_run_id`, `updated_at`, `finished_at`.
  Old agent cards fall back to the call's `description`/`prompt`, the same values.
- Event `subagent.updated` → `task.updated` (same envelope session and run).
- Approvals: no bridge copies; a subagent's approval carries the child's
  `session_id`, `run_id`, `tool_call_id`, `tool_name`, `params`, plus new
  `task_id` and `agent_name`; it is listed and announced under the parent
  session. A parent whose blocking child asks stays `running`.
- `POST /v1/tools/execute`: `run_id` is gone. `POST /v1/messages`: new
  `reply_once`; deliveries and session inputs gain `reply_once`.
- Event `file.versioned` is gone.

Clients: `internal/clientruntime` (task event), `internal/daemonclient`
(`DecodeTask`, `reply_once` parameter), `clients/terminal` (field renames in
`conversation.go`, `app_layout_views.go`, `surface_state.go`, `agent.go`; the
permission dialog's "Subagent:" label reads `agent_name` instead of params),
`clients/telegram` (sets `ReplyOnce`; approval text names the subagent),
`clients/ios` (drops `EventType.fileVersioned`; `Approval` decodes as before).
`internal/telephony/gateway` uses none of these.

## Data migration (store, idempotent, removed in phase D)

- `approvals.task_id` + index: backfill from `tasks.child_session_id`; runs in
  `waiting_approval` that have a bridge copy become `running` (recovered at start:
  their `agent` call re-attaches); bridge copies are deleted.
- `run_wakeups` of runs not in `waiting_events` are deleted.
- `client_deliveries.reply_once`, `session_inputs.reply_once`, backfilled from
  Telegram's address kinds once, when the column is added.
- `DROP TABLE file_snapshots`. `001_init.sql` gets the new columns and loses the table.

## Size

Roughly −1500 / +700 lines in core, store and agent (non-test); tests are
ported (subagent approval tests in particular), not dropped.

## Commits (each builds, vets and passes `go test ./...`)

1. `refactor(core): cancel runs through the context cause` (§5).
2. `chore(core): stop writing file versions` (§6, table dropped).
3. `refactor(core): clients mark reply-once delivery targets` (§6).
4. `refactor(core): one run lifecycle` (§1; subagent bridging still in place,
   called from `afterRun`).
5. `refactor(core): one task type for shell and subagent tasks` (§2, clients).
6. `refactor(core): subagents ask the user directly` (§3, migration, clients).
7. `refactor(agent): the engine settles calls a restart interrupted` (§4,
   `ExecuteTool` run-less, `Tools.Finish` gone).
8. `refactor(core): one recovery entry point` (`Core.Recover`, daemon).
9. `docs: run lifecycle as built` (ARCHITECTURE.md, this note).

## Risks and tests

- **Restart recovery** (highest risk): `run_crash_recovery_integration_test.go`
  (all, incl. `TestRecoverMutatingToolRequiresFreshApprovalBeforeSingleReplay`,
  the live-verified ask-again, and the deferred/batch cases),
  `run_reschedule_test.go`, `run_recovery_test.go`; new engine tests for each
  `Settle` kind and the reply seal; new `Recover` test that starts a parent and
  child once each.
- **Approval park/resume**: the `native_run_characterization_test.go` approval
  tests, `TestRecoverDeniedToolReturnsTheDenialToTheModel`.
- **Subagents**: `subagents_approval_test.go` ported to the new model (child's
  own approval, parent `running`, rule kept for the parent, read-only refusal,
  background approval delivered to the chat), `subagents_parallel_test.go`,
  `subagents_cancel_test.go`, `agent_tool_test.go`,
  `TestBlockingSubagentReturnsChildSummaryToParent`,
  `TestRecoverBlockingSubagentCompletesChildThenParentWithoutDuplicate`,
  `TestRestartedParentWaitsForAllItsBlockingChildren`.
- **Steering**: `TestSteerDuringToolIsAppendedToThatToolResult`,
  `TestSteerIsRequeuedWhenCancelStopsItsToolResultWrite`,
  `TestMessageToABusySessionSteersByDefault`,
  `TestSteeringFromAnotherChatDeliversTheRunThere`.
- **Await/wake**: `await_test.go`, `task_wake_test.go` (incl. the guest/inline
  target test, now driven by `ReplyOnce`).
- **Cancellation**: `TestUserCancellationIsNotConvertedIntoRestartRecovery`,
  `TestCancelDuringToolStopsRunWithoutAnotherModelCall`,
  `TestACancelWhileTheEngineFinishesStands`, `TestCanceledRunRecordsFinishedAt`;
  new engine test: cause-canceled vs interrupted.
- **Lifecycle**: new table test of `runEdges` (a forbidden edge is an error)
  and of wakeup/checkpoint/FinishedAt handling per edge.
- **Migrations**: store tests on an old-shape database (bridge copy and parked
  parent, orphan wakeup, Telegram addresses, `file_snapshots`).
- `go test -race` on `core`, `agent`, `store`, `clientruntime`.
- Behaviour changes to call out: a parent stays `running` while its blocking
  child waits for approval (Telegram shows the child's approval as its own
  message, as it does for background children); a canceled blocking child's
  task is `canceled`; a failed parent cancels its blocking children.

## As built

Where the build differs from the text above:

- **`afterRun`** is run by the executor when it lets go of the run, and by
  `CancelRun` for a run that has none; `transition` does not call it, as it may
  run under a session gate the hooks take. An executor whose run was
  interrupted while the daemon lives hands it back to the run starter.
- **Edges**: an interrupted run stays `running` until it executes again; there
  is no edge back to `accepted`. `claim` treats only `running` as interrupted:
  a `waiting_approval` run with nothing pending resumes normally (its approved
  calls never started), and an `accepted` run is never recovered.
- **Recovery plan** reaches the engine in `agent.Task` (`Recovering`,
  `Interrupted []InterruptedCall` with `Settle` rerun, ask or answer), not
  through `Inbox`, as it exists only at the start of an execution. External
  agent runs keep their recovery in core (prompt and reply seal).
- **`Tools.Finish`** left the engine port with the unified task (commit 5), as
  its only user was the result-message bookkeeping.
- **Subagent approvals**: a read-only child's approval is stored rejected with
  no event or delivery. The approval delivery carries the parent's run and the
  task. `ListApprovals(session)` adds the session's subagents' approvals by
  `task_id`; `rejectRunApprovals` replaced the session-wide rejection.
- **Cancel race**: `CancelRun` of a run that ended meanwhile returns it as it
  ended, without an error. A parent canceled while it waits for its child may
  read the child's end ("Subagent canceled with its parent run.") as its call's
  result, when that came before the parent's own stop.
- **`Core.Recover`** runs once after the bootstrap applied external agents, in
  the goroutine that ran the five calls before, so shell tasks are marked lost
  a moment after the API serves (a live task is never touched).
- Left as they were: `tasks.result_message_id` (no longer written; phase D
  drops it with the canonical schema), `TaskStatusPending` (never written),
  `tools.Result.FileVersion` (C2).
- An extra commit made the approval rejection of an ended run run-scoped.
