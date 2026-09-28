# Stage 6b — Await, waiting_events and Wake-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A run can wait for its background work instead of polling: the `await(ids?, timeout_seconds)` tool parks the run in the new `waiting_events` status, and it wakes when an awaited task finishes, its timer (`run_wakeups`) runs out, or the user writes (as steer — now the default for every message to a busy session); a task that finishes while its session is idle starts a `wake` run (automation budget, delivered to the chat the user last wrote from), at most 20 in a row without a user message, after which the user only gets a notice.

**Architecture:** The engine keeps what an await asked for in `Counters.Await` (so it survives an approval park and a restart) and checks it at the start of every step (`resumeAwait`): a pending steer (journaled as a user message), a pending event for an awaited task, or a passed deadline (engine note) clears it; otherwise the step checkpoints and the run ends `waiting_events` without a model call. Core parks the run (status + `run_wakeups` row with the deadline and the awaited task IDs) and wakes it through `wakeWaitingRun`, which checks the same three conditions under the session gate and calls `startRun`; callers are task completion, new input, a one-second ticker (`WakeDueRuns`), daemon start, and the run's own post-park check (`resumeParkedRun`), which closes the race with an event that arrived while the run was parking. Idle sessions are woken by `wakeSession`, which replaces the subagent-completion runs of earlier stages.

**Tech Stack:** Go 1.26, SQLite (modernc), go-workflows run starter, bubbletea v2 TUI, Telegram Bot API.

**Prerequisite / base:** Stage 6a (`docs/superpowers/plans/2026-09-29-stage6a-background-tasks.md`) is on `main`. This plan was written and every task built and tested in a scratch copy stacked on stage 6a on top of **`d290a40`**. Locate code by the declaration names given.

---

## Ground rules for executors

- Repo `/root/projects/matrixclaw`, module `github.com/Suren878/matrixclaw`. Work directly on `main`. Another session may commit in parallel: run `git status --short` before each commit and stage only the paths the task lists with explicit `git add <paths>` / `git rm <paths>` (never `-A`, `-u` or directories).
- Code blocks give complete new files, or for existing files the **whole new version of every declaration that changes** ("replace `func X` with"), the declarations to add, the declarations to delete by name, and the new import block when it changes. `func (T) M` names a method on `T` or `*T`. Non-Go files are given as diffs.
- Run `gofmt -w` on every Go file you touch. Every commit must pass `go build ./... && go vet ./... && go test ./...`.
- Commit messages end with a blank line and exactly `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- Owner rules: delete replaced code (no shims, aliases or dead code); doc comments at most 4 lines and without history; tests only on observable behaviour; match the surrounding style; core tests call `t.Parallel()`.
- Do not start, stop or restart any `matrixclawd` daemon and do not touch `~/.matrixclaw`: a production daemon runs on this host.
- If the plan does not fit the code, fix small mismatches and report them; stop with BLOCKED / NEEDS_CONTEXT for anything bigger.
- Verified: every task below was committed in a scratch worktree (stage 6a, then this plan, on `d290a40`); after each commit `go build ./... && go vet ./... && go test ./...` passed; on the final state also `go test -race` for `internal/core`, `internal/agent`, `internal/store`, `clients/telegram`, and `deadcode -test ./...` reported nothing new. `TestTaskFinishingWhileTheRunParksStillWakesIt` was checked to fail (10 s timeout) when `resumeParkedRun` does not wake waiting runs.

## Decisions taken in this plan (owner should know)

1. **await is a core tool** (`internal/core/await.go`): `await{ids?: [string], timeout_seconds?: 0..3600}` (default 600 s), read-only, never asks, category automation (so subagents, which get no automation tools, cannot await). It needs a run. Unknown IDs or IDs of another session are an error result. Named tasks that already finished are reported at once (`Already finished: task_x failed, exit code 3.`) instead of waited for; without IDs it waits for any running background task of the session and says "nothing to wait for" when none runs. Its result is written immediately (`Waiting up to 10m0s for task_a. …`) — the await call is never left without a result — and `tools.Result.Await{task_ids, until}` tells the engine to wait.
2. **Engine state:** `Counters.Await` (`*tools.Await`, checkpointed with the counters). `resumeAwait` runs at the start of every step, after decided approvals: a pending steer ends the wait and is journaled as a **user message** (no tool result can carry it); a pending event whose task is awaited (any task when no IDs) ends it (the event is then drained as a note as usual); a passed deadline ends it with an engine note `Stopped waiting: the await timed out before task_a finished.`; otherwise the run parks (`StatusWaitingEvents`, a `model` checkpoint). Events for other tasks stay pending while it waits. A run woken for nothing parks again without a model call. Time parked is not active time (the counters' `Active` stops at the park).
3. **Core status and timers:** `RunStatusWaitingEvents = "waiting_events"` counts as active (`GetActiveRunBySession`, `ListActiveRuns`), `prepareClaimedRun` accepts it. `run_wakeups(run_id PK → runs ON DELETE CASCADE, session_id, wake_at INTEGER ms, task_ids_json)` — milliseconds, because RFC3339Nano text does not compare correctly. A waiting run without a wakeup row counts as due.
4. **Waking** (`wakeWaitingRun`, under the session gate): the run must still be `waiting_events` (a terminal run's wakeup is deleted) and due: its timer passed, a steer for it is pending, or an undelivered event matches its awaited tasks. It deletes the wakeup and calls `startRun`. Callers: `taskFinished` (6a's `finishTask` and async subagent completion), `AcceptRun` (a message to a waiting run), `WakeDueRuns` (ticker every second, started with the run starter; overdue timers fire on the first tick after a restart), `RecoverActiveRuns` (every waiting run at start), and `resumeParkedRun` after the parking run is unregistered — an event that arrived while the run was still active found it `running`, and this check starts it.
5. **User input steers by default.** `normalizeBusyInputMode("")` is steer (TUI and Telegram send no mode unless the user picks one); `queue` and `interrupt` stay explicit. A queued message for a `waiting_events` run is turned into a steer, since it should wake the run.
6. **Idle wakes** (`internal/core/task_wake.go`): when a background task finishes and its **top-level** session has no active run and no pending input, `wakeSession` starts a run with trigger `wake` (automation budget: 50 steps / 30 min) whose user message is `Background work finished while you were idle; its results follow. Carry on with what it was for, or report what happened.`; the events follow as engine notes in its first step. The run copies client, external key, capabilities and delivery address from the newest run before the wake chain, so Telegram delivers its reply. Tasks that start a run: commands that exited by themselves (`completed`/`failed`) and any subagent; stopped and lost commands only wait for the next run. Runs are created under the session gate, so two tasks finishing together start one run.
7. **Chain limit:** 20 wake runs in a row (newest runs of the session, ordered by their user message) without another run in between stop the chain; every further finished task only shows a system message in the session (`… This session has continued on its own 20 times in a row, so it waits for your message before it goes on.`) and creates a `notice` delivery for the last chat, which Telegram sends as text. Any user or scheduled run resets the chain.
8. **Subagent completion runs are wake runs now.** `deliverPendingSubagentCompletionsForParent`, `parentReadyForSubagentAutoResume`, `subagentCompletionPrompt`, `subagentCompletionTriggerID`, `markSubagentCompletionDelivered` and `ListPendingSubagentCompletionTasks` are deleted; `RecoverTaskEvents` wakes idle sessions with undelivered events at start. The follow-up run's user message changes from the subagent summary to the wake text (the summary follows as a note); two characterization tests are updated for that.
9. **Clients:** the TUI treats `waiting_events` as busy and shows "Waiting for background tasks"; Telegram shows progress for it, sends no typing action, and renders "Waiting for background tasks..."; the iOS package decodes the status as `.unknown("waiting_events")` (tolerant decoding from stage 0) — no Swift change.
10. **Prompt:** the background line asks to wait with `await` instead of polling when nothing else is left.

Out of scope: daemon-owned goals and timers across runs (sub-project 2), the unified `agent` tool and background subagent limits (6c).

## File structure

Created: `internal/store/sqlite_run_wakeups.go` (+ test), `internal/core/{run_wakeups,await,task_wake}.go` (+ `await_test.go`, `task_wake_test.go`), `internal/agent/events.go` gains `resumeAwait` (its test file too), `clients/terminal/chat/runtime/app_waiting_test.go`.

Modified: `internal/store/{migrations/001_init.sql,sqlite_messages.go,sqlite_subagents.go}`; core `types_run.go`, `ports.go`, `run_budget.go`, `run_outcome.go`, `run_execute.go`, `run_recovery.go`, `runs.go`, `session_inputs.go`, `run_continue.go`, `shell_tasks.go`, `subagents_*.go`, `deliveries.go`; engine `task.go`, `budget.go`, `engine.go`, `batch.go`; `internal/tools/types.go`; TUI `app_events.go`, `app_layout_views.go`; Telegram `delivery.go`, `delivery_queue.go`, `render_helpers.go`, `worker.go`; daemon wiring; prompt; the spec.

---



### Task 1: Run wakeups and the `waiting_events` status in the store

**Files:**
- Modify: `internal/core/ports.go`
- Modify: `internal/core/run_budget.go`
- Modify: `internal/core/types_run.go`
- Modify: `internal/store/migrations/001_init.sql`
- Modify: `internal/store/sqlite_messages.go`
- Create: `internal/store/sqlite_run_wakeups.go`
- Test (create): `internal/store/sqlite_run_wakeups_test.go`

The storage and types the rest builds on. `RunTriggerWake` gets the automation budget here. No code produces `waiting_events` yet.

- [ ] **Step 1: Write the failing tests**

Create `internal/store/sqlite_run_wakeups_test.go`:

```go
package store_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestRunWakeupsComeDueInOrder(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	for _, id := range []string{"r1", "r2", "r3"} {
		createTestRun(t, st, "s1", id)
	}
	for _, wakeup := range []core.RunWakeup{
		{RunID: "r1", SessionID: "s1", WakeAt: testEpoch.Add(2 * time.Minute), TaskIDs: []string{"task_a", "task_b"}},
		{RunID: "r2", SessionID: "s1", WakeAt: testEpoch.Add(time.Minute)},
		{RunID: "r3", SessionID: "s1", WakeAt: testEpoch.Add(time.Hour)},
		{RunID: "r1", SessionID: "s1", WakeAt: testEpoch.Add(90 * time.Second), TaskIDs: []string{"task_a"}},
	} {
		if err := st.SaveRunWakeup(ctx, wakeup); err != nil {
			t.Fatal(err)
		}
	}

	due, err := st.ListDueRunWakeups(ctx, testEpoch.Add(2*time.Minute))
	if err != nil || len(due) != 2 || due[0].RunID != "r2" || due[1].RunID != "r1" || !reflect.DeepEqual(due[1].TaskIDs, []string{"task_a"}) || !due[1].WakeAt.Equal(testEpoch.Add(90*time.Second)) {
		t.Fatalf("due = %+v, %v", due, err)
	}
	if err := st.DeleteRunWakeup(ctx, "r2"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRunWakeup(ctx, "r2"); err != core.ErrNotFound {
		t.Fatalf("deleted wakeup err = %v", err)
	}
	if got, err := st.GetRunWakeup(ctx, "r3"); err != nil || got.TaskIDs != nil || got.SessionID != "s1" {
		t.Fatalf("r3 = %+v, %v", got, err)
	}
}

func TestWaitingRunsAreActiveAndSessionRunsListNewestFirst(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	for i, id := range []string{"r1", "r2", "r3"} {
		at := testEpoch.Add(time.Duration(i) * time.Minute)
		message := transcript.Message{ID: "m_" + id, SessionID: "s1", RunID: id, Role: transcript.MessageRoleUser, Content: id, CreatedAt: at, UpdatedAt: at}
		run := core.Run{ID: id, SessionID: "s1", UserMessageID: message.ID, Status: core.RunStatusCompleted, Trigger: core.RunTriggerWake, StartedAt: at, UpdatedAt: at}
		if id == "r3" {
			run.Status = core.RunStatusWaitingEvents
		}
		if err := st.AcceptMessage(ctx, message, run); err != nil {
			t.Fatal(err)
		}
	}

	active, err := st.GetActiveRunBySession(ctx, "s1")
	if err != nil || active.ID != "r3" {
		t.Fatalf("active = %+v, %v", active, err)
	}
	all, err := st.ListActiveRuns(ctx)
	if err != nil || len(all) != 1 || all[0].ID != "r3" {
		t.Fatalf("active runs = %+v, %v", all, err)
	}
	runs, err := st.ListSessionRuns(ctx, "s1", 2)
	if err != nil || len(runs) != 2 || runs[0].ID != "r3" || runs[1].ID != "r2" || runs[1].Trigger != core.RunTriggerWake {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/store -run '^(TestRunWakeupsComeDueInOrder|TestWaitingRunsAreActiveAndSessionRunsListNewestFirst)$'
```

Expected: build fails (`SaveRunWakeup`, `RunStatusWaitingEvents`, `ListSessionRuns` undefined).

- [ ] **Step 3: Implement**

In `internal/core/ports.go`, replace `type RunStore` with:

```go
type RunStore interface {
	CreateRun(ctx context.Context, run Run) error
	GetRun(ctx context.Context, runID string) (Run, error)
	GetActiveRunBySession(ctx context.Context, sessionID string) (Run, error)
	GetLatestRunBySession(ctx context.Context, sessionID string) (Run, error)
	// ListSessionRuns lists the session's newest runs first, ordered by their user message.
	ListSessionRuns(ctx context.Context, sessionID string, limit int) ([]Run, error)
	ListActiveRuns(ctx context.Context) ([]Run, error)
	UpdateRun(ctx context.Context, run Run) error
	CompleteRun(ctx context.Context, assistantMessage transcript.Message, run Run) error

	AcceptMessage(ctx context.Context, message transcript.Message, run Run, deliveries ...ClientDelivery) error
}
```

In `internal/core/ports.go`, replace `type Store` with:

```go
type Store interface {
	SessionStore
	SubagentTaskStore
	TaskStore
	BindingStore
	DeliveryStore
	MessageStore
	RunStore
	RunWakeupStore
	SessionInputStore
	UsageStore
	SessionBudgetStore
	EngineStateStore
	TodoStore
	SearchStore
	MemoryStore
	ApprovalStore
	PermissionRuleStore
	FileSnapshotStore
}
```

In `internal/core/ports.go`, add:

```go
// RunWakeupStore keeps the timers of runs parked in waiting_events.
type RunWakeupStore interface {
	SaveRunWakeup(ctx context.Context, wakeup RunWakeup) error
	GetRunWakeup(ctx context.Context, runID string) (RunWakeup, error)
	DeleteRunWakeup(ctx context.Context, runID string) error
	ListDueRunWakeups(ctx context.Context, at time.Time) ([]RunWakeup, error)
}
```

In `internal/core/run_budget.go`, replace `func (Core) defaultRunBudget` with:

```go
// defaultRunBudget is the daemon default for what started the run.
func (c *Core) defaultRunBudget(run Run, session Session) agent.Budget {
	switch {
	case isSubagentSession(session):
		return c.budgets.Subagent
	case run.Trigger == RunTriggerAutomation || run.Trigger == RunTriggerWake:
		return c.budgets.Automation
	default:
		return c.budgets.User
	}
}
```

In `internal/core/types_run.go`, replace `const block starting with RunStatusAccepted` with:

```go
const (
	RunStatusAccepted        RunStatus = "accepted"
	RunStatusRunning         RunStatus = "running"
	RunStatusWaitingApproval RunStatus = "waiting_approval"
	// RunStatusWaitingEvents is a run parked by await until a task it waits for
	// finishes, the user writes or its timer runs out.
	RunStatusWaitingEvents RunStatus = "waiting_events"
	RunStatusCompleted     RunStatus = "completed"
	RunStatusCanceled      RunStatus = "canceled"
	RunStatusFailed        RunStatus = "failed"
)
```

In `internal/core/types_run.go`, replace `const block starting with RunTriggerAutomation` with:

```go
const (
	// RunTriggerAutomation covers scheduled jobs.
	RunTriggerAutomation RunTrigger = "automation"
	// RunTriggerWake is a run an idle session started for background work that finished.
	RunTriggerWake RunTrigger = "wake"
)
```

In `internal/core/types_run.go`, add:

```go
// RunWakeup is when a run parked in waiting_events wakes at the latest, and the
// tasks it waits for; none means any background task of its session.
type RunWakeup struct {
	RunID     string
	SessionID string
	WakeAt    time.Time
	TaskIDs   []string
}
```

In `internal/store/migrations/001_init.sql`:

```diff
diff --git a/internal/store/migrations/001_init.sql b/internal/store/migrations/001_init.sql
index ae31d3a..f85e699 100644
--- a/internal/store/migrations/001_init.sql
+++ b/internal/store/migrations/001_init.sql
@@ -108,6 +108,14 @@ CREATE TABLE IF NOT EXISTS run_checkpoints (
     FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
 );
 
+CREATE TABLE IF NOT EXISTS run_wakeups (
+    run_id TEXT PRIMARY KEY,
+    session_id TEXT NOT NULL,
+    wake_at INTEGER NOT NULL,
+    task_ids_json TEXT NOT NULL DEFAULT '',
+    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
+);
+
 CREATE TABLE IF NOT EXISTS session_inputs (
     id TEXT PRIMARY KEY,
     session_id TEXT NOT NULL,
@@ -254,6 +262,9 @@ CREATE INDEX IF NOT EXISTS idx_tasks_parent_call
 CREATE INDEX IF NOT EXISTS idx_tasks_child_run
     ON tasks(child_run_id);
 
+CREATE INDEX IF NOT EXISTS idx_run_wakeups_wake_at
+    ON run_wakeups(wake_at);
+
 CREATE INDEX IF NOT EXISTS idx_runs_session_started_at
     ON runs(session_id, started_at);
 
```

In `internal/store/sqlite_messages.go`, replace `func (SQLiteStore) GetActiveRunBySession` with:

```go
func (s *SQLiteStore) GetActiveRunBySession(ctx context.Context, sessionID string) (core.Run, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+runColumns+`
FROM runs
WHERE session_id = ?
  AND status IN (?, ?, ?, ?)
ORDER BY started_at DESC, updated_at DESC
LIMIT 1`,
		strings.TrimSpace(sessionID),
		string(core.RunStatusAccepted),
		string(core.RunStatusRunning),
		string(core.RunStatusWaitingApproval),
		string(core.RunStatusWaitingEvents),
	)

	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Run{}, core.ErrNotFound
		}
		return core.Run{}, fmt.Errorf("store: get active run by session: %w", err)
	}
	return run, nil
}
```

In `internal/store/sqlite_messages.go`, replace `func (SQLiteStore) ListActiveRuns` with:

```go
func (s *SQLiteStore) ListActiveRuns(ctx context.Context) ([]core.Run, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+runColumns+`
FROM runs
WHERE status IN (?, ?, ?, ?)
ORDER BY started_at ASC, updated_at ASC`,
		string(core.RunStatusAccepted),
		string(core.RunStatusRunning),
		string(core.RunStatusWaitingApproval),
		string(core.RunStatusWaitingEvents),
	)
	if err != nil {
		return nil, fmt.Errorf("store: list active runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	runs := []core.Run{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan active run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate active runs: %w", err)
	}
	return runs, nil
}
```

In `internal/store/sqlite_messages.go`, add:

```go
func (s *SQLiteStore) ListSessionRuns(ctx context.Context, sessionID string, limit int) ([]core.Run, error) {
	query := `
SELECT ` + runColumns + `
FROM runs
WHERE session_id = ?
ORDER BY (SELECT seq FROM messages WHERE messages.id = runs.user_message_id) DESC`
	args := []any{strings.TrimSpace(sessionID)}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list session runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var runs []core.Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan session run: %w", err)
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}
```

Create `internal/store/sqlite_run_wakeups.go`:

```go
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

// Wake times are kept in Unix milliseconds so they compare as numbers.

func (s *SQLiteStore) SaveRunWakeup(ctx context.Context, wakeup core.RunWakeup) error {
	taskIDs := ""
	if len(wakeup.TaskIDs) > 0 {
		encoded, err := json.Marshal(wakeup.TaskIDs)
		if err != nil {
			return err
		}
		taskIDs = string(encoded)
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO run_wakeups(run_id, session_id, wake_at, task_ids_json) VALUES(?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET session_id = excluded.session_id, wake_at = excluded.wake_at, task_ids_json = excluded.task_ids_json`,
		strings.TrimSpace(wakeup.RunID), strings.TrimSpace(wakeup.SessionID), wakeup.WakeAt.UnixMilli(), taskIDs); err != nil {
		return fmt.Errorf("store: save run wakeup: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetRunWakeup(ctx context.Context, runID string) (core.RunWakeup, error) {
	wakeup, err := scanRunWakeup(s.db.QueryRowContext(ctx, `SELECT run_id, session_id, wake_at, task_ids_json FROM run_wakeups WHERE run_id = ?`, strings.TrimSpace(runID)))
	if errors.Is(err, sql.ErrNoRows) {
		return core.RunWakeup{}, core.ErrNotFound
	}
	return wakeup, err
}

func (s *SQLiteStore) DeleteRunWakeup(ctx context.Context, runID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM run_wakeups WHERE run_id = ?`, strings.TrimSpace(runID)); err != nil {
		return fmt.Errorf("store: delete run wakeup: %w", err)
	}
	return nil
}

// ListDueRunWakeups lists the wakeups due at at, earliest first.
func (s *SQLiteStore) ListDueRunWakeups(ctx context.Context, at time.Time) ([]core.RunWakeup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_id, session_id, wake_at, task_ids_json FROM run_wakeups WHERE wake_at <= ? ORDER BY wake_at ASC`, at.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("store: list due run wakeups: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var wakeups []core.RunWakeup
	for rows.Next() {
		wakeup, err := scanRunWakeup(rows)
		if err != nil {
			return nil, err
		}
		wakeups = append(wakeups, wakeup)
	}
	return wakeups, rows.Err()
}

type runWakeupScanner interface {
	Scan(dest ...any) error
}

func scanRunWakeup(scanner runWakeupScanner) (core.RunWakeup, error) {
	var wakeup core.RunWakeup
	var wakeAt int64
	var taskIDs string
	if err := scanner.Scan(&wakeup.RunID, &wakeup.SessionID, &wakeAt, &taskIDs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.RunWakeup{}, err
		}
		return core.RunWakeup{}, fmt.Errorf("store: scan run wakeup: %w", err)
	}
	wakeup.WakeAt = time.UnixMilli(wakeAt).UTC()
	if taskIDs != "" {
		if err := json.Unmarshal([]byte(taskIDs), &wakeup.TaskIDs); err != nil {
			return core.RunWakeup{}, fmt.Errorf("store: decode run wakeup tasks: %w", err)
		}
	}
	return wakeup, nil
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/store -run '^(TestRunWakeupsComeDueInOrder|TestWaitingRunsAreActiveAndSessionRunsListNewestFirst)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/core/ports.go \
  internal/core/run_budget.go \
  internal/core/types_run.go \
  internal/store/migrations/001_init.sql \
  internal/store/sqlite_messages.go \
  internal/store/sqlite_run_wakeups.go \
  internal/store/sqlite_run_wakeups_test.go
git commit -m "feat(store): run wakeups and the waiting_events run status

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 2: The engine parks a run that awaits

**Files:**
- Modify: `internal/agent/batch.go`
- Modify: `internal/agent/budget.go`
- Modify: `internal/agent/engine.go`
- Modify: `internal/agent/events.go`
- Modify: `internal/agent/task.go`
- Modify: `internal/tools/types.go`
- Test (modify): `internal/agent/events_test.go`

The engine side of await. `record` copies a call's `Result.Await` into the counters; the next step's `resumeAwait` decides whether the run goes on or parks. `settle` checkpoints a park like an approval park; an interrupted run reports `Reached: StatusWaitingEvents`.

- [ ] **Step 1: Write the failing tests**

In `internal/agent/events_test.go`, the imports become:

```go
import (
	"context"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)
```

In `internal/agent/events_test.go`, add:

```go
// awaitTool asks the engine to wait up to ten minutes for taskIDs; each call
// takes a minute of the fixture's clock.
func awaitTool(f *agenttest.Fixture, taskIDs ...string) agenttest.ToolFunc {
	return func(tools.Call) tools.Result {
		f.Clock = f.Clock.Add(time.Minute)
		return tools.Result{Content: "Waiting.", Await: &tools.Await{TaskIDs: taskIDs, Until: f.Clock.Add(10 * time.Minute)}}
	}
}
```

In `internal/agent/events_test.go`, add:

```go
func resume(t *testing.T, f *agenttest.Fixture, model agent.Model, counters agent.Counters) agent.Outcome {
	t.Helper()
	task := f.Task(model)
	task.Resume = counters
	outcome, err := f.Engine().Run(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}
```

In `internal/agent/events_test.go`, add:

```go
func TestAwaitParksTheRunUntilAnAwaitedTaskFinishes(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["await"] = awaitTool(f, "task_a")
	model := agenttest.NewScriptedModel(calls(call("w1", "await")), text("Tests passed."))

	parked := run(t, f, model)
	if parked.Status != agent.StatusWaitingEvents || parked.Counters.Await == nil || parked.Counters.Await.TaskIDs[0] != "task_a" {
		t.Fatalf("parked = %+v", parked)
	}
	if last := f.Journal.States[len(f.Journal.States)-1]; last.Counters.Await == nil {
		t.Fatalf("the park checkpoint lacks the await: %+v", last)
	}

	f.Inbox.Events = []agent.Input{{Kind: agent.InputEvent, ID: "task_b", Text: "Background task task_b finished with exit code 0."}}
	still := resume(t, f, model, parked.Counters)
	if still.Status != agent.StatusWaitingEvents || len(model.Requests()) != 1 || len(f.Inbox.Events) != 1 {
		t.Fatalf("an unawaited task woke the run: %+v, requests %d", still, len(model.Requests()))
	}

	f.Inbox.Events = append(f.Inbox.Events, agent.Input{Kind: agent.InputEvent, ID: "task_a", Text: "Background task task_a finished with exit code 0."})
	done := resume(t, f, model, still.Counters)
	if done.Status != agent.StatusCompleted || done.Assistant.Content != "Tests passed." || done.Counters.Await != nil || len(f.Inbox.Events) != 0 {
		t.Fatalf("done = %+v, events left %+v", done, f.Inbox.Events)
	}
	last := model.Requests()[1].Messages
	if got := last[len(last)-1].Content; got != "Background task task_a finished with exit code 0." {
		t.Fatalf("the woken request ends with %q", got)
	}
}
```

In `internal/agent/events_test.go`, add:

```go
func TestUserInputWakesAnAwaitingRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["await"] = awaitTool(f)
	model := agenttest.NewScriptedModel(calls(call("w1", "await")), text("Stopped the build."))
	parked := run(t, f, model)

	f.Inbox.Steers = []string{"stop waiting and cancel the build"}
	done := resume(t, f, model, parked.Counters)

	if done.Status != agent.StatusCompleted || len(f.Inbox.Steers) != 0 {
		t.Fatalf("done = %+v, steers left %v", done, f.Inbox.Steers)
	}
	var user []string
	for _, message := range f.Journal.Messages {
		if message.Role == transcript.MessageRoleUser {
			user = append(user, message.Content)
		}
	}
	if len(user) != 2 || user[1] != "stop waiting and cancel the build" {
		t.Fatalf("user messages = %q", user)
	}
}
```

In `internal/agent/events_test.go`, add:

```go
func TestAwaitTimesOutAndTellsTheModel(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["await"] = awaitTool(f, "task_a")
	model := agenttest.NewScriptedModel(calls(call("w1", "await")), text("Still building; I will check later."))
	parked := run(t, f, model)

	f.Clock = parked.Counters.Await.Until
	done := resume(t, f, model, parked.Counters)

	if done.Status != agent.StatusCompleted {
		t.Fatalf("done = %+v", done)
	}
	last := model.Requests()[1].Messages
	if got := last[len(last)-1].Content; got != "Stopped waiting: the await timed out before task_a finished." {
		t.Fatalf("the woken request ends with %q", got)
	}
}
```

In `internal/agent/events_test.go`, add:

```go
func TestTimeParkedInAwaitIsNotActiveTime(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["await"] = awaitTool(f, "task_a")
	model := agenttest.NewScriptedModel(calls(call("w1", "await")), text("Done."))
	parked := run(t, f, model)
	if parked.Counters.Active != time.Minute {
		t.Fatalf("active before parking = %v", parked.Counters.Active)
	}

	f.Clock = f.Clock.Add(5 * time.Minute)
	f.Inbox.Events = []agent.Input{{Kind: agent.InputEvent, ID: "task_a", Text: "Background task task_a finished with exit code 0."}}
	done := resume(t, f, model, parked.Counters)

	if done.Counters.Active != time.Minute {
		t.Fatalf("active after five parked minutes = %v", done.Counters.Active)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/agent -run '^(TestAwaitParksTheRunUntilAnAwaitedTaskFinishes|TestUserInputWakesAnAwaitingRun|TestAwaitTimesOutAndTellsTheModel|TestTimeParkedInAwaitIsNotActiveTime)$'
```

Expected: build fails (`tools.Await`, `agent.StatusWaitingEvents` undefined).

- [ ] **Step 3: Implement**

In `internal/agent/batch.go`, the imports become:

```go
import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/tools"
)
```

In `internal/agent/batch.go`, replace `func (batch) record` with:

```go
// record stores a finished call's outcome and reports whether it asks for
// approval; a tool error is returned, as it fails the run.
func (b *batch) record(done toolsched.Done[callOutcome]) (bool, error) {
	c := b.calls[done.Index]
	switch {
	case errors.Is(done.Err, toolsched.ErrPanicked):
		c.state, c.result = callFinished, panicResult
	case done.Err != nil:
		c.state = callStopped
		return false, done.Err
	case done.Value.err != nil:
		c.state = callStopped
		return false, done.Value.err
	case done.Value.result.Approval != nil && !c.req.approved:
		c.state = callAsked
		return true, nil
	default:
		c.state, c.result = callFinished, done.Value.result
		if await := c.result.Await; await != nil {
			b.r.counters.Await = &tools.Await{TaskIDs: slices.Clone(await.TaskIDs), Until: await.Until}
		}
	}
	return false, nil
}
```

In `internal/agent/budget.go`, the imports become:

```go
import (
	"context"
	"fmt"
	"time"

	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)
```

In `internal/agent/budget.go`, replace `type Counters` with:

```go
// steps: no-progress streak, output limit, low-yield summaries, elision, last
// history edit, learned prompt room, whether open todo items were pointed out
// and what the run awaits. It is checkpointed so a parked or restarted run
// continues from it.
type Counters struct {
	Steps         int           `json:"steps,omitempty"`
	Tokens        int64         `json:"tokens,omitempty"`
	Active        time.Duration `json:"active,omitempty"`
	WrapUpSent    bool          `json:"wrap_up_sent,omitempty"`
	LoopHash      string        `json:"loop_hash,omitempty"`
	LoopTool      string        `json:"loop_tool,omitempty"`
	LoopRepeats   int           `json:"loop_repeats,omitempty"`
	LoopWarned    bool          `json:"loop_warned,omitempty"`
	Continuations int           `json:"continuations,omitempty"`
	OutputLimit   int           `json:"output_limit,omitempty"`
	LowYield      int           `json:"low_yield,omitempty"`
	ElidedResults int64         `json:"elided_results,omitempty"`
	ElidedImages  int64         `json:"elided_images,omitempty"`
	HistoryEdit   int64         `json:"history_edit,omitempty"`
	LearnedLimit  int           `json:"learned_limit,omitempty"`
	TodoNudged    bool          `json:"todo_nudged,omitempty"`
	// Await is set while the run waits for what an await call asked for.
	Await *tools.Await `json:"await,omitempty"`
}
```

In `internal/agent/engine.go`, replace `func (run) step` with:

```go
func (r *run) step(ctx context.Context) stepResult {
	waiting, err := r.resumeDecided(ctx)
	if err != nil {
		return failedStep(err)
	}
	if waiting {
		return stepResult{kind: stepWaitingApproval}
	}
	if waiting, err = r.resumeAwait(ctx); err != nil {
		return failedStep(err)
	}
	if waiting {
		return stepResult{kind: stepWaitingEvents}
	}
	if err := r.drainEvents(ctx); err != nil {
		return failedStep(err)
	}
	if err := r.syncContext(ctx); err != nil {
		return failedStep(err)
	}
	final, err := r.prepareStep(ctx)
	if err != nil {
		return failedStep(err)
	}
	request, final, err := r.fitRequest(ctx, final)
	if err != nil {
		return failedStep(err)
	}
	if err := r.checkpoint(ctx, PhaseModel, nil); err != nil {
		return failedStep(err)
	}
	r.counters.Steps++
	gen, err := r.generateWithRetry(ctx, request)
	if err != nil && agentcontext.IsContextLengthExceeded(err) {
		tokens := r.promptTokens(request)
		r.learnLimit(tokens)
		compacted, compactErr := r.compactHistory(ctx, nil, tokens, agentcontext.TailPercent/2)
		if compactErr != nil {
			return failedStep(compactErr)
		}
		if !compacted {
			err = fmt.Errorf("%w: %w", ErrContextExhausted, err)
		} else {
			retry, buildErr := r.afterSummary(ctx, final)
			if buildErr != nil {
				return failedStep(buildErr)
			}
			if gen, err = r.generateWithRetry(ctx, retry); err != nil && agentcontext.IsContextLengthExceeded(err) {
				err = fmt.Errorf("%w: %w", ErrContextExhausted, err)
			}
		}
	}
	if err == nil {
		r.anchorUsage(gen.response)
	}
	if final != "" && errors.Is(err, providers.ErrEmptyResponse) {
		return finalTurn(gen, final)
	}
	if err != nil {
		result := stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, response: gen.response, err: err, markErrored: true}
		if errors.Is(err, ErrContextExhausted) {
			result.stop = StopContextExhausted
		}
		return result
	}
	if r.canceled(ctx) {
		return stepResult{kind: stepDone, canceled: true, assistant: &gen.assistant, saved: gen.saved}
	}
	if final != "" {
		return finalTurn(gen, final)
	}
	return r.handleResponse(ctx, gen)
}
```

In `internal/agent/engine.go`, replace `func (run) settle` with:

```go
func (r *run) settle(ctx context.Context, result stepResult) (Outcome, bool, error) {
	if result.assistant != nil && r.canceled(ctx) {
		return Outcome{Status: StatusCanceled, Assistant: result.assistant, AssistantSaved: result.saved}, true, nil
	}
	if result.err != nil {
		outcome := Outcome{Status: StatusFailed, Err: result.err, StopReason: result.stop}
		if result.markErrored && result.assistant != nil {
			outcome.Assistant, outcome.AssistantSaved, outcome.MarkErrored = result.assistant, result.saved, true
		}
		return outcome, true, nil
	}
	switch result.kind {
	case stepWaitingApproval:
		pending, err := r.Approvals.Pending(ctx, r.task.RunID)
		if err != nil {
			return Outcome{}, true, err
		}
		if !pending {
			return Outcome{}, false, nil
		}
		if err := r.checkpoint(ctx, PhaseModel, nil); err != nil {
			return Outcome{}, true, err
		}
		return Outcome{Status: StatusWaitingApproval}, true, nil
	case stepWaitingEvents:
		if err := r.checkpoint(ctx, PhaseModel, nil); err != nil {
			return Outcome{}, true, err
		}
		return Outcome{Status: StatusWaitingEvents}, true, nil
	case stepDone:
		reply := finalReply(*result.assistant, result.response)
		return Outcome{Status: StatusCompleted, StopReason: result.stopReason(), Assistant: &reply, AssistantSaved: result.saved}, true, nil
	default:
		return Outcome{}, false, nil
	}
}
```

In `internal/agent/engine.go`, replace `func (run) interrupted` with:

```go
func (r *run) interrupted(result stepResult) Outcome {
	outcome := Outcome{Status: StatusInterrupted, Assistant: result.assistant, AssistantSaved: result.saved}
	if result.err != nil {
		return outcome
	}
	switch result.kind {
	case stepDone:
		reply := finalReply(*result.assistant, result.response)
		outcome.Assistant, outcome.Reached, outcome.StopReason = &reply, StatusCompleted, result.stopReason()
	case stepWaitingApproval:
		outcome.Reached = StatusWaitingApproval
	case stepWaitingEvents:
		outcome.Reached = StatusWaitingEvents
	}
	return outcome
}
```

In `internal/agent/engine.go`, replace `const block starting with stepContinue` with:

```go
const (
	stepContinue stepKind = iota
	stepWaitingApproval
	stepWaitingEvents
	stepDone
)
```

In `internal/agent/events.go`, the imports become:

```go
import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)
```

In `internal/agent/events.go`, add:

```go
// resumeAwait keeps a run that called await parked until what it waits for
// happened: an awaited task finished, the user wrote, which is journaled as a
// user message, or the wait timed out, which the model is told. It reports
// whether the run still waits.
func (r *run) resumeAwait(ctx context.Context) (bool, error) {
	await := r.counters.Await
	if await == nil {
		return false, nil
	}
	steers, err := r.Inbox.Peek(ctx, r.task.RunID, InputSteer)
	if err != nil {
		return false, err
	}
	events, err := r.Inbox.Peek(ctx, r.task.RunID, InputEvent)
	if err != nil {
		return false, err
	}
	finished := slices.ContainsFunc(events, func(event Input) bool {
		return len(await.TaskIDs) == 0 || slices.Contains(await.TaskIDs, event.ID)
	})
	switch {
	case len(steers) > 0:
		r.counters.Await = nil
		return false, r.appendSteers(ctx, steers)
	case finished:
		r.counters.Await = nil
		return false, nil
	case r.Now().Before(await.Until):
		return true, nil
	default:
		r.counters.Await = nil
		return false, r.appendEngineMessage(ctx, transcript.OriginEngine, awaitTimeoutText(await.TaskIDs))
	}
}
```

In `internal/agent/events.go`, add:

```go
// appendSteers journals steer input that no tool result can carry as one user
// message and consumes it.
func (r *run) appendSteers(ctx context.Context, steers []Input) error {
	texts := make([]string, 0, len(steers))
	ids := make([]string, 0, len(steers))
	for _, steer := range steers {
		texts = append(texts, steer.Text)
		ids = append(ids, steer.ID)
	}
	text := strings.Join(texts, "\n\n")
	now := r.Now()
	message := transcript.Message{ID: r.NewID("msg"), SessionID: r.task.SessionID, RunID: r.task.RunID, Role: transcript.MessageRoleUser, Content: text, Parts: transcript.NormalizeMessageParts(text, nil), CreatedAt: now, UpdatedAt: now}
	if err := r.history.append(ctx, message); err != nil {
		return err
	}
	return r.Inbox.Consume(context.WithoutCancel(ctx), r.task.RunID, ids)
}
```

In `internal/agent/events.go`, add:

```go
func awaitTimeoutText(taskIDs []string) string {
	if len(taskIDs) == 0 {
		return "Stopped waiting: the await timed out before any background task finished."
	}
	return fmt.Sprintf("Stopped waiting: the await timed out before %s finished.", strings.Join(taskIDs, ", "))
}
```

In `internal/agent/task.go`, replace `const block starting with StatusCompleted` with:

```go
const (
	StatusCompleted       Status = "completed"
	StatusWaitingApproval Status = "waiting_approval"
	StatusWaitingEvents   Status = "waiting_events"
	StatusInterrupted     Status = "interrupted"
	StatusCanceled        Status = "canceled"
	StatusFailed          Status = "failed"
)
```

In `internal/tools/types.go`, the imports become:

```go
import (
	"context"
	"encoding/json"
	"time"

	"github.com/Suren878/matrixclaw/internal/permission"
)
```

In `internal/tools/types.go`, replace `type Result` with:

```go
type Result struct {
	Content     string           `json:"content"`
	Metadata    any              `json:"metadata,omitempty"`
	MIMEType    string           `json:"mime_type,omitempty"`
	Status      ResultStatus     `json:"status,omitempty"`
	IsError     bool             `json:"is_error,omitempty"`
	Approval    *ApprovalRequest `json:"approval,omitempty"`
	FileVersion *FileVersion     `json:"file_version,omitempty"`
	// OutputPath is the file holding the full output when Content was cut.
	OutputPath string `json:"output_path,omitempty"`
	// Await parks the run once its batch is done, until one of the tasks
	// finishes, the user writes or Until passes.
	Await *Await `json:"await,omitempty"`
}
```

In `internal/tools/types.go`, add:

```go
// Await is what a run waits for: any of TaskIDs, or any background task of its
// session when there are none, until Until.
type Await struct {
	TaskIDs []string  `json:"task_ids,omitempty"`
	Until   time.Time `json:"until"`
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/agent -run '^(TestAwaitParksTheRunUntilAnAwaitedTaskFinishes|TestUserInputWakesAnAwaitingRun|TestAwaitTimesOutAndTellsTheModel|TestTimeParkedInAwaitIsNotActiveTime)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/agent/batch.go \
  internal/agent/budget.go \
  internal/agent/engine.go \
  internal/agent/events.go \
  internal/agent/events_test.go \
  internal/agent/task.go \
  internal/tools/types.go
git commit -m "feat(agent): await parks a run in waiting_events until its tasks, the user or a timeout

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 3: Core: await, park and wake

**Files:**
- Create: `internal/core/await.go`
- Modify: `internal/core/run_execute.go`
- Modify: `internal/core/run_outcome.go`
- Modify: `internal/core/run_recovery.go`
- Create: `internal/core/run_wakeups.go`
- Modify: `internal/core/runs.go`
- Modify: `internal/core/session_inputs.go`
- Modify: `internal/core/shell_tasks.go`
- Modify: `internal/core/subagents_lifecycle.go`
- Modify: `internal/daemoncmd/run.go`
- Test (create): `internal/core/await_test.go`
- Test (modify): `internal/providers/ai/gemini/schema_test.go`

Core parks and wakes. `applyOutcome` stores the wakeup and the status (also for an interrupted run that had reached the park); `resumeParkedRun` checks waiting runs after the run is unregistered. The await tool and the ticker are registered in the daemon. `finishTask` and async subagent completion call `taskFinished`, which wakes a waiting run of the session (the idle case follows in Task 5).

- [ ] **Step 1: Write the failing tests**

Create `internal/core/await_test.go`:

```go
package core_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// awaitScenario is a native run whose model awaits and then replies "Done."; it
// records every request.
type awaitScenario struct {
	app      *core.Core
	db       *store.SQLiteStore
	session  core.Session
	run      core.Run
	starter  *executingRunStarter
	mu       sync.Mutex
	requests []providers.Request
}

func newAwaitScenario(t *testing.T, st core.Store, db *store.SQLiteStore, awaitArgs func(taskID string) string) (*awaitScenario, string) {
	t.Helper()
	app := core.New(st)
	s := &awaitScenario{app: app, db: db, starter: &executingRunStarter{app: app}}
	app.WithSessionFiles(t.TempDir()).WithRunStarter(s.starter)
	app.WithTools(tools.NewRegistry(core.AwaitToolExecutors(app)...))
	s.session, s.run = saveCrashRecoveryRun(t, db, "await", core.RunStatusAccepted, false)
	started, err := app.RunCommand(context.Background(), tools.Call{SessionID: s.session.ID}, tools.Command{Command: "sleep 30", Background: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = app.CancelTask(context.Background(), started.TaskID); s.starter.wait(t) })
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests = append(s.requests, request)
		if len(s.requests) == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call_await", Name: "await", Arguments: json.RawMessage(awaitArgs(started.TaskID))}}}, nil
		}
		return providers.Response{Text: "Done."}, nil
	})})
	return s, started.TaskID
}

// woken is what the woken request sends after the await call's result.
func (s *awaitScenario) woken(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) != 2 {
		t.Fatalf("requests = %d, want the first and the woken one", len(s.requests))
	}
	var after []string
	seen := false
	for _, message := range s.requests[1].Messages {
		if seen {
			after = append(after, message.Content)
		}
		seen = seen || message.ToolCallID == "call_await"
	}
	return strings.Join(after, "\n")
}

func waitRunStatus(t *testing.T, db *store.SQLiteStore, runID string, want core.RunStatus) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		run, err := db.GetRun(context.Background(), runID)
		if err == nil && run.Status == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s = %+v, %v; want %s", runID, run.Status, err, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func openScenarioStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestAwaitedTaskFinishingWakesTheRun(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	s, taskID := newAwaitScenario(t, db, db, func(id string) string { return `{"ids":["` + id + `"]}` })

	if err := s.app.ExecuteRun(context.Background(), s.run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, s.run.ID, core.RunStatusWaitingEvents)
	if wakeup, err := db.GetRunWakeup(context.Background(), s.run.ID); err != nil || len(wakeup.TaskIDs) != 1 || wakeup.TaskIDs[0] != taskID {
		t.Fatalf("wakeup = %+v, %v", wakeup, err)
	}

	if _, err := s.app.CancelTask(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, s.run.ID, core.RunStatusCompleted)
	if got := s.woken(t); !strings.Contains(got, "Background task "+taskID+" was stopped") {
		t.Fatalf("the woken request sends %q", got)
	}
	if _, err := db.GetRunWakeup(context.Background(), s.run.ID); err != core.ErrNotFound {
		t.Fatalf("wakeup after waking: %v", err)
	}
}

func TestAwaitTimerWakesTheRun(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	s, _ := newAwaitScenario(t, db, db, func(string) string { return `{"timeout_seconds":60}` })
	now := time.Now().UTC()
	var clock sync.Mutex
	s.app.WithClock(func() time.Time { clock.Lock(); defer clock.Unlock(); return now })

	if err := s.app.ExecuteRun(context.Background(), s.run.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.app.WakeDueRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, s.run.ID, core.RunStatusWaitingEvents)

	clock.Lock()
	now = now.Add(time.Minute)
	clock.Unlock()
	if err := s.app.WakeDueRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, s.run.ID, core.RunStatusCompleted)
	if got := s.woken(t); !strings.Contains(got, "Stopped waiting: the await timed out before any background task finished.") {
		t.Fatalf("the woken request sends %q", got)
	}
}

func TestUserMessageWakesAnAwaitingRun(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	s, _ := newAwaitScenario(t, db, db, func(string) string { return `{}` })
	if err := s.app.ExecuteRun(context.Background(), s.run.ID); err != nil {
		t.Fatal(err)
	}

	accepted, err := s.app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: s.session.ID, Text: "never mind, stop waiting"})
	if err != nil || accepted.Status != core.AcceptRunStatusSteered {
		t.Fatalf("accepted = %+v, %v", accepted, err)
	}
	waitRunStatus(t, db, s.run.ID, core.RunStatusCompleted)
	if got := s.woken(t); !strings.Contains(got, "never mind, stop waiting") {
		t.Fatalf("the woken request sends %q", got)
	}
}

// parkHookStore runs onPark when the engine checkpoints a run that awaits.
type parkHookStore struct {
	*store.SQLiteStore
	once   sync.Once
	onPark func()
}

func (s *parkHookStore) SaveRunCheckpoint(ctx context.Context, checkpoint core.RunCheckpoint) error {
	if strings.Contains(string(checkpoint.EngineState), `"await"`) {
		s.once.Do(s.onPark)
	}
	return s.SQLiteStore.SaveRunCheckpoint(ctx, checkpoint)
}

func TestTaskFinishingWhileTheRunParksStillWakesIt(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	hooked := &parkHookStore{SQLiteStore: db}
	s, taskID := newAwaitScenario(t, hooked, db, func(id string) string { return `{"ids":["` + id + `"]}` })
	hooked.onPark = func() {
		if _, err := s.app.CancelTask(context.Background(), taskID); err != nil {
			t.Error(err)
		}
	}

	if err := s.app.ExecuteRun(context.Background(), s.run.ID); err != nil {
		t.Fatal(err)
	}

	waitRunStatus(t, db, s.run.ID, core.RunStatusCompleted)
	if got := s.woken(t); !strings.Contains(got, "Background task "+taskID+" was stopped") {
		t.Fatalf("the woken request sends %q", got)
	}
}

func TestAwaitReportsTasksThatAlreadyFinished(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	app := core.New(db).WithSessionFiles(t.TempDir())
	session := permissionSession(t, db, "session_done", t.TempDir(), core.PermissionModeDefault, "")
	started, err := app.RunCommand(context.Background(), tools.Call{SessionID: session.ID}, tools.Command{Command: "exit 3", Background: true})
	if err != nil {
		t.Fatal(err)
	}
	waitTaskStatus(t, db, started.TaskID, core.TaskStatusFailed)
	await := core.AwaitToolExecutors(app)[0]

	result, err := await.Execute(context.Background(), tools.Call{SessionID: session.ID, RunID: "run_x", Args: json.RawMessage(`{"ids":["` + started.TaskID + `"]}`)})
	if err != nil || result.Await != nil || result.Content != "Already finished: "+started.TaskID+" failed, exit code 3." {
		t.Fatalf("result = %+v, %v", result, err)
	}
	idle, err := await.Execute(context.Background(), tools.Call{SessionID: session.ID, RunID: "run_x", Args: json.RawMessage(`{}`)})
	if err != nil || idle.Await != nil || !strings.Contains(idle.Content, "nothing to wait for") {
		t.Fatalf("idle = %+v, %v", idle, err)
	}
	runless, err := await.Execute(context.Background(), tools.Call{SessionID: session.ID, Args: json.RawMessage(`{}`)})
	if err != nil || !runless.IsError {
		t.Fatalf("run-less = %+v, %v", runless, err)
	}
}
```

In `internal/providers/ai/gemini/schema_test.go`, replace `func registeredToolDefinitions` with:

```go
// registeredToolDefinitions mirrors the daemon's registry, minus modules that import this package.
func registeredToolDefinitions(t *testing.T) []providers.ToolDefinition {
	t.Helper()
	app := core.New(nil)
	web := webtools.NewWebService(nil, nil)
	registry := tools.NewCoreCodingRegistry(
		automation.NewReminderTool(nil),
		automation.NewScheduledAITaskTool(nil),
		deliverymodule.NewSendFileTool(nil, nil),
		webtools.NewWebFetchExecutorWithService(web),
		webtools.NewWebSearchExecutorWithService(web),
	)
	storage, err := storagemodule.New(storagemodule.Config{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, executors := range [][]tools.Executor{
		webtools.NewWebResearchExecutorsWithService(web),
		tools.NewShellExecutors(app),
		core.TodoToolExecutors(app),
		core.AwaitToolExecutors(app),
		core.MemoryToolExecutors(app),
		core.SubagentToolExecutors(app),
		tools.NewOSMGeoExecutors(tools.NewOSMService(tools.OSMConfig{})),
		skills.ToolExecutors(nil),
	} {
		if err := registry.Register(executors...); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.RegisterTools(registry); err != nil {
		t.Fatal(err)
	}
	var definitions []providers.ToolDefinition
	for _, spec := range registry.List() {
		definitions = append(definitions, providers.ToolDefinition{Name: spec.ID, Description: spec.Description, InputSchema: spec.InputJSONSchema})
	}
	return definitions
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/core -run '^(TestAwaitedTaskFinishingWakesTheRun|TestAwaitTimerWakesTheRun|TestUserMessageWakesAnAwaitingRun|TestTaskFinishingWhileTheRunParksStillWakesIt|TestAwaitReportsTasksThatAlreadyFinished)$'
```

Expected: build fails (`core.AwaitToolExecutors`, `WakeDueRuns`, `GetRunWakeup` users undefined).

- [ ] **Step 3: Implement**

Create `internal/core/await.go`:

```go
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const awaitToolName = "await"

// An await waits 10 minutes unless the call names up to an hour.
const (
	defaultAwaitTimeout = 10 * time.Minute
	maxAwaitTimeout     = time.Hour
)

type awaitInput struct {
	IDs            []string `json:"ids,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

type awaitTool struct {
	app *Core
}

// AwaitToolExecutors returns the await tool.
func AwaitToolExecutors(app *Core) []tools.Executor {
	return []tools.Executor{&awaitTool{app: app}}
}

func (t *awaitTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              awaitToolName,
		Name:            "Await",
		Description:     "Wait for background tasks to finish instead of polling them. Ends your turn; you are woken when one of the tasks finishes, the user writes, or the timeout passes.",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "core.await",
		Category:        tools.CategoryAutomation,
		Profiles:        []tools.Profile{tools.ProfileCoding},
		OutputKind:      tools.OutputText,
		InputJSONSchema: awaitToolSchema,
	}
}

// Execute checks what the call waits for and asks the engine to wait; tasks
// that already finished are reported at once instead.
func (t *awaitTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input awaitInput
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &input); err != nil {
			return tools.Result{}, tools.InvalidArgs(awaitToolName, err)
		}
	}
	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	switch {
	case strings.TrimSpace(call.RunID) == "":
		return awaitError("await waits inside a run only."), nil
	case input.TimeoutSeconds < 0 || timeout > maxAwaitTimeout:
		return awaitError(fmt.Sprintf("timeout_seconds is at most %d.", int(maxAwaitTimeout/time.Second))), nil
	case timeout == 0:
		timeout = defaultAwaitTimeout
	}
	waiting, finished, err := t.app.awaitedTasks(ctx, call.SessionID, input.IDs)
	if err != nil {
		return tools.Result{}, err
	}
	if len(waiting) == 0 {
		if len(finished) == 0 {
			return tools.Result{Content: "No background task is running; there is nothing to wait for.", Status: tools.ResultStatusNeutral}, nil
		}
		return tools.Result{Content: "Already finished: " + strings.Join(finished, "; ") + ".", Status: tools.ResultStatusNeutral}, nil
	}
	await := &tools.Await{Until: t.app.now().UTC().Add(timeout)}
	if len(input.IDs) > 0 {
		await.TaskIDs = waiting
	}
	content := fmt.Sprintf("Waiting up to %s for %s. You are woken when one of them finishes, the user writes, or the time is up.", timeout, strings.Join(waiting, ", "))
	return tools.Result{Content: content, Status: tools.ResultStatusNeutral, Await: await}, nil
}

// awaitedTasks sorts the tasks an await names into those still running and
// those that finished (with their status); no IDs means every running
// background task of the session.
func (c *Core) awaitedTasks(ctx context.Context, sessionID string, ids []string) (waiting []string, finished []string, err error) {
	if len(ids) == 0 {
		tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: sessionID, Statuses: []TaskStatus{TaskStatusPending, TaskStatusRunning, TaskStatusWaitingApproval}})
		if err != nil {
			return nil, nil, err
		}
		for _, task := range tasks {
			if task.Background {
				waiting = append(waiting, task.ID)
			}
		}
		return waiting, nil, nil
	}
	for _, id := range ids {
		task, err := c.sessionTask(ctx, sessionID, id)
		if err != nil {
			return nil, nil, err
		}
		if task.FinishedAt == nil {
			waiting = append(waiting, task.ID)
			continue
		}
		status := string(task.Status)
		if task.ExitCode != nil {
			status += fmt.Sprintf(", exit code %d", *task.ExitCode)
		}
		finished = append(finished, task.ID+" "+status)
	}
	return waiting, finished, nil
}

func awaitError(text string) tools.Result {
	return tools.Result{Content: text, Status: tools.ResultStatusError, IsError: true}
}

var awaitToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "ids": {"type": "array", "items": {"type": "string"}, "description": "Background task ids to wait for; none waits for any running background task."},
    "timeout_seconds": {"type": "integer", "minimum": 0, "maximum": 3600, "description": "How long to wait at most; default 600."}
  },
  "additionalProperties": false
}`)
```

In `internal/core/run_execute.go`, replace `func (Core) resumeParkedRun` with:

```go
// resumeParkedRun starts a run that parked while what it waits for arrived:
// ResolveApproval or an event found it still active and left the start to
// this check, which runs once the run is no longer active.
func (c *Core) resumeParkedRun(runID string) {
	if c.lifetime.Err() != nil {
		return
	}
	ctx := context.Background()
	run, err := c.store.GetRun(ctx, runID)
	if err == nil {
		switch run.Status {
		case RunStatusWaitingApproval:
			err = c.resumeDecidedRun(ctx, run.SessionID, runID)
		case RunStatusWaitingEvents:
			err = c.wakeWaitingRun(ctx, run.SessionID, runID)
		}
	}
	if err != nil && !ignoreMissing(err) {
		log.Printf("core: resume parked run %q failed: %v", runID, err)
	}
}
```

In `internal/core/run_outcome.go`, replace `func (Core) applyOutcome` with:

```go
// applyOutcome persists how the engine left a native run and reports whether
// the run was kept for recovery.
func (c *Core) applyOutcome(ctx context.Context, run Run, outcome agent.Outcome) (bool, error) {
	switch outcome.Status {
	case agent.StatusCompleted:
		if outcome.Assistant == nil {
			return false, nil
		}
		run.StopReason = outcome.StopReason
		return false, c.completeAssistantTurn(ctx, &run, run.SessionID, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusWaitingApproval:
		return false, c.setRunStatus(ctx, &run, RunStatusWaitingApproval, "")
	case agent.StatusWaitingEvents:
		return false, c.parkRun(ctx, run, outcome.Counters.Await)
	case agent.StatusCanceled:
		return false, c.finishCanceledAssistant(ctx, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusFailed:
		run.StopReason = outcome.StopReason
		if outcome.MarkErrored && outcome.Assistant != nil {
			return false, c.persistAssistantError(ctx, run, outcome.Assistant, outcome.AssistantSaved, outcome.Err)
		}
		return false, c.failRunByID(ctx, run, outcome.Err)
	case agent.StatusInterrupted:
		return c.applyInterruptedOutcome(run, outcome)
	default:
		return false, fmt.Errorf("core: unknown run outcome %q", outcome.Status)
	}
}
```

In `internal/core/run_outcome.go`, replace `func (Core) applyInterruptedOutcome` with:

```go
// applyInterruptedOutcome commits what the run reached before its context stopped,
// or keeps it running with a recovery checkpoint (reported as true).
func (c *Core) applyInterruptedOutcome(run Run, outcome agent.Outcome) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runInterruptionPersistenceTimeout)
	defer cancel()
	latest, err := c.store.GetRun(ctx, run.ID)
	if err != nil {
		return false, err
	}
	if latest.Status == RunStatusCanceled {
		return false, c.finishCanceledAssistant(ctx, outcome.Assistant, outcome.AssistantSaved)
	}
	if subagentRunStatusTerminal(latest.Status) {
		return false, nil
	}
	switch outcome.Reached {
	case agent.StatusCompleted:
		latest.StopReason = outcome.StopReason
		return false, c.completeAssistantTurn(ctx, &latest, latest.SessionID, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusWaitingApproval:
		pending, err := c.runHasPendingApprovals(ctx, latest.SessionID, latest.ID)
		if err != nil {
			return false, err
		}
		if pending {
			return false, c.setRunStatus(ctx, &latest, RunStatusWaitingApproval, "")
		}
	case agent.StatusWaitingEvents:
		return false, c.parkRun(ctx, latest, outcome.Counters.Await)
	}
	if err := c.preserveRunForRecovery(ctx, latest, outcome.Assistant, outcome.AssistantSaved); err != nil {
		return false, err
	}
	current, err := c.store.GetRun(ctx, latest.ID)
	if err != nil {
		return false, err
	}
	return !subagentRunStatusTerminal(current.Status), nil
}
```

In `internal/core/run_recovery.go`, replace `func (Core) RecoverActiveRuns` with:

```go
func (c *Core) RecoverActiveRuns(ctx context.Context) error {
	if c == nil || c.store == nil {
		return nil
	}
	runs, err := c.store.ListActiveRuns(ctx)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.Status == RunStatusWaitingEvents {
			if err := c.wakeWaitingRun(ctx, run.SessionID, run.ID); err != nil {
				return fmt.Errorf("wake waiting run %s: %w", run.ID, err)
			}
			continue
		}
		start, err := c.prepareInactiveRunForRecovery(ctx, run.ID)
		if err != nil {
			return fmt.Errorf("recover interrupted run %s: %w", run.ID, err)
		}
		if start {
			if err := c.startRun(ctx, run.ID); err != nil {
				return fmt.Errorf("restart recovered run %s: %w", run.ID, err)
			}
		}
	}
	return nil
}
```

In `internal/core/run_recovery.go`, replace `func (Core) prepareClaimedRun` with:

```go
// prepareClaimedRun lets a persisted workflow activity recover its own orphan
// inline. This closes the startup race where the workflow worker can poll an
// old activity before RecoverActiveRuns has reset the durable run to accepted.
func (c *Core) prepareClaimedRun(ctx context.Context, runID string) (bool, error) {
	run, err := c.store.GetRun(ctx, normalizeText(runID))
	if err != nil {
		return false, err
	}
	gate := c.sessionGate(run.SessionID)
	gate.Lock()
	defer gate.Unlock()

	run, err = c.store.GetRun(ctx, run.ID)
	if err != nil {
		return false, err
	}
	switch run.Status {
	case RunStatusAccepted:
		return true, nil
	case RunStatusRunning:
		return c.prepareRunAfterCrash(ctx, &run)
	case RunStatusWaitingApproval:
		pending, err := c.runHasPendingApprovals(ctx, run.SessionID, run.ID)
		if err != nil {
			return false, err
		}
		return !pending, nil
	case RunStatusWaitingEvents:
		// A woken run parks again at once when nothing it waits for arrived.
		return true, nil
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
		return false, nil
	default:
		return false, fmt.Errorf("core: unsupported run status %q", run.Status)
	}
}
```

Create `internal/core/run_wakeups.go`:

```go
package core

import (
	"context"
	"errors"
	"log"
	"slices"
	"time"

	"github.com/Suren878/matrixclaw/internal/tools"
)

// wakeupInterval is how often due run wakeups are looked for.
const wakeupInterval = time.Second

// parkRun leaves a run waiting for what its await asked for; its wakeup keeps
// the timer and the awaited tasks.
func (c *Core) parkRun(ctx context.Context, run Run, await *tools.Await) error {
	if await == nil {
		return errors.New("core: a run waiting for events has nothing to wait for")
	}
	wakeup := RunWakeup{RunID: run.ID, SessionID: run.SessionID, WakeAt: await.Until, TaskIDs: await.TaskIDs}
	if err := c.store.SaveRunWakeup(ctx, wakeup); err != nil {
		return err
	}
	return c.setRunStatus(ctx, &run, RunStatusWaitingEvents, "")
}

// wakeWaitingRun starts a run parked in waiting_events once what it waits for
// happened: an awaited task finished, the user wrote, or its timer ran out.
// The session gate makes a wake and the park's own check start it once.
func (c *Core) wakeWaitingRun(ctx context.Context, sessionID string, runID string) error {
	gate := c.sessionGate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	run, err := c.store.GetRun(ctx, runID)
	if errors.Is(err, ErrNotFound) {
		return c.store.DeleteRunWakeup(ctx, runID)
	}
	if err != nil {
		return err
	}
	if subagentRunStatusTerminal(run.Status) {
		return c.store.DeleteRunWakeup(ctx, runID)
	}
	if run.Status != RunStatusWaitingEvents {
		return nil
	}
	due, err := c.waitOver(ctx, run)
	if err != nil || !due {
		return err
	}
	if err := c.store.DeleteRunWakeup(ctx, run.ID); err != nil {
		return err
	}
	return c.startRun(ctx, run.ID)
}

// waitOver reports whether a waiting run has something to go on with.
func (c *Core) waitOver(ctx context.Context, run Run) (bool, error) {
	wakeup, err := c.store.GetRunWakeup(ctx, run.ID)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !c.now().Before(wakeup.WakeAt) {
		return true, nil
	}
	steers, err := c.store.ListPendingSteerInputs(ctx, run.SessionID, run.ID)
	if err != nil || len(steers) > 0 {
		return len(steers) > 0, err
	}
	events, err := c.store.ListTasks(ctx, TaskFilter{SessionID: run.SessionID, Undelivered: true})
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(events, func(task Task) bool {
		return len(wakeup.TaskIDs) == 0 || slices.Contains(wakeup.TaskIDs, task.ID)
	}), nil
}

// wakeSessionRun wakes the session's run if it waits for events.
func (c *Core) wakeSessionRun(ctx context.Context, sessionID string) error {
	active, err := c.store.GetActiveRunBySession(ctx, sessionID)
	if errors.Is(err, ErrNotFound) || err == nil && active.Status != RunStatusWaitingEvents {
		return nil
	}
	if err != nil {
		return err
	}
	return c.wakeWaitingRun(ctx, sessionID, active.ID)
}

// WakeDueRuns starts the waiting runs whose timer ran out.
func (c *Core) WakeDueRuns(ctx context.Context) error {
	due, err := c.store.ListDueRunWakeups(ctx, c.now().UTC())
	if err != nil {
		return err
	}
	var errs []error
	for _, wakeup := range due {
		errs = append(errs, c.wakeWaitingRun(ctx, wakeup.SessionID, wakeup.RunID))
	}
	return errors.Join(errs...)
}

// RunWakeups wakes due runs until ctx ends; timers stored before a restart
// fire on its first tick.
func (c *Core) RunWakeups(ctx context.Context) {
	ticker := time.NewTicker(wakeupInterval)
	defer ticker.Stop()
	for {
		if err := c.WakeDueRuns(ctx); err != nil && ctx.Err() == nil {
			log.Printf("core: wake due runs: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
```

In `internal/core/runs.go`, replace `func (Core) AcceptRun` with:

```go
func (c *Core) AcceptRun(ctx context.Context, input HandleMessageInput) (AcceptRunResult, error) {
	if input.Continue {
		return c.acceptContinueRun(ctx, input)
	}
	text := normalizeText(input.Text)
	parts := transcript.NormalizeMessageParts(text, input.Parts)
	if text == "" && !messagePartsHaveUserContent(parts) {
		return AcceptRunResult{}, fmt.Errorf("%w: message text is required", ErrInvalidInput)
	}

	session, err := c.resolveSession(ctx, input)
	if err != nil {
		return AcceptRunResult{}, err
	}

	var result AcceptRunResult
	var startRunID string
	var interruptRunID string
	var wakeRunID string
	gate := c.sessionGate(session.ID)
	gate.Lock()
	active, err := c.store.GetActiveRunBySession(ctx, session.ID)
	switch {
	case err == nil:
		pending, createErr := c.createPendingSessionInput(ctx, session, active, input, text, parts)
		if createErr != nil {
			gate.Unlock()
			return AcceptRunResult{}, createErr
		}
		result = AcceptRunResult{
			SessionID: session.ID,
			Status:    acceptRunStatusForInputMode(pending.Mode),
			Input:     &pending,
		}
		switch {
		case pending.Mode == BusyInputModeInterrupt:
			interruptRunID = active.ID
		case pending.Mode == BusyInputModeSteer && active.Status == RunStatusWaitingEvents:
			wakeRunID = active.ID
		}
	case errors.Is(err, ErrNotFound):
		result, err = c.createAcceptedRun(ctx, session, text, parts, input.Client, input.ExternalKey, input.ClientCapabilities, input.DeliveryAddress, "")
		if err != nil {
			gate.Unlock()
			return AcceptRunResult{}, err
		}
		startRunID = result.Run.ID
	default:
		gate.Unlock()
		return AcceptRunResult{}, err
	}
	gate.Unlock()

	if wakeRunID != "" {
		if err := c.wakeWaitingRun(ctx, session.ID, wakeRunID); err != nil {
			return result, err
		}
	}
	if interruptRunID != "" {
		if _, err := c.CancelRun(ctx, interruptRunID); err != nil {
			return result, err
		}
		if _, err := c.startNextPendingSessionInput(ctx, session.ID); err != nil {
			return result, err
		}
	}

	// The daemon hands execution off here; transport is already out of the picture.
	if startRunID != "" {
		if err := c.startRun(ctx, startRunID); err != nil {
			return c.failAcceptedRun(ctx, result, err)
		}
	}

	return result, nil
}
```

In `internal/core/session_inputs.go`, replace `func normalizeBusyInputMode` with:

```go
// normalizeBusyInputMode reads how a message for a busy session is taken; one
// that names no mode steers the run.
func normalizeBusyInputMode(mode BusyInputMode) BusyInputMode {
	switch BusyInputMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case BusyInputModeQueue:
		return BusyInputModeQueue
	case BusyInputModeInterrupt:
		return BusyInputModeInterrupt
	default:
		return BusyInputModeSteer
	}
}
```

In `internal/core/session_inputs.go`, replace `func (Core) createPendingSessionInput` with:

```go
func (c *Core) createPendingSessionInput(ctx context.Context, session Session, active Run, input HandleMessageInput, text string, parts []transcript.MessagePart) (SessionInput, error) {
	mode := normalizeBusyInputMode(input.BusyMode)
	if mode == BusyInputModeQueue && active.Status == RunStatusWaitingEvents {
		// A run waiting for events takes any message at once.
		mode = BusyInputModeSteer
	}
	if mode == BusyInputModeSteer && !sessionAcceptsNativeSteer(session) {
		mode = BusyInputModeQueue
	}
	now := c.now().UTC()
	pending := SessionInput{
		ID:                 c.newID("input"),
		SessionID:          session.ID,
		TargetRunID:        active.ID,
		Mode:               mode,
		Status:             SessionInputStatusPending,
		Text:               text,
		Parts:              parts,
		Client:             normalizeText(input.Client),
		ExternalKey:        normalizeText(input.ExternalKey),
		ClientCapabilities: input.ClientCapabilities,
		DeliveryAddress:    cloneRawMessage(input.DeliveryAddress),
		WorkingDir:         normalizeText(input.WorkingDir),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := c.store.CreateSessionInput(ctx, pending); err != nil {
		return SessionInput{}, err
	}
	c.publishSessionInputUpdated(pending)
	return pending, nil
}
```

In `internal/core/shell_tasks.go`, replace `func (Core) finishTask` with:

```go
// finishTask ends a task unless it already ended and tells its session's
// clients; the finished task becomes an event for the session.
func (c *Core) finishTask(ctx context.Context, taskID string, status TaskStatus, exitCode *int, errText string) error {
	finished, err := c.store.FinishTask(ctx, taskID, status, exitCode, errText, c.now().UTC())
	if err != nil || !finished {
		return err
	}
	task, err := c.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	c.publishTaskUpdated(task)
	c.taskFinished(ctx, task)
	return nil
}
```

In `internal/core/shell_tasks.go`, add:

```go
// taskFinished wakes the session's run if it waits for events; a running run
// reads the event at its next step.
func (c *Core) taskFinished(ctx context.Context, task Task) {
	if !task.Background || task.DeliveredAt != nil {
		return
	}
	if err := c.wakeSessionRun(ctx, task.SessionID); err != nil {
		log.Printf("core: wake session %q for task %q: %v", task.SessionID, task.ID, err)
	}
}
```

In `internal/core/subagents_lifecycle.go`, replace `func (Core) syncAsyncSubagentTaskAfterRun` with:

```go
func (c *Core) syncAsyncSubagentTaskAfterRun(ctx context.Context, task SubagentTask, run Run) error {
	if taskStatusTerminal(task.Status) {
		return nil
	}
	switch run.Status {
	case RunStatusWaitingApproval:
		_, err := c.mirrorPendingSubagentApproval(ctx, task)
		return err
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
	default:
		return nil
	}
	summary, failed := c.subagentRunSummary(ctx, task.ChildSessionID, task.ChildRunID, nil)
	status := TaskStatusCompleted
	errText := ""
	if run.Status == RunStatusCanceled {
		status = TaskStatusCanceled
		errText = summary
	} else if failed {
		status = TaskStatusFailed
		errText = summary
	}
	task, err := c.finishSubagentTaskRecord(ctx, task, status, summary, errText, true)
	if err != nil {
		return err
	}
	c.publishSubagentToolUpdate(task)
	if finished, err := c.store.GetTask(ctx, task.ID); err == nil {
		c.taskFinished(ctx, finished)
	}
	return c.deliverPendingSubagentCompletionsForParent(ctx, task.ParentSessionID)
}
```

In `internal/daemoncmd/run.go`, replace `func Run` with:

```go
func Run(ctx context.Context) error {
	bootstrap, err := loadBootstrap()
	if err != nil {
		return err
	}

	sqliteStore, err := store.NewSQLite(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = sqliteStore.Close() }()
	automationStore, err := automation.NewSQLiteStore(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = automationStore.Close() }()
	workStore, err := work.NewSQLiteStore(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = workStore.Close() }()
	webResearchStore := webresearch.NewStore(workStore)

	storageModule, err := localstorage.New(localstorage.Config{
		Root: defaultStorageRoot(bootstrap.DBPath),
	})
	if err != nil {
		return err
	}
	mcpModule, err := mcpmodule.New(ctx, mcpConfigWithBrowser(bootstrap.ExternalAgents))
	if err != nil {
		log.Printf("matrixclawd mcp module disabled: %v", err)
		mcpModule, _ = mcpmodule.New(ctx, setup.MCPConfig{})
	}
	defer func() { _ = mcpModule.Close() }()
	skillsModule, err := skillsmodule.New(skillsConfigFromBootstrap(bootstrap))
	if err != nil {
		return err
	}
	defer func() { _ = skillsModule.Close() }()
	moduleRegistry := modules.NewRegistry(storageModule, mcpModule, skillsModule)

	app := core.New(sqliteStore).
		WithSessionLLMs(bootstrap.SessionLLMs).
		WithRunBudgets(bootstrap.Budgets).
		WithSessionFiles(sessionFilesRoot(bootstrap.DBPath)).
		WithCompactModel(bootstrap.CompactModel.Provider, bootstrap.CompactModel.Model).
		WithContextWindowCap(bootstrap.WindowCap).
		WithModelConcurrency(bootstrap.ModelConcurrency).
		WithWorkStore(workStore).
		WithAttachmentReader(storageAttachmentReader{store: storageModule.Store()}).
		WithSkillsContext(skillsModule).
		WithRuntimeStatusContext(&setupRuntimeStatusContext{setup: bootstrap.SetupService, runtime: localruntime.New("")})
	// The workflow worker (below) may start executing persisted runs before
	// supervisor.ApplyBootstrap runs, so the profile is set here too, via the
	// same helper, ensuring module context is never missing.
	applyAssistantProfile(app, bootstrap.Assistant, moduleRegistry.Context)
	externalRegistry, externalRuntimes, err := builtins.BuildRegistry(bootstrap.ExternalAgents)
	if err != nil {
		return err
	}
	app.WithExternalAgents(externalRegistry, sqliteStore)
	automationService := automation.NewService(automationStore, app, bootstrap.Timezone).
		WithDeliveryTargets(automationDeliveryTargets(bootstrap))
	webSearchConfig := webSearchProviderConfig(bootstrap.SetupService)
	webResearchEngine := newWebResearchEngine(bootstrap.DBPath, bootstrap.ExternalAgents.MCP, mcpModule, webResearchStore, webSearchConfig)
	webTools := webtools.NewWebService(webSearchConfig, webResearchEngine)
	osmGeo := tools.NewOSMServiceFromEnv()
	extraTools := []tools.Executor{
		automation.NewReminderTool(automationService),
		automation.NewScheduledAITaskTool(automationService),
		deliverymodule.NewSendFileTool(storageModule.Store(), app),
		telephonymodule.NewCallTool(bootstrap.SetupService),
		telephonymodule.NewEndCallTool(bootstrap.SetupService),
		voicemodule.NewTextToSpeechTool(bootstrap.SetupService),
		webtools.NewWebFetchExecutorWithService(webTools),
		webtools.NewWebSearchExecutorWithService(webTools),
	}
	extraTools = append(extraTools, webtools.NewWebResearchExecutorsWithService(webTools)...)
	toolRegistry := tools.NewCoreCodingRegistry(extraTools...)
	if err := toolRegistry.Register(tools.NewShellExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.TodoToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.AwaitToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.MemoryToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.SubagentToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Err(); err != nil {
		return err
	}
	if err := toolRegistry.Register(tools.NewOSMGeoExecutors(osmGeo)...); err != nil {
		return err
	}
	if err := moduleRegistry.RegisterTools(toolRegistry); err != nil {
		return err
	}
	app.WithTools(newSetupAwareToolExecutor(toolRegistry, bootstrap.SetupService))
	// Shell tasks do not survive a restart; this runs before any run can start new ones.
	if err := app.RecoverTasks(ctx); err != nil {
		log.Printf("matrixclawd background task recovery failed: %v", err)
	}
	// The workflow worker executes persisted runs as soon as it starts, so it
	// starts only once the core is fully wired and before anything accepts runs.
	lifetime, stopLifetime := context.WithCancel(ctx)
	defer stopLifetime()
	app.WithLifetime(lifetime)
	runStarter, err := goworkflows.NewForStore(bootstrap.DBPath, app)
	if err != nil {
		return err
	}
	defer func() {
		// End the lifetime first so runs interrupted by closing the worker are not rescheduled.
		stopLifetime()
		_ = runStarter.Close()
	}()
	app.WithRunStarter(runStarter)
	safego.Go("core.runWakeups", func() { app.RunWakeups(lifetime) })
	server := api.New(app)
	server.SetAPIToken(bootstrap.APIToken)
	server.SetAutomationService(automationService)
	server.SetStorageStore(storageModule.Store())
	server.SetSkillsService(skillsModule.Service())
	server.SetSetupService(bootstrap.SetupService)
	server.SetRealtimeVoiceService(newRealtimeVoiceManager(bootstrap.SetupService, app))
	supervisor := newSupervisor(ctx, server, app, osmGeo)
	supervisor.SetModuleContext(moduleRegistry.Context)
	supervisor.SetExternalAgents(sqliteStore, externalRuntimes)
	defer supervisor.CloseExternalAgents()
	httpServer := &http.Server{
		Addr:              bootstrap.Addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 2)
	safego.Go("daemon.httpServer", func() {
		err := httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	})

	if err := supervisor.ApplyBootstrap(bootstrap); err != nil {
		return err
	}
	startConfiguredVoiceRuntimes(ctx, bootstrap.SetupService)
	safego.Go("automation.Run", func() { automationService.Run(ctx) })
	safego.Go("webresearch.Run", func() { webResearchEngine.Start(ctx) })
	safego.Go("supervisor.deliverStartupNotifications", func() {
		supervisor.DeliverPendingStartupNotifications(bootstrap)
	})
	safego.Go("core.recoverState", func() {
		if err := app.RecoverActiveRuns(context.Background()); err != nil {
			log.Printf("matrixclawd active run recovery failed: %v", err)
		}
		if err := app.RecoverSessionInputs(context.Background()); err != nil {
			log.Printf("matrixclawd session input recovery failed: %v", err)
		}
		if err := app.RecoverSubagentTasks(context.Background()); err != nil {
			log.Printf("matrixclawd subagent recovery failed: %v", err)
		}
	})

	log.Printf("matrixclawd bootstrap: setup=%s", bootstrap.SetupPath)
	log.Printf("matrixclawd listening on %s using %s", bootstrap.Addr, bootstrap.DBPath)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/core -run '^(TestAwaitedTaskFinishingWakesTheRun|TestAwaitTimerWakesTheRun|TestUserMessageWakesAnAwaitingRun|TestTaskFinishingWhileTheRunParksStillWakesIt|TestAwaitReportsTasksThatAlreadyFinished)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/core/await.go \
  internal/core/await_test.go \
  internal/core/run_execute.go \
  internal/core/run_outcome.go \
  internal/core/run_recovery.go \
  internal/core/run_wakeups.go \
  internal/core/runs.go \
  internal/core/session_inputs.go \
  internal/core/shell_tasks.go \
  internal/core/subagents_lifecycle.go \
  internal/daemoncmd/run.go \
  internal/providers/ai/gemini/schema_test.go
git commit -m "feat(core): await parks runs; events, timers and user messages wake them

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 4: Clients show runs waiting for background tasks

**Files:**
- Modify: `clients/telegram/delivery.go`
- Modify: `clients/telegram/render_helpers.go`
- Modify: `clients/terminal/chat/runtime/app_events.go`
- Modify: `clients/terminal/chat/runtime/app_layout_views.go`
- Test (modify): `clients/telegram/typing_indicator_test.go`
- Test (create): `clients/terminal/chat/runtime/app_waiting_test.go`

Clients learn the status.

- [ ] **Step 1: Write the failing tests**

In `clients/telegram/typing_indicator_test.go`, add:

```go
func TestRunWaitingForBackgroundTasksStopsTypingAndSaysSo(t *testing.T) {
	api := &recordingBotAPI{}
	worker := &Worker{api: api, config: Config{ChatActionInterval: 4 * time.Second}, now: time.Now}
	run := &core.Run{ID: "run-1", Status: core.RunStatusWaitingEvents}

	worker.updateRunTypingIndicator(context.Background(), chatTarget{chatID: 42, externalKey: "telegram:42"}, run)

	if got := api.actionCount(); got != 0 {
		t.Fatalf("typing actions while waiting = %d", got)
	}
	if got := renderRunStatus(*run); got != "Waiting for background tasks..." {
		t.Fatalf("status = %q", got)
	}
}
```

Create `clients/terminal/chat/runtime/app_waiting_test.go`:

```go
package runtime

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestRunWaitingForBackgroundTasksKeepsTheSessionBusy(t *testing.T) {
	m := newApp(context.Background(), nil)
	run := core.Run{ID: "run_1", SessionID: "session_1", Status: core.RunStatusWaitingEvents}
	m.read = viewmodel.NewReadModel(core.ClientSnapshot{SessionID: "session_1", Run: &run})

	if !runIsActive(&run) {
		t.Fatal("a run waiting for background tasks is not active")
	}
	if got := m.workingStatusPhase(); got != "Waiting for background tasks" {
		t.Fatalf("phase = %q", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./clients/telegram -run '^(TestRunWaitingForBackgroundTasksStopsTypingAndSaysSo)$'
go test ./clients/terminal/chat/runtime -run '^(TestRunWaitingForBackgroundTasksKeepsTheSessionBusy)$'
```

Expected: the TUI test fails (`runIsActive` is false, phase "Waiting for model"); the Telegram test gets "Run status: waiting_events".

- [ ] **Step 3: Implement**

In `clients/telegram/delivery.go`, replace `func (Worker) deliverInlineRunDelivery` with:

```go
func (w *Worker) deliverInlineRunDelivery(ctx context.Context, target chatTarget, sessionID string, runID string, deliveryID string) error {
	daemon := w.daemon(target.externalKey)
	run, err := daemon.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	switch run.Status {
	case core.RunStatusAccepted, core.RunStatusRunning, core.RunStatusWaitingEvents:
		messages, err := daemon.ListMessages(ctx, sessionID, 0)
		if err != nil {
			return err
		}
		text := transcript.RunReply(messages, runID)
		if strings.TrimSpace(text) == "" {
			text = renderRunStatus(run)
		}
		return w.sendText(ctx, target, text)
	case core.RunStatusWaitingApproval:
		return w.sendText(ctx, target, "Approval required. Open the private Matrixclaw chat to approve or deny the request.")
	case core.RunStatusCompleted, core.RunStatusFailed, core.RunStatusCanceled:
	default:
		return nil
	}

	text := renderRunStatus(run)
	inlineVoiceDelivered := false
	messages, err := daemon.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return err
	}
	if run.Status == core.RunStatusCompleted {
		caption := ""
		if assistant := transcript.RunReply(messages, runID); assistant != "" {
			text = assistant
			caption = assistant
		}
		inlineVoiceDelivered, err = w.renderInlineVoiceToolResultUpdates(ctx, target, messages, runID, w.runRenderState(target.externalKey, runID), caption)
		if err != nil {
			return err
		}
	}
	if !inlineVoiceDelivered {
		if err := w.sendText(ctx, target, text); err != nil {
			return err
		}
	}
	if err := w.acknowledgeSentDelivery(ctx, daemon, deliveryID); err != nil {
		return err
	}
	w.clearRunRenderState(target.externalKey, runID)
	return nil
}
```

In `clients/telegram/delivery.go`, replace `func (Worker) deliverChatRunDelivery` with:

```go
func (w *Worker) deliverChatRunDelivery(ctx context.Context, target chatTarget, sessionID string, runID string, deliveryID string) error {
	daemon := w.daemon(target.externalKey)
	run, err := daemon.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	w.updateRunTypingIndicator(ctx, target, &run)
	switch run.Status {
	case core.RunStatusWaitingApproval:
		return w.deliverRunApprovals(ctx, target, daemon, sessionID, runID)
	case core.RunStatusAccepted, core.RunStatusRunning, core.RunStatusWaitingEvents:
		return w.deliverActiveRunProgress(ctx, target, daemon, sessionID, runID)
	case core.RunStatusCompleted, core.RunStatusFailed, core.RunStatusCanceled:
	default:
		return nil
	}

	messages, err := daemon.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return err
	}
	state := w.runRenderState(target.externalKey, runID)
	if err := w.renderToolCallUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderVoiceToolResultUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderToolResultUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderTodoUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderEngineNotes(ctx, target, messages, runID, state); err != nil {
		return err
	}
	assistantCtx := ctx
	if run.Status != core.RunStatusCompleted {
		assistantCtx = silentTelegramDelivery(ctx)
	}
	if err := w.renderAssistantUpdates(assistantCtx, target, messages, runID, state); err != nil {
		return err
	}
	if run.Status == core.RunStatusCompleted {
		if err := w.offerContinue(ctx, target, run, state); err != nil {
			return err
		}
	}
	if run.Status != core.RunStatusCompleted && !state.statusSent {
		if err := w.sendText(ctx, target, renderRunStatus(run)); err != nil {
			return err
		}
		state.statusSent = true
	}
	if err := w.acknowledgeSentDelivery(ctx, daemon, deliveryID); err != nil {
		return err
	}
	w.clearRunRenderState(target.externalKey, runID)
	return nil
}
```

In `clients/telegram/render_helpers.go`, replace `func renderRunStatus` with:

```go
func renderRunStatus(run core.Run) string {
	switch run.Status {
	case core.RunStatusAccepted, core.RunStatusRunning:
		return "Thinking..."
	case core.RunStatusWaitingApproval:
		return "Approval required."
	case core.RunStatusWaitingEvents:
		return "Waiting for background tasks..."
	case core.RunStatusCanceled:
		return "Run canceled."
	case core.RunStatusFailed:
		if strings.TrimSpace(run.Error) != "" {
			return "Run failed: " + strings.TrimSpace(run.Error)
		}
		return "Run failed."
	default:
		return "Run status: " + string(run.Status)
	}
}
```

In `clients/terminal/chat/runtime/app_events.go`, replace `func runIsActive` with:

```go
func runIsActive(run *core.Run) bool {
	if run == nil {
		return false
	}
	switch run.Status {
	case core.RunStatusAccepted, core.RunStatusRunning, core.RunStatusWaitingApproval, core.RunStatusWaitingEvents:
		return true
	default:
		return false
	}
}
```

In `clients/terminal/chat/runtime/app_layout_views.go`, replace `func (appModel) workingStatusPhase` with:

```go
func (m *appModel) workingStatusPhase() string {
	if m.read == nil {
		return "Waiting for model"
	}
	snapshot := m.currentSnapshot()
	if activeRunWaitingForPermission(snapshot) {
		return "Waiting for permission"
	}
	if snapshot.Run != nil && snapshot.Run.Status == core.RunStatusWaitingEvents {
		return "Waiting for background tasks"
	}
	if update, ok := latestActiveToolUpdate(snapshot, core.ToolLifecycleRequested); ok {
		if isSubagentToolName(update.ToolName) {
			if phase := activeSubagentPhase(snapshot); phase != "" {
				return phase
			}
		}
		return workingToolPhaseWithDetail(snapshot.Messages, update)
	}
	if snapshot.Run != nil && snapshot.Run.Status == core.RunStatusAccepted {
		return "Waiting for model"
	}
	if phase := modelOutputPhase(snapshot.Messages); phase != "" {
		return phase
	}
	if phase := activeSubagentPhase(snapshot); phase != "" {
		return phase
	}
	return "Waiting for model"
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./clients/telegram -run '^(TestRunWaitingForBackgroundTasksStopsTypingAndSaysSo)$'
go test ./clients/terminal/chat/runtime -run '^(TestRunWaitingForBackgroundTasksKeepsTheSessionBusy)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add clients/telegram/delivery.go \
  clients/telegram/render_helpers.go \
  clients/telegram/typing_indicator_test.go \
  clients/terminal/chat/runtime/app_events.go \
  clients/terminal/chat/runtime/app_layout_views.go \
  clients/terminal/chat/runtime/app_waiting_test.go
git commit -m "feat(clients): show runs waiting for background tasks

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 5: Idle sessions wake for finished work

**Files:**
- Modify: `clients/telegram/delivery.go`
- Modify: `clients/telegram/delivery_queue.go`
- Modify: `clients/telegram/worker.go`
- Modify: `internal/core/deliveries.go`
- Modify: `internal/core/ports.go`
- Modify: `internal/core/run_continue.go`
- Modify: `internal/core/run_wakeups.go`
- Modify: `internal/core/runs.go`
- Modify: `internal/core/session_inputs.go`
- Modify: `internal/core/shell_tasks.go`
- Modify: `internal/core/subagents_lifecycle.go`
- Modify: `internal/core/subagents_persistence.go`
- Modify: `internal/core/subagents_presentation.go`
- Create: `internal/core/task_wake.go`
- Modify: `internal/daemoncmd/run.go`
- Modify: `internal/store/sqlite_subagents.go`
- Test (modify): `clients/telegram/delivery_queue_test.go`
- Test (modify): `internal/core/native_run_characterization_test.go`
- Test (create): `internal/core/task_wake_test.go`
- Test (modify): `internal/store/sqlite_tasks_test.go`

Idle sessions wake for finished background work. This replaces the subagent-completion runs; `taskFinished` now starts `wakeSession` when the session has no active run, and `afterRunExecution` calls it when a top-level run ends (events that arrived after the run's last step). `createAcceptedRun` takes the run's trigger. Telegram learns the `notice` delivery.

- [ ] **Step 1: Write the failing tests**

In `clients/telegram/delivery_queue_test.go`, add:

```go
func TestNoticeDeliverySendsItsTextOnce(t *testing.T) {
	now := time.Unix(100, 0)
	notice := core.ClientDelivery{ID: "notice-1", Type: core.ClientDeliveryTypeNotice, SessionID: "s1", Summary: "Background task task_1 finished.", Address: encodeDeliveryAddress(DeliveryAddress{ChatID: 7})}
	d := &deliveryTestDaemon{deliveries: []core.ClientDelivery{notice}}
	api := &deliveryTestAPI{}
	w := newDeliveryTestWorker(t, d, api, &now)

	for range 2 {
		if err := w.deliverPendingNotices(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	if len(api.messages) != 1 || api.messages[0].ChatID != 7 || !strings.Contains(api.messages[0].Text, "task_1 finished") || !d.acked["notice-1"] {
		t.Fatalf("messages = %+v acked = %v", api.messages, d.acked)
	}
}
```

In `internal/core/native_run_characterization_test.go`, replace `func runAsyncSubagentScenario` with:

```go
func runAsyncSubagentScenario(t *testing.T) asyncSubagentScenario {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	t.Cleanup(cleanup)
	app.WithTools(tools.NewRegistry(core.SubagentToolExecutors(app)...))
	app.WithRunStarter(orchestration.NewStub(app))
	var mu sync.Mutex
	spawned := false
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return providers.Response{Text: "child async result"}, nil
		}
		for _, message := range request.Messages {
			if message.Role == "user" && strings.HasPrefix(message.Content, "Subagent ") && strings.Contains(message.Content, "finished: completed.") {
				return providers.Response{Text: "Synthesized."}, nil
			}
		}
		if !spawned {
			spawned = true
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-spawn", Name: "spawn_subagent", Arguments: []byte(`{"name":"Scanner","goal":"scan the tree","runtime":"matrixclaw"}`)}}}, nil
		}
		return providers.Response{Text: "Spawned."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "async", core.RunStatusAccepted, false)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := app.SubscribeEvents(ctx, session.ID)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)

	deadline := time.Now().Add(10 * time.Second)
	for {
		runs, err := db.ListSessionRuns(context.Background(), session.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if runs[0].Trigger == core.RunTriggerWake && runs[0].Status == core.RunStatusCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subagent completion follow-up run did not complete")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return asyncSubagentScenario{db: db, session: session, run: run, events: drainEvents(events)}
}
```

Create `internal/core/task_wake_test.go`:

```go
package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

var telegramAddress = json.RawMessage(`{"chat_id":42}`)

// wakeScenario is a session a Telegram chat last wrote to; its model answers
// every request with "Reported." and records them.
type wakeScenario struct {
	app      *core.Core
	db       *store.SQLiteStore
	session  core.Session
	starter  *executingRunStarter
	mu       sync.Mutex
	requests []providers.Request
}

func newWakeScenario(t *testing.T) *wakeScenario {
	t.Helper()
	db := openScenarioStore(t)
	app := core.New(db)
	s := &wakeScenario{app: app, db: db, starter: &executingRunStarter{app: app}}
	app.WithSessionFiles(t.TempDir()).WithRunStarter(s.starter)
	app.WithTools(tools.NewRegistry(core.AwaitToolExecutors(app)...))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests = append(s.requests, request)
		return providers.Response{Text: "Reported."}, nil
	})})
	s.session = permissionSession(t, db, "session_wake", t.TempDir(), core.PermissionModeDefault, "")
	return s
}

// userRun is a finished run the Telegram chat started.
func (s *wakeScenario) userRun(t *testing.T) core.Run {
	t.Helper()
	accepted, err := s.app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: s.session.ID, Client: "telegram", ExternalKey: "telegram:42", DeliveryAddress: telegramAddress, Text: "run the tests"})
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, s.db, accepted.Run.ID, core.RunStatusCompleted)
	return accepted.Run
}

func (s *wakeScenario) runTask(t *testing.T, command string) string {
	t.Helper()
	started, err := s.app.RunCommand(context.Background(), tools.Call{SessionID: s.session.ID}, tools.Command{Command: command, Background: true})
	if err != nil {
		t.Fatal(err)
	}
	return started.TaskID
}

func (s *wakeScenario) runs(t *testing.T) []core.Run {
	t.Helper()
	runs, err := s.db.ListSessionRuns(context.Background(), s.session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func TestFinishedTaskWakesAnIdleSessionWhereTheUserWrote(t *testing.T) {
	t.Parallel()
	s := newWakeScenario(t)
	s.userRun(t)

	taskID := s.runTask(t, "echo passed")

	var wake core.Run
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if runs := s.runs(t); runs[0].Trigger == core.RunTriggerWake && runs[0].Status == core.RunStatusCompleted {
			wake = runs[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no wake run: %+v", s.runs(t))
		}
	}
	s.starter.wait(t)
	if wake.Client != "telegram" || wake.ExternalKey != "telegram:42" {
		t.Fatalf("wake run = %+v", wake)
	}
	deliveries, err := s.db.ListClientDeliveries(context.Background(), core.ClientDeliveryFilter{RunID: wake.ID})
	if err != nil || len(deliveries) != 1 || string(deliveries[0].Address) != string(telegramAddress) {
		t.Fatalf("deliveries = %+v, %v", deliveries, err)
	}
	task, err := s.db.GetTask(context.Background(), taskID)
	if err != nil || task.DeliveredRunID != wake.ID {
		t.Fatalf("task = %+v, %v", task, err)
	}
	s.mu.Lock()
	last := s.requests[len(s.requests)-1]
	s.mu.Unlock()
	var sent []string
	for _, message := range last.Messages {
		sent = append(sent, message.Content)
	}
	if text := strings.Join(sent, "\n"); !strings.Contains(text, "Background task "+taskID+" finished with exit code 0.") || !strings.Contains(text, "passed") {
		t.Fatalf("the wake request sends:\n%s", text)
	}
}

func TestWakeChainStopsAfterTwentyRunsWithoutTheUser(t *testing.T) {
	t.Parallel()
	s := newWakeScenario(t)
	s.userRun(t)
	ctx := context.Background()
	for i := range 20 {
		at := time.Now().UTC().Add(time.Duration(i+1) * time.Second)
		id := fmt.Sprintf("run_wake_%d", i)
		message := transcript.Message{ID: "msg_" + id, SessionID: s.session.ID, RunID: id, Role: transcript.MessageRoleUser, Content: "woken", CreatedAt: at, UpdatedAt: at}
		run := core.Run{ID: id, SessionID: s.session.ID, UserMessageID: message.ID, Trigger: core.RunTriggerWake, Status: core.RunStatusCompleted, StartedAt: at, UpdatedAt: at}
		if err := s.db.AcceptMessage(ctx, message, run); err != nil {
			t.Fatal(err)
		}
	}
	before := len(s.runs(t))

	taskID := s.runTask(t, "true")

	var notices []core.ClientDelivery
	for deadline := time.Now().Add(10 * time.Second); len(notices) == 0; time.Sleep(10 * time.Millisecond) {
		var err error
		if notices, err = s.db.ListClientDeliveries(ctx, core.ClientDeliveryFilter{Type: core.ClientDeliveryTypeNotice}); err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("no notice")
		}
	}
	if len(notices) != 1 || notices[0].Client != "telegram" || string(notices[0].Address) != string(telegramAddress) || !strings.Contains(notices[0].Summary, taskID) {
		t.Fatalf("notices = %+v", notices)
	}
	if after := len(s.runs(t)); after != before {
		t.Fatalf("runs = %d, want %d", after, before)
	}
	shown := false
	for _, message := range sessionMessages(t, s.db, s.session.ID) {
		shown = shown || message.Role == transcript.MessageRoleSystem && strings.Contains(message.Content, "waits for your message")
	}
	if !shown {
		t.Fatal("the session shows no notice")
	}
}

func TestStoppedTasksDoNotWakeAnIdleSession(t *testing.T) {
	t.Parallel()
	s := newWakeScenario(t)
	s.userRun(t)
	before := len(s.runs(t))
	taskID := s.runTask(t, "sleep 30")

	if _, err := s.app.CancelTask(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	waitTaskStatus(t, s.db, taskID, core.TaskStatusCanceled)
	time.Sleep(100 * time.Millisecond)

	if after := len(s.runs(t)); after != before {
		t.Fatalf("a stopped task started a run: %d runs, want %d", after, before)
	}
	events, err := s.db.ListTasks(context.Background(), core.TaskFilter{SessionID: s.session.ID, Undelivered: true})
	if err != nil || len(events) != 1 {
		t.Fatalf("the next run should still read the task: %+v, %v", events, err)
	}
}

func TestMessageToABusySessionSteersByDefault(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	session, _ := saveCrashRecoveryRun(t, db, "busy", core.RunStatusRunning, false)

	accepted, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Text: "also check the logs"})

	if err != nil || accepted.Status != core.AcceptRunStatusSteered || accepted.Input == nil || accepted.Input.Mode != core.BusyInputModeSteer {
		t.Fatalf("accepted = %+v, %v", accepted, err)
	}
}
```

In `internal/store/sqlite_tasks_test.go`, replace `func TestSubagentTasksMoveIntoTheTasksTable` with:

```go
func TestSubagentTasksMoveIntoTheTasksTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "s1")
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE subagent_tasks (id TEXT PRIMARY KEY, mode TEXT NOT NULL DEFAULT 'blocking', parent_session_id TEXT NOT NULL,
			parent_run_id TEXT NOT NULL DEFAULT '', parent_tool_call_id TEXT NOT NULL DEFAULT '', child_session_id TEXT NOT NULL DEFAULT '',
			child_run_id TEXT NOT NULL DEFAULT '', runtime TEXT NOT NULL, goal TEXT NOT NULL, status TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', completion_queued_at TEXT, completion_delivered_at TEXT,
			completion_auto_resume_run_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, finished_at TEXT)`,
		`INSERT INTO subagent_tasks(id, mode, parent_session_id, parent_run_id, parent_tool_call_id, child_run_id, runtime, goal, status, summary,
			completion_queued_at, created_at, updated_at, finished_at)
			VALUES('queued', 'async', 's1', 'r1', 'call_1', 'child_1', 'matrixclaw', 'Read the logs', 'completed', 'All good',
			'2026-09-23T10:00:05Z', '2026-09-23T10:00:00Z', '2026-09-23T10:00:05Z', '2026-09-23T10:00:05Z')`,
		`INSERT INTO subagent_tasks(id, mode, parent_session_id, runtime, goal, status, completion_queued_at, completion_delivered_at,
			completion_auto_resume_run_id, created_at, updated_at, finished_at)
			VALUES('delivered', 'async', 's1', 'matrixclaw', 'Fix it', 'completed', '2026-09-23T10:00:05Z', '2026-09-23T10:00:06Z',
			'r2', '2026-09-23T10:00:00Z', '2026-09-23T10:00:06Z', '2026-09-23T10:00:05Z')`,
		`INSERT INTO subagent_tasks(id, parent_session_id, runtime, goal, status, created_at, updated_at, finished_at)
			VALUES('blocking', 's1', 'matrixclaw', 'Check', 'failed', '2026-09-23T10:00:00Z', '2026-09-23T10:00:01Z', '2026-09-23T10:00:01Z')`,
		`INSERT INTO subagent_tasks(id, mode, parent_session_id, runtime, goal, status, created_at, updated_at)
			VALUES('running', 'async', 's1', 'codex', 'Build', 'running', '2026-09-23T10:00:00Z', '2026-09-23T10:00:00Z')`,
		`INSERT INTO subagent_tasks(id, parent_session_id, runtime, goal, status, created_at, updated_at)
			VALUES('orphan', 'gone', 'matrixclaw', 'Lost', 'running', '2026-09-23T10:00:00Z', '2026-09-23T10:00:00Z')`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	for reopen := 0; reopen < 2; reopen++ {
		st := openTestStore(t, path)
		pending, err := st.ListTasks(ctx, core.TaskFilter{SessionID: "s1", Undelivered: true})
		if err != nil || len(pending) != 1 || pending[0].ID != "queued" {
			t.Fatalf("open %d: pending = %+v, %v", reopen, pending, err)
		}
		queued, err := st.GetSubagentTask(ctx, "queued")
		if err != nil {
			t.Fatal(err)
		}
		if queued.Mode != core.SubagentTaskModeAsync || queued.ParentRunID != "r1" || queued.ParentToolCallID != "call_1" ||
			queued.ChildRunID != "child_1" || queued.Goal != "Read the logs" || queued.Summary != "All good" || queued.Isolation != core.SubagentIsolationShared {
			t.Fatalf("open %d: queued = %+v", reopen, queued)
		}
		delivered, err := st.GetSubagentTask(ctx, "delivered")
		if err != nil || delivered.DeliveredAt == nil || delivered.DeliveredRunID != "r2" {
			t.Fatalf("open %d: delivered = %+v, %v", reopen, delivered, err)
		}
		blocking, err := st.GetSubagentTask(ctx, "blocking")
		if err != nil || blocking.Mode != core.SubagentTaskModeBlocking || blocking.DeliveredAt == nil {
			t.Fatalf("open %d: blocking = %+v, %v", reopen, blocking, err)
		}
		active, err := st.ListActiveSubagentTasksByParent(ctx, "s1")
		if err != nil || len(active) != 1 || active[0].ID != "running" || active[0].DeliveredAt != nil {
			t.Fatalf("open %d: active = %+v, %v", reopen, active, err)
		}
		if _, err := st.GetSubagentTask(ctx, "orphan"); err != core.ErrNotFound {
			t.Fatalf("open %d: orphan err = %v", reopen, err)
		}
		_ = st.Close()
	}

	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = check.Close() }()
	var tables int
	if err := check.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'subagent_tasks'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("subagent_tasks still exists after the migration")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./clients/telegram -run '^(TestNoticeDeliverySendsItsTextOnce)$'
go test ./internal/core -run '^(TestFinishedTaskWakesAnIdleSessionWhereTheUserWrote|TestWakeChainStopsAfterTwentyRunsWithoutTheUser|TestStoppedTasksDoNotWakeAnIdleSession|TestMessageToABusySessionSteersByDefault)$'
go test ./internal/store -run '^(TestSubagentTasksMoveIntoTheTasksTable)$'
```

Expected: build fails (`core.RunTriggerWake` users, `ClientDeliveryTypeNotice`, `deliverPendingNotices` undefined).

- [ ] **Step 3: Implement**

In `clients/telegram/delivery.go`, add:

```go
func (w *Worker) deliverPendingNotices(ctx context.Context) error {
	return w.deliverPendingDeliveries(ctx, core.ClientDeliveryTypeNotice, core.ClientDeliveryFilter{})
}
```

In `clients/telegram/delivery.go`, add:

```go
// deliverNotice sends a notice's text to its chat.
func (w *Worker) deliverNotice(ctx context.Context, daemon *daemonclient.Client, delivery core.ClientDelivery) error {
	target, ok := targetFromClientDelivery(delivery)
	if !ok || strings.TrimSpace(delivery.Summary) == "" {
		return daemon.AcknowledgeClientDelivery(ctx, delivery.ID)
	}
	if err := w.sendText(ctx, target, delivery.Summary); err != nil {
		return err
	}
	return w.acknowledgeSentDelivery(ctx, daemon, delivery.ID)
}
```

In `clients/telegram/delivery_queue.go`, replace `func (Worker) deliverPendingDeliveries` with:

```go
func (w *Worker) deliverPendingDeliveries(ctx context.Context, deliveryType string, filter core.ClientDeliveryFilter) error {
	w.delivery.Lock()
	defer w.delivery.Unlock()

	ctx = context.WithValue(ctx, telegramDeferredRetryKey{}, true)
	daemon := w.daemon("")
	filter.Client = w.config.ClientName
	filter.Type = deliveryType
	filter.Status = core.ClientDeliveryStatusPending
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	deliveries, err := daemon.ListClientDeliveries(ctx, filter)
	if err != nil {
		return err
	}
	now := w.nowUTC()
	if w.deliveryRetryAt == nil {
		w.deliveryRetryAt = make(map[string]time.Time)
	}
	for key, deadline := range w.deliveryRetryAt {
		if !now.Before(deadline) {
			delete(w.deliveryRetryAt, key)
		}
	}
	// An acknowledgement may succeed remotely while its response is lost.
	// Keep local receipts bounded even if those deliveries disappear from polling.
	for id, sentAt := range w.deliveryReceipts {
		if now.Sub(sentAt) >= 24*time.Hour {
			delete(w.deliveryReceipts, id)
		}
	}
	var pendingErr error
	for _, delivery := range deliveries {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := deliveryRetryKey(delivery)
		if w.nowUTC().Before(w.deliveryRetryAt[key]) {
			continue
		}
		if _, sent := w.deliveryReceipts[delivery.ID]; sent {
			err = w.acknowledgeSentDelivery(ctx, daemon, delivery.ID)
			if err == nil && delivery.Type == core.ClientDeliveryTypeRun {
				if target, ok := targetFromClientDelivery(delivery); ok {
					w.clearRunRenderState(target.externalKey, delivery.RunID)
				}
			}
		} else if deliveryType == core.ClientDeliveryTypeDocument {
			err = w.deliverDocument(ctx, delivery)
		} else if deliveryType == core.ClientDeliveryTypeNotice {
			err = w.deliverNotice(ctx, daemon, delivery)
		} else {
			err = w.deliverPendingRunDelivery(ctx, daemon, delivery)
		}
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, sent := w.deliveryReceipts[delivery.ID]
		if retryableDeliveryError(err) || sent {
			delay := telegramRetryAfter(err)
			if delay <= 0 {
				delay = w.config.PollRetryDelay
				if delay <= 0 {
					delay = 2 * time.Second
				}
			}
			w.deliveryRetryAt[key] = w.nowUTC().Add(delay)
		} else {
			if failErr := daemon.FailClientDelivery(ctx, delivery.ID, err.Error()); failErr != nil {
				err = errors.Join(err, failErr)
			}
		}
		pendingErr = errors.Join(pendingErr, fmt.Errorf("delivery %s: %w", delivery.ID, err))
	}
	return pendingErr
}
```

In `clients/telegram/worker.go`, replace `func (Worker) runDeliveryLoop` with:

```go
func (w *Worker) runDeliveryLoop(ctx context.Context) error {
	interval := w.config.StreamFlushInterval
	if interval <= 0 {
		interval = defaultStreamFlushInterval
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		if err := w.deliverPendingRuns(ctx); err != nil && ctx.Err() == nil {
			log.Printf("telegram: run delivery failed: %v", err)
		}
		if err := w.deliverPendingDocuments(ctx); err != nil && ctx.Err() == nil {
			log.Printf("telegram: document delivery failed: %v", err)
		}
		if err := w.deliverPendingNotices(ctx); err != nil && ctx.Err() == nil {
			log.Printf("telegram: notice delivery failed: %v", err)
		}
		timer.Reset(interval)
	}
}
```

In `internal/core/deliveries.go`, replace `const block starting with ClientDeliveryTypeDaemonRestart` with:

```go
const (
	ClientDeliveryTypeDaemonRestart = "daemon_restart"
	ClientDeliveryTypeRun           = "run"
	ClientDeliveryTypeDocument      = "document"
	// ClientDeliveryTypeNotice is a short text for the user outside any run.
	ClientDeliveryTypeNotice = "notice"
)
```

In `internal/core/ports.go`, replace `type SubagentTaskStore` with:

```go
type SubagentTaskStore interface {
	CreateSubagentTask(ctx context.Context, task SubagentTask) error
	UpdateSubagentTask(ctx context.Context, task SubagentTask) error
	GetSubagentTask(ctx context.Context, taskID string) (SubagentTask, error)
	GetSubagentTaskByParentToolCall(ctx context.Context, parentSessionID string, parentRunID string, parentToolCallID string) (SubagentTask, error)
	GetSubagentTaskByChildRun(ctx context.Context, childRunID string) (SubagentTask, error)
	ListSubagentTasks(ctx context.Context, filter SubagentTaskFilter) ([]SubagentTask, error)
	ListActiveSubagentTasksByParent(ctx context.Context, parentSessionID string) ([]SubagentTask, error)
}
```

In `internal/core/run_continue.go`, replace `func (Core) createContinueRun` with:

```go
func (c *Core) createContinueRun(ctx context.Context, session Session, input HandleMessageInput) (AcceptRunResult, error) {
	if _, err := c.store.GetActiveRunBySession(ctx, session.ID); err == nil {
		return AcceptRunResult{}, fmt.Errorf("%w: wait for the current run to finish before continuing", ErrRunActive)
	} else if !errors.Is(err, ErrNotFound) {
		return AcceptRunResult{}, err
	}
	latest, err := c.store.GetLatestRunBySession(ctx, session.ID)
	if errors.Is(err, ErrNotFound) {
		return AcceptRunResult{}, fmt.Errorf("%w: the session has no run to continue", ErrInvalidInput)
	}
	if err != nil {
		return AcceptRunResult{}, err
	}
	parts := transcript.NormalizeMessageParts(continueRunText, nil)
	return c.createAcceptedRun(ctx, session, continueRunText, parts, input.Client, input.ExternalKey, input.ClientCapabilities, input.DeliveryAddress, latest.ID, "")
}
```

In `internal/core/run_wakeups.go`, delete `func (Core) wakeSessionRun`.

In `internal/core/runs.go`, replace `func (Core) AcceptRun` with:

```go
func (c *Core) AcceptRun(ctx context.Context, input HandleMessageInput) (AcceptRunResult, error) {
	if input.Continue {
		return c.acceptContinueRun(ctx, input)
	}
	text := normalizeText(input.Text)
	parts := transcript.NormalizeMessageParts(text, input.Parts)
	if text == "" && !messagePartsHaveUserContent(parts) {
		return AcceptRunResult{}, fmt.Errorf("%w: message text is required", ErrInvalidInput)
	}

	session, err := c.resolveSession(ctx, input)
	if err != nil {
		return AcceptRunResult{}, err
	}

	var result AcceptRunResult
	var startRunID string
	var interruptRunID string
	var wakeRunID string
	gate := c.sessionGate(session.ID)
	gate.Lock()
	active, err := c.store.GetActiveRunBySession(ctx, session.ID)
	switch {
	case err == nil:
		pending, createErr := c.createPendingSessionInput(ctx, session, active, input, text, parts)
		if createErr != nil {
			gate.Unlock()
			return AcceptRunResult{}, createErr
		}
		result = AcceptRunResult{
			SessionID: session.ID,
			Status:    acceptRunStatusForInputMode(pending.Mode),
			Input:     &pending,
		}
		switch {
		case pending.Mode == BusyInputModeInterrupt:
			interruptRunID = active.ID
		case pending.Mode == BusyInputModeSteer && active.Status == RunStatusWaitingEvents:
			wakeRunID = active.ID
		}
	case errors.Is(err, ErrNotFound):
		result, err = c.createAcceptedRun(ctx, session, text, parts, input.Client, input.ExternalKey, input.ClientCapabilities, input.DeliveryAddress, "", "")
		if err != nil {
			gate.Unlock()
			return AcceptRunResult{}, err
		}
		startRunID = result.Run.ID
	default:
		gate.Unlock()
		return AcceptRunResult{}, err
	}
	gate.Unlock()

	if wakeRunID != "" {
		if err := c.wakeWaitingRun(ctx, session.ID, wakeRunID); err != nil {
			return result, err
		}
	}
	if interruptRunID != "" {
		if _, err := c.CancelRun(ctx, interruptRunID); err != nil {
			return result, err
		}
		if _, err := c.startNextPendingSessionInput(ctx, session.ID); err != nil {
			return result, err
		}
	}

	// The daemon hands execution off here; transport is already out of the picture.
	if startRunID != "" {
		if err := c.startRun(ctx, startRunID); err != nil {
			return c.failAcceptedRun(ctx, result, err)
		}
	}

	return result, nil
}
```

In `internal/core/session_inputs.go`, replace `func (Core) createAcceptedRun` with:

```go
func (c *Core) createAcceptedRun(ctx context.Context, session Session, text string, parts []transcript.MessagePart, client string, externalKey string, capabilities ClientCapabilities, deliveryAddress json.RawMessage, continuesRunID string, trigger RunTrigger) (AcceptRunResult, error) {
	autoTitle := c.firstMessageAutoTitle(ctx, session, text)
	now := c.now().UTC()
	runID := c.newID("run")
	messageID := c.newID("msg")

	message := transcript.Message{
		ID:        messageID,
		SessionID: session.ID,
		RunID:     runID,
		Role:      transcript.MessageRoleUser,
		Content:   text,
		Parts:     parts,
		CreatedAt: now,
		UpdatedAt: now,
	}
	run := Run{
		ID:                 runID,
		SessionID:          session.ID,
		UserMessageID:      messageID,
		Client:             normalizeText(client),
		ExternalKey:        normalizeText(externalKey),
		ClientCapabilities: capabilities,
		ContinuesRunID:     normalizeText(continuesRunID),
		Trigger:            trigger,
		Status:             RunStatusAccepted,
		StartedAt:          now,
		UpdatedAt:          now,
	}
	delivery, hasDelivery, err := c.prepareSessionRunDelivery(run, text, parts, client, externalKey, deliveryAddress)
	if err != nil {
		return AcceptRunResult{}, err
	}
	var deliveries []ClientDelivery
	if hasDelivery {
		deliveries = append(deliveries, delivery)
	}
	if err := c.store.AcceptMessage(ctx, message, run, deliveries...); err != nil {
		return AcceptRunResult{}, err
	}
	c.applyAutoSessionTitle(ctx, session, autoTitle)
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: session.ID, RunID: run.ID, Payload: message})
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: session.ID, RunID: run.ID, Payload: run})
	return AcceptRunResult{
		SessionID:   session.ID,
		Status:      AcceptRunStatusStarted,
		UserMessage: message,
		Run:         run,
	}, nil
}
```

In `internal/core/session_inputs.go`, replace `func (Core) consumeSessionInputAsRun` with:

```go
func (c *Core) consumeSessionInputAsRun(ctx context.Context, session Session, input SessionInput) (AcceptRunResult, error) {
	parts := transcript.NormalizeMessageParts(input.Text, input.Parts)
	result, err := c.createAcceptedRun(ctx, session, input.Text, parts, input.Client, input.ExternalKey, input.ClientCapabilities, input.DeliveryAddress, "", "")
	if err != nil {
		return AcceptRunResult{}, err
	}
	now := c.now().UTC()
	input.Status = SessionInputStatusConsumed
	input.ConsumedRunID = result.Run.ID
	input.ConsumedAt = &now
	input.UpdatedAt = now
	if err := c.store.UpdateSessionInput(ctx, input); err != nil {
		return AcceptRunResult{}, err
	}
	result.Input = &input
	c.publishSessionInputUpdated(input)
	return result, nil
}
```

In `internal/core/shell_tasks.go`, replace `func (Core) taskFinished` with:

```go
// taskFinished lets the task's session react: a run waiting for events wakes,
// a running run reads the event at its next step, an idle session starts a run.
func (c *Core) taskFinished(ctx context.Context, task Task) {
	if !task.Background || task.DeliveredAt != nil {
		return
	}
	active, err := c.store.GetActiveRunBySession(ctx, task.SessionID)
	switch {
	case errors.Is(err, ErrNotFound):
		err = c.wakeSession(ctx, task.SessionID, &task)
	case err == nil && active.Status == RunStatusWaitingEvents:
		err = c.wakeWaitingRun(ctx, task.SessionID, active.ID)
	}
	if err != nil {
		log.Printf("core: tell session %q that task %q finished: %v", task.SessionID, task.ID, err)
	}
}
```

In `internal/core/subagents_lifecycle.go`, the imports become:

```go
import (
	"context"
	"errors"
	"time"
)
```

In `internal/core/subagents_lifecycle.go`, replace `func (Core) afterRunExecution` with:

```go
func (c *Core) afterRunExecution(ctx context.Context, runID string) error {
	runID = normalizeText(runID)
	if runID == "" || c == nil || c.store == nil {
		return nil
	}
	run, err := c.store.GetRun(ctx, runID)
	if err != nil {
		if ignoreMissing(err) {
			return nil
		}
		return err
	}
	if task, err := c.store.GetSubagentTaskByChildRun(ctx, runID); err == nil {
		switch task.Mode {
		case SubagentTaskModeAsync:
			if syncErr := c.syncAsyncSubagentTaskAfterRun(ctx, task, run); syncErr != nil {
				return syncErr
			}
		case SubagentTaskModeBlocking:
			if syncErr := c.syncBlockingSubagentTaskAfterRun(ctx, task, run); syncErr != nil {
				return syncErr
			}
		}
	} else if !ignoreMissing(err) {
		return err
	}
	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		if ignoreMissing(err) {
			return nil
		}
		return err
	}
	if subagentRunStatusTerminal(run.Status) {
		if err := c.queuePendingSteersForRun(ctx, session.ID, run.ID); err != nil {
			return err
		}
		startedInput, err := c.startNextPendingSessionInput(ctx, session.ID)
		if err != nil || startedInput {
			return err
		}
	}
	if subagentRunStatusTerminal(run.Status) {
		return c.wakeSession(ctx, session.ID, nil)
	}
	return nil
}
```

In `internal/core/subagents_lifecycle.go`, replace `func (Core) syncAsyncSubagentTaskAfterRun` with:

```go
func (c *Core) syncAsyncSubagentTaskAfterRun(ctx context.Context, task SubagentTask, run Run) error {
	if taskStatusTerminal(task.Status) {
		return nil
	}
	switch run.Status {
	case RunStatusWaitingApproval:
		_, err := c.mirrorPendingSubagentApproval(ctx, task)
		return err
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
	default:
		return nil
	}
	summary, failed := c.subagentRunSummary(ctx, task.ChildSessionID, task.ChildRunID, nil)
	status := TaskStatusCompleted
	errText := ""
	if run.Status == RunStatusCanceled {
		status = TaskStatusCanceled
		errText = summary
	} else if failed {
		status = TaskStatusFailed
		errText = summary
	}
	task, err := c.finishSubagentTaskRecord(ctx, task, status, summary, errText, true)
	if err != nil {
		return err
	}
	c.publishSubagentToolUpdate(task)
	finished, err := c.store.GetTask(ctx, task.ID)
	if err != nil {
		return err
	}
	c.taskFinished(ctx, finished)
	return nil
}
```

In `internal/core/subagents_lifecycle.go`, replace `func (Core) RecoverSubagentTasks` with:

```go
func (c *Core) RecoverSubagentTasks(ctx context.Context) error {
	active, err := c.store.ListSubagentTasks(ctx, SubagentTaskFilter{
		Statuses: activeTaskStatuses(),
		Limit:    200,
	})
	if err != nil {
		return err
	}
	for _, task := range active {
		run, err := c.store.GetRun(ctx, task.ChildRunID)
		if err != nil {
			continue
		}
		switch task.Mode {
		case SubagentTaskModeAsync:
			if subagentRunStatusTerminal(run.Status) || run.Status == RunStatusWaitingApproval {
				if err := c.syncAsyncSubagentTaskAfterRun(ctx, task, run); err != nil {
					return err
				}
				continue
			}
			if err := c.startRun(ctx, task.ChildRunID); err != nil {
				return err
			}
		case SubagentTaskModeBlocking:
			if subagentRunStatusTerminal(run.Status) || run.Status == RunStatusWaitingApproval {
				if err := c.syncBlockingSubagentTaskAfterRun(ctx, task, run); err != nil {
					return err
				}
				continue
			}
			if run.Status == RunStatusAccepted {
				if err := c.startRun(ctx, task.ChildRunID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
```

In `internal/core/subagents_lifecycle.go`, delete `func (Core) deliverPendingSubagentCompletionsForParent`.

In `internal/core/subagents_lifecycle.go`, delete `func (Core) parentReadyForSubagentAutoResume`.

In `internal/core/subagents_persistence.go`, delete `func (Core) markSubagentCompletionDelivered`.

In `internal/core/subagents_presentation.go`, delete `func subagentCompletionPrompt`.

In `internal/core/subagents_presentation.go`, delete `func subagentCompletionTriggerID`.

Create `internal/core/task_wake.go`:

```go
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// maxWakeChain is how many runs in a row a session starts for finished
// background work without a user message; after that it only tells the user.
const maxWakeChain = 20

// wakeRunText is the user message of a run started for finished background work;
// the tasks' results follow it as engine notes.
const wakeRunText = "Background work finished while you were idle; its results follow. Carry on with what it was for, or report what happened."

// wakeSession starts a run in an idle session for background tasks that
// finished unseen, delivered where the user last wrote from. Past the wake
// chain limit it only tells the user about finished, when given.
func (c *Core) wakeSession(ctx context.Context, sessionID string, finished *Task) error {
	gate := c.sessionGate(sessionID)
	gate.Lock()
	result, notice, err := c.prepareWake(ctx, sessionID, finished)
	gate.Unlock()
	switch {
	case err != nil:
		return err
	case notice != nil:
		return c.sendWakeNotice(ctx, *notice)
	case result == nil:
		return nil
	}
	if err := c.startRun(ctx, result.Run.ID); err != nil {
		_, err = c.failAcceptedRun(ctx, *result, err)
		return err
	}
	return nil
}

// wakeNotice tells the user that background work finished while the session
// may not wake itself.
type wakeNotice struct {
	session Session
	last    Run
	text    string
}

// prepareWake creates the wake run under the session gate: only for an idle,
// top-level session with a task whose end should wake it.
func (c *Core) prepareWake(ctx context.Context, sessionID string, finished *Task) (*AcceptRunResult, *wakeNotice, error) {
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil || isSubagentSession(session) {
		return nil, nil, err
	}
	if _, err := c.store.GetActiveRunBySession(ctx, session.ID); !errors.Is(err, ErrNotFound) {
		return nil, nil, err
	}
	inputs, err := c.store.ListPendingSessionInputs(ctx, session.ID)
	if err != nil || len(inputs) > 0 {
		return nil, nil, err
	}
	events, err := c.store.ListTasks(ctx, TaskFilter{SessionID: session.ID, Undelivered: true})
	if err != nil {
		return nil, nil, err
	}
	wakes := false
	for _, task := range events {
		wakes = wakes || taskWakesSession(task)
	}
	if !wakes {
		return nil, nil, nil
	}
	runs, err := c.store.ListSessionRuns(ctx, session.ID, maxWakeChain+1)
	if err != nil {
		return nil, nil, err
	}
	chain := 0
	for chain < len(runs) && runs[chain].Trigger == RunTriggerWake {
		chain++
	}
	var last Run
	if chain < len(runs) {
		last = runs[chain]
	}
	if chain >= maxWakeChain {
		if finished == nil || !taskWakesSession(*finished) {
			return nil, nil, nil
		}
		text := fmt.Sprintf("%s finished. This session has continued on its own %d times in a row, so it waits for your message before it goes on.", taskLabel(*finished), maxWakeChain)
		return nil, &wakeNotice{session: session, last: last, text: text}, nil
	}
	address, err := c.runDeliveryAddress(ctx, last)
	if err != nil {
		return nil, nil, err
	}
	parts := transcript.NormalizeMessageParts(wakeRunText, nil)
	result, err := c.createAcceptedRun(ctx, session, wakeRunText, parts, last.Client, last.ExternalKey, last.ClientCapabilities, address, "", RunTriggerWake)
	if err != nil {
		return nil, nil, err
	}
	return &result, nil, nil
}

// taskWakesSession reports whether a finished task starts a run: a command
// that ended by itself or a subagent; a stopped or lost command waits for the
// session's next run.
func taskWakesSession(task Task) bool {
	if task.Kind == TaskKindSubagent {
		return true
	}
	return task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed
}

func taskLabel(task Task) string {
	if task.Kind == TaskKindSubagent {
		return "Subagent " + firstNonEmpty(task.AgentName, task.ID)
	}
	return "Background task " + task.ID + " (" + truncateForTitle(task.Command, 80) + ")"
}

// runDeliveryAddress is where the run's reply was delivered; none for a run
// without a client delivery.
func (c *Core) runDeliveryAddress(ctx context.Context, run Run) (json.RawMessage, error) {
	if run.ID == "" || run.Client == "" || run.ExternalKey == "" {
		return nil, nil
	}
	deliveries, err := c.store.ListClientDeliveries(ctx, ClientDeliveryFilter{RunID: run.ID, Type: ClientDeliveryTypeRun, Limit: 1})
	if err != nil || len(deliveries) == 0 {
		return nil, err
	}
	return deliveries[0].Address, nil
}

// sendWakeNotice shows the notice in the session and sends it where the user
// last wrote from.
func (c *Core) sendWakeNotice(ctx context.Context, notice wakeNotice) error {
	if _, err := c.CreateSystemMessage(ctx, notice.session.ID, notice.text); err != nil {
		return err
	}
	if notice.last.Client == "" || notice.last.ExternalKey == "" {
		return nil
	}
	address, err := c.runDeliveryAddress(ctx, notice.last)
	if err != nil {
		return err
	}
	_, err = c.CreateClientDelivery(ctx, ClientDelivery{
		Type:        ClientDeliveryTypeNotice,
		Client:      notice.last.Client,
		ExternalKey: notice.last.ExternalKey,
		SessionID:   notice.session.ID,
		Summary:     notice.text,
		Address:     address,
	})
	return err
}

// RecoverTaskEvents starts the runs that idle sessions owe to background work
// finished before the daemon restarted.
func (c *Core) RecoverTaskEvents(ctx context.Context) error {
	events, err := c.store.ListTasks(ctx, TaskFilter{Undelivered: true})
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var errs []error
	for _, task := range events {
		if seen[task.SessionID] {
			continue
		}
		seen[task.SessionID] = true
		errs = append(errs, c.wakeSession(ctx, task.SessionID, nil))
	}
	return errors.Join(errs...)
}
```

In `internal/daemoncmd/run.go`, replace `func Run` with:

```go
func Run(ctx context.Context) error {
	bootstrap, err := loadBootstrap()
	if err != nil {
		return err
	}

	sqliteStore, err := store.NewSQLite(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = sqliteStore.Close() }()
	automationStore, err := automation.NewSQLiteStore(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = automationStore.Close() }()
	workStore, err := work.NewSQLiteStore(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = workStore.Close() }()
	webResearchStore := webresearch.NewStore(workStore)

	storageModule, err := localstorage.New(localstorage.Config{
		Root: defaultStorageRoot(bootstrap.DBPath),
	})
	if err != nil {
		return err
	}
	mcpModule, err := mcpmodule.New(ctx, mcpConfigWithBrowser(bootstrap.ExternalAgents))
	if err != nil {
		log.Printf("matrixclawd mcp module disabled: %v", err)
		mcpModule, _ = mcpmodule.New(ctx, setup.MCPConfig{})
	}
	defer func() { _ = mcpModule.Close() }()
	skillsModule, err := skillsmodule.New(skillsConfigFromBootstrap(bootstrap))
	if err != nil {
		return err
	}
	defer func() { _ = skillsModule.Close() }()
	moduleRegistry := modules.NewRegistry(storageModule, mcpModule, skillsModule)

	app := core.New(sqliteStore).
		WithSessionLLMs(bootstrap.SessionLLMs).
		WithRunBudgets(bootstrap.Budgets).
		WithSessionFiles(sessionFilesRoot(bootstrap.DBPath)).
		WithCompactModel(bootstrap.CompactModel.Provider, bootstrap.CompactModel.Model).
		WithContextWindowCap(bootstrap.WindowCap).
		WithModelConcurrency(bootstrap.ModelConcurrency).
		WithWorkStore(workStore).
		WithAttachmentReader(storageAttachmentReader{store: storageModule.Store()}).
		WithSkillsContext(skillsModule).
		WithRuntimeStatusContext(&setupRuntimeStatusContext{setup: bootstrap.SetupService, runtime: localruntime.New("")})
	// The workflow worker (below) may start executing persisted runs before
	// supervisor.ApplyBootstrap runs, so the profile is set here too, via the
	// same helper, ensuring module context is never missing.
	applyAssistantProfile(app, bootstrap.Assistant, moduleRegistry.Context)
	externalRegistry, externalRuntimes, err := builtins.BuildRegistry(bootstrap.ExternalAgents)
	if err != nil {
		return err
	}
	app.WithExternalAgents(externalRegistry, sqliteStore)
	automationService := automation.NewService(automationStore, app, bootstrap.Timezone).
		WithDeliveryTargets(automationDeliveryTargets(bootstrap))
	webSearchConfig := webSearchProviderConfig(bootstrap.SetupService)
	webResearchEngine := newWebResearchEngine(bootstrap.DBPath, bootstrap.ExternalAgents.MCP, mcpModule, webResearchStore, webSearchConfig)
	webTools := webtools.NewWebService(webSearchConfig, webResearchEngine)
	osmGeo := tools.NewOSMServiceFromEnv()
	extraTools := []tools.Executor{
		automation.NewReminderTool(automationService),
		automation.NewScheduledAITaskTool(automationService),
		deliverymodule.NewSendFileTool(storageModule.Store(), app),
		telephonymodule.NewCallTool(bootstrap.SetupService),
		telephonymodule.NewEndCallTool(bootstrap.SetupService),
		voicemodule.NewTextToSpeechTool(bootstrap.SetupService),
		webtools.NewWebFetchExecutorWithService(webTools),
		webtools.NewWebSearchExecutorWithService(webTools),
	}
	extraTools = append(extraTools, webtools.NewWebResearchExecutorsWithService(webTools)...)
	toolRegistry := tools.NewCoreCodingRegistry(extraTools...)
	if err := toolRegistry.Register(tools.NewShellExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.TodoToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.AwaitToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.MemoryToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.SubagentToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Err(); err != nil {
		return err
	}
	if err := toolRegistry.Register(tools.NewOSMGeoExecutors(osmGeo)...); err != nil {
		return err
	}
	if err := moduleRegistry.RegisterTools(toolRegistry); err != nil {
		return err
	}
	app.WithTools(newSetupAwareToolExecutor(toolRegistry, bootstrap.SetupService))
	// Shell tasks do not survive a restart; this runs before any run can start new ones.
	if err := app.RecoverTasks(ctx); err != nil {
		log.Printf("matrixclawd background task recovery failed: %v", err)
	}
	// The workflow worker executes persisted runs as soon as it starts, so it
	// starts only once the core is fully wired and before anything accepts runs.
	lifetime, stopLifetime := context.WithCancel(ctx)
	defer stopLifetime()
	app.WithLifetime(lifetime)
	runStarter, err := goworkflows.NewForStore(bootstrap.DBPath, app)
	if err != nil {
		return err
	}
	defer func() {
		// End the lifetime first so runs interrupted by closing the worker are not rescheduled.
		stopLifetime()
		_ = runStarter.Close()
	}()
	app.WithRunStarter(runStarter)
	safego.Go("core.runWakeups", func() { app.RunWakeups(lifetime) })
	server := api.New(app)
	server.SetAPIToken(bootstrap.APIToken)
	server.SetAutomationService(automationService)
	server.SetStorageStore(storageModule.Store())
	server.SetSkillsService(skillsModule.Service())
	server.SetSetupService(bootstrap.SetupService)
	server.SetRealtimeVoiceService(newRealtimeVoiceManager(bootstrap.SetupService, app))
	supervisor := newSupervisor(ctx, server, app, osmGeo)
	supervisor.SetModuleContext(moduleRegistry.Context)
	supervisor.SetExternalAgents(sqliteStore, externalRuntimes)
	defer supervisor.CloseExternalAgents()
	httpServer := &http.Server{
		Addr:              bootstrap.Addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 2)
	safego.Go("daemon.httpServer", func() {
		err := httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	})

	if err := supervisor.ApplyBootstrap(bootstrap); err != nil {
		return err
	}
	startConfiguredVoiceRuntimes(ctx, bootstrap.SetupService)
	safego.Go("automation.Run", func() { automationService.Run(ctx) })
	safego.Go("webresearch.Run", func() { webResearchEngine.Start(ctx) })
	safego.Go("supervisor.deliverStartupNotifications", func() {
		supervisor.DeliverPendingStartupNotifications(bootstrap)
	})
	safego.Go("core.recoverState", func() {
		if err := app.RecoverActiveRuns(context.Background()); err != nil {
			log.Printf("matrixclawd active run recovery failed: %v", err)
		}
		if err := app.RecoverSessionInputs(context.Background()); err != nil {
			log.Printf("matrixclawd session input recovery failed: %v", err)
		}
		if err := app.RecoverSubagentTasks(context.Background()); err != nil {
			log.Printf("matrixclawd subagent recovery failed: %v", err)
		}
		if err := app.RecoverTaskEvents(context.Background()); err != nil {
			log.Printf("matrixclawd background task event recovery failed: %v", err)
		}
	})

	log.Printf("matrixclawd bootstrap: setup=%s", bootstrap.SetupPath)
	log.Printf("matrixclawd listening on %s using %s", bootstrap.Addr, bootstrap.DBPath)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
```

In `internal/store/sqlite_subagents.go`, delete `func (SQLiteStore) ListPendingSubagentCompletionTasks`.

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./clients/telegram -run '^(TestNoticeDeliverySendsItsTextOnce)$'
go test ./internal/core -run '^(TestFinishedTaskWakesAnIdleSessionWhereTheUserWrote|TestWakeChainStopsAfterTwentyRunsWithoutTheUser|TestStoppedTasksDoNotWakeAnIdleSession|TestMessageToABusySessionSteersByDefault)$'
go test ./internal/store -run '^(TestSubagentTasksMoveIntoTheTasksTable)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add clients/telegram/delivery.go \
  clients/telegram/delivery_queue.go \
  clients/telegram/delivery_queue_test.go \
  clients/telegram/worker.go \
  internal/core/deliveries.go \
  internal/core/native_run_characterization_test.go \
  internal/core/ports.go \
  internal/core/run_continue.go \
  internal/core/run_wakeups.go \
  internal/core/runs.go \
  internal/core/session_inputs.go \
  internal/core/shell_tasks.go \
  internal/core/subagents_lifecycle.go \
  internal/core/subagents_persistence.go \
  internal/core/subagents_presentation.go \
  internal/core/task_wake.go \
  internal/core/task_wake_test.go \
  internal/daemoncmd/run.go \
  internal/store/sqlite_subagents.go \
  internal/store/sqlite_tasks_test.go
git commit -m "feat(core): finished background work wakes idle sessions, up to 20 runs in a row

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 6: Prompt guidance and spec notes

**Files:**
- Modify: `docs/superpowers/specs/2026-09-23-long-running-agent-design.md`
- Modify: `internal/agent/prompt/guidance.go`
- Test (modify): `internal/agent/prompt/guidance_test.go`

Prompt and spec.

- [ ] **Step 1: Write the failing tests**

In `internal/agent/prompt/guidance_test.go`, replace `func TestToolUseDisciplineRunsLongCommandsInTheBackground` with:

```go
func TestToolUseDisciplineRunsLongCommandsInTheBackground(t *testing.T) {
	text := ToolUseDiscipline()
	for _, want := range []string{"run_in_background", "task_output", "task_kill", "after 2 minutes", "await instead of polling"} {
		if !strings.Contains(text, want) {
			t.Fatalf("tool-use discipline lacks %q:\n%s", want, text)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/agent/prompt -run '^(TestToolUseDisciplineRunsLongCommandsInTheBackground)$'
```

Expected: the guidance test fails: no "await instead of polling".

- [ ] **Step 3: Implement**

In `docs/superpowers/specs/2026-09-23-long-running-agent-design.md`:

```diff
diff --git a/docs/superpowers/specs/2026-09-23-long-running-agent-design.md b/docs/superpowers/specs/2026-09-23-long-running-agent-design.md
index 5dd7e08..e833ebc 100644
--- a/docs/superpowers/specs/2026-09-23-long-running-agent-design.md
+++ b/docs/superpowers/specs/2026-09-23-long-running-agent-design.md
@@ -584,6 +584,46 @@ runtime, model}`; `runtime`/`model` keep delegation to Codex and Claude Code.
   `POST /v1/tasks/{id}/cancel`. A task the user stops is an event for the
   session.
 
+### Implementation notes (as built, stage 6b)
+
+- **await** is a core tool (`await{ids?, timeout_seconds ≤ 3600, default
+  600}`, category automation, so children lack it). Its result is written at
+  once ("Waiting up to … for …"); `tools.Result.Await` puts `{task_ids, until}`
+  into `Counters.Await`, so the park survives an approval or a restart. Named
+  tasks that already finished are reported instead of waited for; with no
+  running background task it says so. It needs a run.
+- **Park**: each step starts with `resumeAwait`: it clears `Counters.Await`
+  when a steer is pending (written as a user message, since no tool result can
+  carry it), an event for an awaited task (any task without ids) is pending,
+  or `until` passed (an engine note says the await timed out); otherwise the
+  step checkpoints and the run ends `waiting_events`. Events for other tasks
+  stay pending. Core stores `run_wakeups(run_id, session_id, wake_at ms,
+  task_ids_json)` and the status; a woken run that finds nothing parks again
+  without a model call.
+- **Wake**: `wakeWaitingRun` runs under the session gate and starts the run
+  when its timer ran out, a steer for it is pending or an awaited task
+  finished unseen; it deletes the wakeup. It is called when a task finishes,
+  when a message arrives, by `WakeDueRuns` (a one-second ticker; overdue
+  timers fire on its first tick after a restart), at startup for every
+  waiting run, and after the parking run is no longer active
+  (`resumeParkedRun`), which closes the race with an event that arrived while
+  it parked. `prepareClaimedRun` accepts a waiting run.
+- **Input**: a message without a busy mode steers (TUI and Telegram send
+  none); a queued message for a waiting run steers it too.
+- **Idle sessions**: a finished background task (a command that exited by
+  itself, or any subagent) in an idle top-level session starts a run with
+  trigger `wake` (automation budget) whose user message says background work
+  finished; the events follow as notes and are delivered to it. It gets the
+  client, capabilities and delivery address of the newest run before the wake
+  chain, so Telegram receives its reply. Twenty wake runs in a row without a
+  user run stop the chain: the session shows a system message and the chat
+  gets a `notice` delivery per finished task. Stopped and lost commands wait
+  for the next run. Subagent completion runs are wake runs now; their prompt,
+  trigger IDs and `ListPendingSubagentCompletionTasks` are gone.
+- **Clients**: the TUI counts `waiting_events` as busy ("Waiting for background
+  tasks"); Telegram shows progress, no typing, and "Waiting for background
+  tasks..."; the iOS package decodes it as `.unknown`.
+
 ## 5. Providers
 
 - `providers.Request` gains `MaxOutputTokens` (priority: provider config →
```

In `internal/agent/prompt/guidance.go`, replace `func ToolUseDiscipline` with:

```go
// ToolUseDiscipline is the tool-use guidance for models that can call tools.
func ToolUseDiscipline() string {
	return strings.TrimSpace(`Tool use discipline:
- Treat requests to do work as instructions to carry the task through to a verified result. A promise, plan, or successful intermediate tool call is not completion.
- Tool calls you make in one reply run at the same time and may finish in any order; their results come back in the order you made them. Put independent reads, searches and inspections into one reply as parallel calls. A call that needs another call's result or effect goes in a later reply.
- Run commands that take minutes (builds, test suites, servers) with bash run_in_background and go on with other work; read their output with task_output and stop them with task_kill. A foreground command becomes a background task by itself after 2 minutes and is killed after its timeout (10 minutes unless you set one). You are told when a background task finishes; when nothing else is left to do, wait for it with await instead of polling.
- Inspect each tool result before deciding the next step. If a tool fails, use its error to correct the request or choose another approach; do not claim success or repeat the same failed call without a reason.
- Continue while useful authorized work remains. Ask a concise question only when missing information or permission actually blocks the next necessary step.
- Track work of three or more steps with todo_write and update it as you go: keep one item in_progress while you work on it and mark it completed as soon as it is done. Skip the list for simple requests.
- In the final reply, report what was accomplished, how it was checked (tests, build, real output), and any failure or remaining blocker honestly.
- Before calling another tool, check whether existing tool results already contain the requested answer; if they do, stop tool use and reply.
- Do not run extra searches, browser snapshots, or verification calls just to improve confidence when the answer is already clear and source-backed.
- If a result is partly useful but has minor uncertainty, answer with that uncertainty instead of repeatedly searching, unless the user asked for exhaustive verification or the sources conflict.
- For simple lookups, prefer one direct path to the answer over parallel or repeated searches.`)
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/agent/prompt -run '^(TestToolUseDisciplineRunsLongCommandsInTheBackground)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add docs/superpowers/specs/2026-09-23-long-running-agent-design.md \
  internal/agent/prompt/guidance.go \
  internal/agent/prompt/guidance_test.go
git commit -m "docs: stage 6b as built; prompt asks to await instead of polling

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 7: Verification

- [ ] **Step 1: Race and dead code**

```bash
go test -race ./internal/core ./internal/agent ./internal/store ./clients/telegram ./clients/terminal/chat/runtime
deadcode -test ./...   # compare with the output before Task 1: nothing new
```

- [ ] **Step 2: Manual check on the test stand** (never the production daemon): ask the agent to start `sleep 90; echo built` in the background and to await it — the TUI shows "Waiting for background tasks", the run resumes after ~90 s with the task's note; send a message while it waits — the run resumes at once with the message; start a background command, let the run finish, and wait — a wake run reports the result in Telegram.
