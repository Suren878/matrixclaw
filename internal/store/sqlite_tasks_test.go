package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

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
		queued, err := st.GetTask(ctx, "queued")
		if err != nil {
			t.Fatal(err)
		}
		if queued.Kind != core.TaskKindSubagent || !queued.Background || queued.RunID != "r1" || queued.ParentToolCallID != "call_1" ||
			queued.ChildRunID != "child_1" || queued.Command != "Read the logs" || queued.Summary != "All good" || queued.Isolation != core.SubagentIsolationShared {
			t.Fatalf("open %d: queued = %+v", reopen, queued)
		}
		delivered, err := st.GetTask(ctx, "delivered")
		if err != nil || delivered.DeliveredAt == nil || delivered.DeliveredRunID != "r2" {
			t.Fatalf("open %d: delivered = %+v, %v", reopen, delivered, err)
		}
		blocking, err := st.GetTask(ctx, "blocking")
		if err != nil || blocking.Background || blocking.DeliveredAt == nil {
			t.Fatalf("open %d: blocking = %+v, %v", reopen, blocking, err)
		}
		active, err := st.ListTasks(ctx, core.TaskFilter{SessionID: "s1", Kind: core.TaskKindSubagent, Background: true, Statuses: []core.TaskStatus{core.TaskStatusRunning}})
		if err != nil || len(active) != 1 || active[0].ID != "running" || active[0].DeliveredAt != nil {
			t.Fatalf("open %d: active = %+v, %v", reopen, active, err)
		}
		if _, err := st.GetTask(ctx, "orphan"); !errors.Is(err, core.ErrNotFound) {
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

func TestMarkTasksDeliveredKeepsTheFirstDelivery(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "tasks.db"))
	createTestSession(t, st, "s1")
	task := core.Task{ID: "task_1", Kind: core.TaskKindSubagent, Background: true, SessionID: "s1", Runtime: "matrixclaw", Command: "Look", Status: core.TaskStatusRunning, StartedAt: testEpoch, UpdatedAt: testEpoch}
	if err := st.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkTasksDelivered(ctx, []string{"task_1"}, "run_a", testEpoch); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkTasksDelivered(ctx, []string{"task_1"}, "run_b", testEpoch.Add(1)); err != nil {
		t.Fatal(err)
	}
	// Ending the task delivered leaves the first delivery alone.
	if _, err := st.FinishTask(ctx, "task_1", core.TaskEnd{Status: core.TaskStatusCompleted, Summary: "Seen", Delivered: true, At: testEpoch.Add(2)}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetTask(ctx, "task_1")
	if err != nil || got.DeliveredRunID != "run_a" || got.DeliveredAt == nil || !got.DeliveredAt.Equal(testEpoch) || got.Summary != "Seen" {
		t.Fatalf("task = %+v, %v", got, err)
	}
}

func TestSubagentTaskStatusChangesOnlyUntilItEnds(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "tasks.db"))
	createTestSession(t, st, "s1")
	task := core.Task{ID: "task_1", Kind: core.TaskKindSubagent, SessionID: "s1", Command: "Look", Status: core.TaskStatusRunning, StartedAt: testEpoch, UpdatedAt: testEpoch}
	if err := st.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(ctx, "task_1", core.TaskStatusWaitingApproval, testEpoch); err != nil {
		t.Fatal(err)
	}
	if got, err := st.GetTask(ctx, "task_1"); err != nil || got.Status != core.TaskStatusWaitingApproval {
		t.Fatalf("task = %+v, %v", got, err)
	}
	if _, err := st.FinishTask(ctx, "task_1", core.TaskEnd{Status: core.TaskStatusFailed, Summary: "Subagent failed: boom", Error: "boom", At: testEpoch}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(ctx, "task_1", core.TaskStatusRunning, testEpoch); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetTask(ctx, "task_1")
	if err != nil || got.Status != core.TaskStatusFailed || got.Summary != "Subagent failed: boom" || got.DeliveredAt != nil {
		t.Fatalf("ended task = %+v, %v", got, err)
	}
}

func TestShellTasksFinishOnceAndBecomeEventsUntilDelivered(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "tasks.db"))
	createTestSession(t, st, "s1")
	createTestSession(t, st, "s2")
	for _, task := range []core.Task{
		{ID: "task_a", SessionID: "s1", RunID: "r1", ParentToolCallID: "call_1", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "npm test", WorkingDir: "/work", Background: true, PID: 42, PGID: 42, OutputPath: "/data/s1/tasks/task_a.log", StartedAt: testEpoch, UpdatedAt: testEpoch},
		{ID: "task_b", SessionID: "s1", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "sleep 9", Background: true, StartedAt: testEpoch.Add(time.Second), UpdatedAt: testEpoch},
		{ID: "task_c", SessionID: "s2", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "make", Background: true, StartedAt: testEpoch, UpdatedAt: testEpoch},
	} {
		if err := st.CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.GetTask(ctx, "task_a")
	if err != nil || got.RunID != "r1" || got.ParentToolCallID != "call_1" || got.PID != 42 || got.OutputPath != "/data/s1/tasks/task_a.log" || got.ExitCode != nil || !got.Background {
		t.Fatalf("task = %+v, %v", got, err)
	}

	code := 1
	finished, err := st.FinishTask(ctx, "task_a", core.TaskEnd{Status: core.TaskStatusFailed, ExitCode: &code, At: testEpoch.Add(time.Minute)})
	if err != nil || !finished {
		t.Fatalf("finish = %v, %v", finished, err)
	}
	again, err := st.FinishTask(ctx, "task_a", core.TaskEnd{Status: core.TaskStatusCanceled, Error: "killed", At: testEpoch.Add(2 * time.Minute)})
	if err != nil || again {
		t.Fatalf("second finish = %v, %v", again, err)
	}
	if err := st.SetTaskCursor(ctx, "task_a", 128); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetTask(ctx, "task_a")
	if err != nil || got.Status != core.TaskStatusFailed || got.ExitCode == nil || *got.ExitCode != 1 || got.OutputCursor != 128 || got.FinishedAt == nil {
		t.Fatalf("finished task = %+v, %v", got, err)
	}

	running, err := st.ListTasks(ctx, core.TaskFilter{Kind: core.TaskKindShell, Statuses: []core.TaskStatus{core.TaskStatusRunning}})
	if err != nil || len(running) != 2 || running[0].ID != "task_b" || running[1].ID != "task_c" {
		t.Fatalf("running = %+v, %v", running, err)
	}
	events, err := st.ListTasks(ctx, core.TaskFilter{SessionID: "s1", Undelivered: true})
	if err != nil || len(events) != 1 || events[0].ID != "task_a" {
		t.Fatalf("events = %+v, %v", events, err)
	}
	if err := st.MarkTasksDelivered(ctx, []string{"task_a"}, "r2", testEpoch.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if events, err := st.ListTasks(ctx, core.TaskFilter{SessionID: "s1", Undelivered: true}); err != nil || len(events) != 0 {
		t.Fatalf("events after delivery = %+v, %v", events, err)
	}
	if _, err := st.GetTask(ctx, "task_gone"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("missing task err = %v", err)
	}
}

func TestSubagentTaskKeepsReadonlyModelAndChildSession(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "tasks.db"))
	createTestSession(t, st, "s1")
	task := core.Task{ID: "task_1", Kind: core.TaskKindSubagent, Background: true, Readonly: true, Model: "gpt-x", SessionID: "s1", ChildSessionID: "child_1", Runtime: "matrixclaw", Command: "Review", Status: core.TaskStatusRunning, StartedAt: testEpoch, UpdatedAt: testEpoch}
	if err := st.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := st.ListTasks(ctx, core.TaskFilter{ChildSessionID: "child_1"})
	if err != nil || len(got) != 1 || !got[0].Readonly || got[0].Model != "gpt-x" || got[0].ID != "task_1" || got[0].Isolation != core.SubagentIsolationShared {
		t.Fatalf("tasks = %+v, %v", got, err)
	}
	if none, err := st.ListTasks(ctx, core.TaskFilter{ChildSessionID: "nobody"}); err != nil || len(none) != 0 {
		t.Fatalf("tasks of an unknown child session = %+v, %v", none, err)
	}
}

func TestSubagentLookupByChildSessionUsesAnIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	if err := openTestStore(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var id, parent, unused int
	var detail string
	if err := db.QueryRow(`EXPLAIN QUERY PLAN SELECT id FROM tasks WHERE kind = 'subagent' AND child_session_id = ?`, "s1").Scan(&id, &parent, &unused, &detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "idx_tasks_child_session") {
		t.Fatalf("query plan = %q", detail)
	}
}
