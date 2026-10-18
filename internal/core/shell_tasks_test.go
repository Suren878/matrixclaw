package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
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
	command.OutputLimit = 30

	result, err := app.RunCommand(context.Background(), taskCall(session), command)

	if err != nil || !strings.HasPrefix(result.Output, "1\n2\n3\n4\n5\n[... ") || !strings.HasSuffix(result.Output, "\n999\n1000\n") || result.OutputPath == "" {
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
	if task.Kind != core.TaskKindShell || task.RunID != "run_tasks" || task.ParentToolCallID != "call_bash" || task.ExitCode == nil || *task.ExitCode != 0 {
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
	if err != nil || out.Text != "FAIL 2\nFAIL 3" || out.Status != "failed" || out.ExitCode == nil || *out.ExitCode != 1 || out.Running {
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
	if out, err := app.ReadTaskOutput(context.Background(), taskCall(session), tools.TaskRead{TaskID: result.TaskID, Wait: 10 * time.Millisecond}); err != nil || !out.Running {
		t.Fatalf("output of a running task = %+v, %v", out, err)
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

func TestStoppedTaskCanCleanUpOnTERM(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	command := foreground("trap 'echo cleaned up; exit 0' TERM; echo up; while true; do sleep 0.1; done")
	command.Background = true
	result, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.GetTask(context.Background(), result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if tail, _ := shelltask.Tail(task.OutputPath, 100); tail == "up\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the task wrote no output")
		}
	}

	if _, err := app.StopTask(context.Background(), taskCall(session), result.TaskID); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		tail, err := shelltask.Tail(task.OutputPath, 100)
		if strings.HasSuffix(tail, "cleaned up\n") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("output = %q, %v", tail, err)
		}
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

func TestRecoveryKillsLeftoverShellTasksAndMarksThemLost(t *testing.T) {
	t.Parallel()
	app, db, session, files := newTaskCore(t)
	out, err := shelltask.CreateOutput(filepath.Join(files, session.ID, "tasks", "task_left.log"))
	if err != nil {
		t.Fatal(err)
	}
	process, err := shelltask.Start("sleep 60", session.WorkingDir, nil, out)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shelltask.KillGroup(process.PID()) })
	left := core.Task{ID: "task_left", SessionID: session.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "sleep 60", Background: true,
		PID: process.PID(), PGID: process.PID(), LeaderStart: process.Leader().Start, BootID: process.Leader().BootID, OutputPath: out.Path(), StartedAt: process.StartedAt(), UpdatedAt: process.StartedAt()}
	if err := db.CreateTask(context.Background(), left); err != nil {
		t.Fatal(err)
	}

	if err := app.Recover(context.Background()); err != nil {
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

// The daemon's stop ends its background commands: the next start reports them
// lost, and no run is started (and failed) for them meanwhile.
func TestACommandEndingWithTheDaemonIsLostAfterTheRestart(t *testing.T) {
	t.Parallel()
	app, db, session, files := newTaskCore(t)
	lifetime, stop := context.WithCancel(context.Background())
	app.WithLifetime(lifetime)
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

	stop()
	app.WaitRuns()
	app.StopShellTasks()
	if err := syscall.Kill(-task.PID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("the command's group outlived StopShellTasks: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := core.New(db).WithSessionFiles(files).Recover(context.Background()); err != nil {
		t.Fatal(err)
	}

	if task, err := db.GetTask(context.Background(), result.TaskID); err != nil || task.Status != core.TaskStatusLost {
		t.Fatalf("task after restart = %+v, %v", task, err)
	}
	if runs, err := db.ListSessionRuns(context.Background(), session.ID, 0); err != nil || len(runs) != 0 {
		t.Fatalf("runs = %+v, %v", runs, err)
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
	if _, err := db.FinishTask(ctx, "task_done", core.TaskEnd{Status: core.TaskStatusFailed, ExitCode: &code, At: now}); err != nil {
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

func TestUserStopsATaskAndTheSessionIsTold(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	command := foreground("echo up; sleep 30")
	command.Background = true
	result, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListSessionTasks(context.Background(), session.ID)
	if err != nil || len(listed) != 1 || listed[0].ID != result.TaskID {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if detail, err := app.TaskDetail(context.Background(), result.TaskID); err == nil && detail.OutputTail == "up\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the task wrote no output")
		}
	}

	task, err := app.CancelTask(context.Background(), result.TaskID)
	if err != nil || task.Status != core.TaskStatusCanceled {
		t.Fatalf("cancel = %+v, %v", task, err)
	}
	waitProcessGone(t, task.PID)
	events, err := db.ListTasks(context.Background(), core.TaskFilter{SessionID: session.ID, Undelivered: true})
	if err != nil || len(events) != 1 || events[0].Error != "stopped by the user" {
		t.Fatalf("events = %+v, %v", events, err)
	}
}

func TestSessionTasksListsBackgroundTasksPastManyOthers(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	ctx := context.Background()
	now := runRecoveryTestTime()
	if err := db.CreateTask(ctx, core.Task{ID: "task_bg", SessionID: session.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "npm run dev", Background: true, StartedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	for i := range 40 {
		at := now.Add(time.Duration(i+1) * time.Second)
		if err := db.CreateTask(ctx, core.Task{ID: fmt.Sprintf("task_fg_%d", i), SessionID: session.ID, Kind: core.TaskKindSubagent, Status: core.TaskStatusCompleted, Command: "look", StartedAt: at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}

	listed, err := app.ListSessionTasks(ctx, session.ID)

	if err != nil || len(listed) != 1 || listed[0].ID != "task_bg" {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
}

func TestSessionRunsAtMostItsLimitOfBackgroundCommands(t *testing.T) {
	t.Parallel()
	app, _, session, _ := newTaskCore(t)
	app.SetTuning(core.Tuning{BackgroundTasks: 1})
	command := foreground("sleep 30")
	command.Background = true
	first, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = app.StopTask(context.Background(), taskCall(session), first.TaskID) })

	if _, err := app.RunCommand(context.Background(), taskCall(session), command); !errors.Is(err, core.ErrInvalidInput) || !strings.Contains(err.Error(), "already runs 1 background command") {
		t.Fatalf("second background command err = %v", err)
	}
	slow := foreground("sleep 0.5; echo done")
	slow.AutoBackground = 100 * time.Millisecond
	result, err := app.RunCommand(context.Background(), taskCall(session), slow)
	if err != nil || result.TaskID != "" || result.Output != "done\n" {
		t.Fatalf("slow command = %+v, %v", result, err)
	}
}

func TestAutoBackgroundedCommandKeepsWhatItLeftRunning(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	command := foreground("sleep 0.3; (sleep 2.5; echo late) & echo early")
	command.AutoBackground = 100 * time.Millisecond

	result, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil || result.TaskID == "" {
		t.Fatalf("result = %+v, %v", result, err)
	}

	task := waitTaskStatus(t, db, result.TaskID, core.TaskStatusCompleted)
	if tail, err := shelltask.Tail(task.OutputPath, 100); err != nil || tail != "early\nlate\n" {
		t.Fatalf("output = %q, %v", tail, err)
	}
}

func TestTaskOutputWaitsForASubagent(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	ctx := context.Background()
	now := runRecoveryTestTime()
	if err := db.CreateTask(ctx, core.Task{ID: "task_agent", SessionID: session.ID, Kind: core.TaskKindSubagent, Status: core.TaskStatusRunning, Command: "look", Background: true, StartedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = db.FinishTask(ctx, "task_agent", core.TaskEnd{Status: core.TaskStatusCompleted, At: now})
	}()

	started := time.Now()
	out, err := app.ReadTaskOutput(ctx, taskCall(session), tools.TaskRead{TaskID: "task_agent", Wait: 10 * time.Second, Limit: 100})

	if err != nil || out.Status != "completed" || time.Since(started) > 5*time.Second {
		t.Fatalf("output = %+v, %v after %s", out, err, time.Since(started))
	}
}

func TestDeletingASessionStopsItsRun(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionFiles(t.TempDir())
	started := make(chan struct{})
	stopped := make(chan struct{})
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("long_build", tools.EffectReadOnly), fn: func(ctx context.Context, _ tools.Call) (tools.Result, error) {
		close(started)
		select {
		case <-ctx.Done():
			close(stopped)
			return tools.Result{}, ctx.Err()
		case <-time.After(2 * time.Second):
			return tools.Result{Content: "built"}, nil
		}
	}}))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-build", Name: "long_build", Arguments: []byte(`{}`)}}}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "delete_busy", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	<-started
	if err := app.DeleteSession(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("the deleted session's run kept executing its tool")
	}
	<-done
}

func TestForegroundCommandKillsWhatItLeftRunning(t *testing.T) {
	t.Parallel()
	app, _, session, _ := newTaskCore(t)

	result, err := app.RunCommand(context.Background(), taskCall(session), foreground("sleep 30 >/dev/null 2>&1 & echo $!"))

	if err != nil || !result.KilledLeftovers {
		t.Fatalf("result = %+v, %v", result, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(result.Output))
	if err != nil {
		t.Fatal(err)
	}
	waitProcessGone(t, pid)
}

func TestCommandsDoNotSeeProviderKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-provider-secret")
	t.Setenv("MATRIXCLAW_API_TOKEN", "daemon-token")
	app, _, session, _ := newTaskCore(t)
	app.WithCommandEnvironment(func() []string { return setup.ChildEnvironment(setup.Config{}, os.Environ()) })

	result, err := app.RunCommand(context.Background(), taskCall(session), foreground("env"))

	if err != nil || !strings.Contains(result.Output, "PATH=") || strings.Contains(result.Output, "sk-provider-secret") || strings.Contains(result.Output, "daemon-token") {
		t.Fatalf("env = %q, %v", result.Output, err)
	}
}

func TestCompactionPrunesTheOutputOfEndedCommands(t *testing.T) {
	t.Parallel()
	app, db, session, files := newTaskCore(t)
	ctx := context.Background()
	// The ended task is stored as such: one ending now would wake the session.
	endedLog := filepath.Join(files, session.ID, "tasks", "task_ended.log")
	if err := os.MkdirAll(filepath.Dir(endedLog), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endedLog, []byte("done\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now, code := runRecoveryTestTime(), 0
	if err := db.CreateTask(ctx, core.Task{ID: "task_ended", SessionID: session.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "echo done", Background: true, OutputPath: endedLog, StartedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishTask(ctx, "task_ended", core.TaskEnd{Status: core.TaskStatusCompleted, ExitCode: &code, At: now}); err != nil {
		t.Fatal(err)
	}
	running := foreground("sleep 30")
	running.Background = true
	runningTask, err := app.RunCommand(ctx, taskCall(session), running)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = app.StopTask(ctx, taskCall(session), runningTask.TaskID) })

	if _, err := app.ClearContext(ctx, session.ID); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(files, session.ID, "tasks"))
	if err != nil || len(entries) != 1 || entries[0].Name() != runningTask.TaskID+".log" {
		t.Fatalf("kept task outputs = %v, %v", entries, err)
	}
	out, err := app.ReadTaskOutput(ctx, taskCall(session), tools.TaskRead{TaskID: "task_ended", Limit: 100})
	if err != nil || out.Status != "completed" || out.Text == "" {
		t.Fatalf("output of a pruned task = %+v, %v", out, err)
	}
}

type recordingShell struct {
	mu       sync.Mutex
	commands []string
}

func (s *recordingShell) RunCommand(_ context.Context, _ tools.Call, command tools.Command) (tools.CommandResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, command.Command)
	return tools.CommandResult{Output: "ok\n"}, nil
}

func (s *recordingShell) ReadTaskOutput(context.Context, tools.Call, tools.TaskRead) (tools.TaskOutput, error) {
	return tools.TaskOutput{}, nil
}

func (s *recordingShell) StopTask(context.Context, tools.Call, string) (tools.TaskInfo, error) {
	return tools.TaskInfo{}, nil
}

func TestReadonlyChildRunsOnlyReadOnlyCommands(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	shell := &recordingShell{}
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), tools.NewShellExecutors(shell)...)...))
	var childTools []providers.ToolDefinition
	var results []string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		seen := toolResults(request)
		switch {
		case childPrompt(request) != "" && len(seen) == 0:
			childTools = request.Tools
			return providers.Response{ToolCalls: []providers.ToolCall{
				{ID: "child-log", Name: "bash", Arguments: json.RawMessage(`{"command":"git log --oneline -3"}`)},
				{ID: "child-test", Name: "bash", Arguments: json.RawMessage(`{"command":"go test ./..."}`)},
			}}, nil
		case childPrompt(request) != "":
			results = seen
			return providers.Response{Text: "looked"}, nil
		case len(seen) == 0:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-a", Name: "agent", Arguments: json.RawMessage(`{"description":"Look","prompt":"look around","readonly":true}`)}}}, nil
		default:
			return providers.Response{Text: "Done."}, nil
		}
	})})
	_, run := saveCrashRecoveryRun(t, db, "readonly_bash", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	if !slices.ContainsFunc(childTools, func(tool providers.ToolDefinition) bool { return tool.Name == "bash" }) {
		t.Fatalf("the read-only child got no bash: %+v", childTools)
	}
	if len(shell.commands) != 1 || shell.commands[0] != "git log --oneline -3" {
		t.Fatalf("commands run = %q", shell.commands)
	}
	if len(results) != 2 || !strings.HasPrefix(results[0], "child-log=ok") || !strings.Contains(results[1], "read-only") {
		t.Fatalf("child results = %q", results)
	}
}
