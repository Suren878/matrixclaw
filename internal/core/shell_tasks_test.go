package core_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/shelltask"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func newTaskCore(t *testing.T) (*core.Core, *store.SQLiteStore, core.Session, string) {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	t.Cleanup(cleanup)
	files := t.TempDir()
	app.WithSessionFiles(files)
	session := permissionSession(t, db, "session_tasks", t.TempDir(), core.PermissionModeDefault, "")
	return app, db, session, files
}

func taskCall(session core.Session) tools.Call {
	return tools.Call{SessionID: session.ID, RunID: "run_tasks", ToolCallID: "call_bash", WorkingDir: session.WorkingDir}
}

func foreground(command string) tools.Command {
	return tools.Command{Command: command, Timeout: time.Minute, AutoBackground: 30 * time.Second, OutputLimit: 1000}
}

func waitTaskStatus(t *testing.T, db *store.SQLiteStore, id string, status core.TaskStatus) core.Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		task, err := db.GetTask(context.Background(), id)
		if err == nil && task.Status == status {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s = %+v, %v; want %s", id, task, err, status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestForegroundCommandReturnsItsOutputAndLeavesNoFile(t *testing.T) {
	t.Parallel()
	app, _, session, files := newTaskCore(t)

	result, err := app.RunCommand(context.Background(), taskCall(session), foreground("echo hello; exit 2"))

	if err != nil || result.TaskID != "" || result.Output != "hello\n" || result.ExitCode != 2 || result.OutputPath != "" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	entries, _ := os.ReadDir(filepath.Join(files, session.ID, "tasks"))
	if len(entries) != 0 {
		t.Fatalf("left files %v", entries)
	}
}

func TestForegroundOutputPastTheLimitKeepsItsFile(t *testing.T) {
	t.Parallel()
	app, _, session, _ := newTaskCore(t)
	command := foreground("seq 1 1000")
	command.OutputLimit = 10

	result, err := app.RunCommand(context.Background(), taskCall(session), command)

	if err != nil || result.Output != "1\n2\n3\n4\n5\n" || result.OutputPath == "" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if tail, err := shelltask.Tail(result.OutputPath, 5); err != nil || tail != "1000\n" {
		t.Fatalf("kept file tail = %q, %v", tail, err)
	}
}

func TestForegroundCommandTimesOut(t *testing.T) {
	t.Parallel()
	app, _, session, _ := newTaskCore(t)
	command := foreground("echo started; sleep 30")
	command.Timeout, command.AutoBackground = 300*time.Millisecond, 0

	result, err := app.RunCommand(context.Background(), taskCall(session), command)

	if err != nil || !result.TimedOut || result.Output != "started\n" || result.ExitCode != -1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestSlowForegroundCommandMovesToTheBackground(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	command := foreground("echo first; sleep 1; echo second")
	command.AutoBackground = 200 * time.Millisecond

	result, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil || result.TaskID == "" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	task := waitTaskStatus(t, db, result.TaskID, core.TaskStatusCompleted)
	if task.Kind != core.TaskKindShell || task.RunID != "run_tasks" || task.ParentToolCallID != "call_bash" || task.ExitCode == nil || *task.ExitCode != 0 || task.DeliveredAt != nil {
		t.Fatalf("task = %+v", task)
	}

	out, err := app.ReadTaskOutput(context.Background(), taskCall(session), tools.TaskRead{TaskID: task.ID, Limit: 100})
	if err != nil || out.Text != "first\nsecond\n" || out.Status != "completed" {
		t.Fatalf("output = %+v, %v", out, err)
	}
	again, err := app.ReadTaskOutput(context.Background(), taskCall(session), tools.TaskRead{TaskID: task.ID, Limit: 100})
	if err != nil || again.Text != "" {
		t.Fatalf("second read = %+v, %v", again, err)
	}
}

func TestBackgroundTaskOutputWaitsAndFilters(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	command := foreground("echo ok 1; echo FAIL 2; sleep 0.3; echo FAIL 3; exit 1")
	command.Background = true

	result, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil || result.TaskID == "" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	out, err := app.ReadTaskOutput(context.Background(), taskCall(session), tools.TaskRead{TaskID: result.TaskID, Wait: 10 * time.Second, Filter: "^FAIL", Limit: 100})
	if err != nil || out.Text != "FAIL 2\nFAIL 3" || out.Status != "failed" || out.ExitCode == nil || *out.ExitCode != 1 {
		t.Fatalf("output = %+v, %v", out, err)
	}
	if task, _ := db.GetTask(context.Background(), result.TaskID); task.OutputCursor == 0 {
		t.Fatalf("cursor not saved: %+v", task)
	}
	if _, err := app.ReadTaskOutput(context.Background(), taskCall(session), tools.TaskRead{TaskID: result.TaskID, Filter: "("}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("bad filter err = %v", err)
	}
	other := taskCall(session)
	other.SessionID = "session_other"
	if _, err := app.ReadTaskOutput(context.Background(), other, tools.TaskRead{TaskID: result.TaskID}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("another session read it: %v", err)
	}
}

func TestStoppedTaskIsCanceledWithoutAnEvent(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	command := foreground("sleep 30")
	command.Background = true
	result, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil {
		t.Fatal(err)
	}

	info, err := app.StopTask(context.Background(), taskCall(session), result.TaskID)
	if err != nil || info.Status != "canceled" {
		t.Fatalf("stop = %+v, %v", info, err)
	}
	task := waitTaskStatus(t, db, result.TaskID, core.TaskStatusCanceled)
	if task.DeliveredAt == nil || task.Error != "stopped with task_kill" {
		t.Fatalf("task = %+v", task)
	}
	waitProcessGone(t, task.PID)
}

func waitProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("process %d still runs", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRecoverTasksKillsLeftoversAndMarksThemLost(t *testing.T) {
	t.Parallel()
	app, db, session, files := newTaskCore(t)
	out, err := shelltask.CreateOutput(filepath.Join(files, session.ID, "tasks", "task_left.log"))
	if err != nil {
		t.Fatal(err)
	}
	process, err := shelltask.Start("sleep 60", session.WorkingDir, out, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = process.Kill() })
	left := core.Task{ID: "task_left", SessionID: session.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "sleep 60", Background: true,
		PID: process.PID(), PGID: process.PID(), OutputPath: out.Path(), StartedAt: process.StartedAt(), UpdatedAt: process.StartedAt()}
	if err := db.CreateTask(context.Background(), left); err != nil {
		t.Fatal(err)
	}

	if err := app.RecoverTasks(context.Background()); err != nil {
		t.Fatal(err)
	}

	select {
	case <-process.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the leftover process group still runs")
	}
	task := waitTaskStatus(t, db, "task_left", core.TaskStatusLost)
	if task.DeliveredAt != nil || !strings.Contains(task.Error, "daemon restarted") {
		t.Fatalf("task = %+v", task)
	}
}

func TestDeletingASessionStopsItsTasks(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	command := foreground("sleep 30")
	command.Background = true
	result, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.GetTask(context.Background(), result.TaskID)
	if err != nil {
		t.Fatal(err)
	}

	if err := app.DeleteSession(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	waitProcessGone(t, task.PID)
	if _, err := os.Stat(task.OutputPath); !os.IsNotExist(err) {
		t.Fatalf("output file after delete: %v", err)
	}
}

func TestNativeRunSeesFinishedAndRunningTasks(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	ctx := context.Background()
	files := t.TempDir()
	app.WithSessionFiles(files)
	session, run := saveCrashRecoveryRun(t, db, "notes", core.RunStatusAccepted, false)
	out, err := shelltask.CreateOutput(filepath.Join(files, session.ID, "tasks", "task_done.log"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte("--- FAIL: TestParse\n")); err != nil {
		t.Fatal(err)
	}
	_ = out.Close()
	now := runRecoveryTestTime()
	for _, task := range []core.Task{
		{ID: "task_done", SessionID: session.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "go test ./...", Background: true, OutputPath: out.Path(), StartedAt: now, UpdatedAt: now},
		{ID: "task_live", SessionID: session.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "npm run dev", Background: true, StartedAt: now, UpdatedAt: now},
	} {
		if err := db.CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	code := 1
	if _, err := db.FinishTask(ctx, "task_done", core.TaskStatusFailed, &code, "", now); err != nil {
		t.Fatal(err)
	}

	var requests []providers.Request
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		requests = append(requests, request)
		return providers.Response{Text: "Noted."}, nil
	})})
	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}

	var sent []string
	for _, message := range requests[0].Messages {
		sent = append(sent, message.Content)
	}
	transcript := strings.Join(sent, "\n")
	for _, want := range []string{"Background task task_done finished with exit code 1.", "--- FAIL: TestParse", "- task_live: npm run dev"} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("first request lacks %q:\n%s", want, transcript)
		}
	}
	done, err := db.GetTask(ctx, "task_done")
	if err != nil || done.DeliveredAt == nil || done.DeliveredRunID != run.ID {
		t.Fatalf("task_done = %+v, %v", done, err)
	}
}
