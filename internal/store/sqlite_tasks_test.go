package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
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

func TestMarkTasksDeliveredKeepsTheFirstDelivery(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "tasks.db"))
	createTestSession(t, st, "s1")
	task := core.SubagentTask{ID: "task_1", Mode: core.SubagentTaskModeAsync, ParentSessionID: "s1", Runtime: "matrixclaw", Goal: "Look", Status: core.TaskStatusCompleted, CreatedAt: testEpoch, UpdatedAt: testEpoch, FinishedAt: &testEpoch}
	if err := st.CreateSubagentTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkTasksDelivered(ctx, []string{"task_1"}, "run_a", testEpoch); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkTasksDelivered(ctx, []string{"task_1"}, "run_b", testEpoch.Add(1)); err != nil {
		t.Fatal(err)
	}
	// Saving the task again leaves its delivery alone.
	if err := st.UpdateSubagentTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSubagentTask(ctx, "task_1")
	if err != nil || got.DeliveredRunID != "run_a" || got.DeliveredAt == nil || !got.DeliveredAt.Equal(testEpoch) {
		t.Fatalf("task = %+v, %v", got, err)
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
	finished, err := st.FinishTask(ctx, "task_a", core.TaskStatusFailed, &code, "", testEpoch.Add(time.Minute))
	if err != nil || !finished {
		t.Fatalf("finish = %v, %v", finished, err)
	}
	again, err := st.FinishTask(ctx, "task_a", core.TaskStatusCanceled, nil, "killed", testEpoch.Add(2*time.Minute))
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
	if _, err := st.GetTask(ctx, "task_gone"); err != core.ErrNotFound {
		t.Fatalf("missing task err = %v", err)
	}
}

func TestSubagentTaskKeepsReadonlyModelAndChildSession(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "tasks.db"))
	createTestSession(t, st, "s1")
	task := core.SubagentTask{ID: "task_1", Mode: core.SubagentTaskModeAsync, Readonly: true, Model: "gpt-x", ParentSessionID: "s1", ChildSessionID: "child_1", Runtime: "matrixclaw", Goal: "Review", Status: core.TaskStatusRunning, CreatedAt: testEpoch, UpdatedAt: testEpoch}
	if err := st.CreateSubagentTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSubagentTaskByChildSession(ctx, "child_1")
	if err != nil || !got.Readonly || got.Model != "gpt-x" || got.ID != "task_1" {
		t.Fatalf("task = %+v, %v", got, err)
	}
	task.Readonly = false
	if err := st.UpdateSubagentTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if got, err := st.GetSubagentTask(ctx, "task_1"); err != nil || got.Readonly {
		t.Fatalf("updated task = %+v, %v", got, err)
	}
	if _, err := st.GetSubagentTaskByChildSession(ctx, "nobody"); err != core.ErrNotFound {
		t.Fatalf("missing child session err = %v", err)
	}
}
