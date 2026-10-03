package core_test

import (
	"context"
	"encoding/json"
	"errors"
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
	running, err := app.RunCommand(context.Background(), tools.Call{SessionID: session.ID}, tools.Command{Command: "sleep 30", Background: true})
	if err != nil {
		t.Fatal(err)
	}
	both, err := await.Execute(context.Background(), tools.Call{SessionID: session.ID, RunID: "run_x", Args: json.RawMessage(`{"ids":["` + started.TaskID + `","` + running.TaskID + `"]}`)})
	if _, stopErr := app.CancelTask(context.Background(), running.TaskID); stopErr != nil {
		t.Fatal(stopErr)
	}
	if err != nil || both.Await == nil || !strings.Contains(both.Content, "Already finished: "+started.TaskID+" failed, exit code 3.") {
		t.Fatalf("both = %+v, %v", both, err)
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

func TestMessageQueuedWhileTheRunWorkedWakesItOnceItAwaits(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	hooked := &parkHookStore{SQLiteStore: db}
	s, _ := newAwaitScenario(t, hooked, db, func(string) string { return `{}` })
	hooked.onPark = func() {
		accepted, err := s.app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: s.session.ID, Text: "also check the logs", BusyMode: core.BusyInputModeQueue})
		if err != nil || accepted.Status != core.AcceptRunStatusQueued {
			t.Errorf("accepted = %+v, %v", accepted, err)
		}
	}

	if err := s.app.ExecuteRun(context.Background(), s.run.ID); err != nil {
		t.Fatal(err)
	}

	waitRunStatus(t, db, s.run.ID, core.RunStatusCompleted)
	if got := s.woken(t); !strings.Contains(got, "also check the logs") {
		t.Fatalf("the woken request sends %q", got)
	}
}

func TestCancelingAWaitingRunStopsItsTasksWithoutWakingTheSession(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	app := core.New(db)
	starter := &executingRunStarter{app: app}
	app.WithSessionFiles(t.TempDir()).WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(core.AwaitToolExecutors(app)...))
	session, run := saveCrashRecoveryRun(t, db, "cancel-waiting", core.RunStatusAccepted, false)
	ctx := context.Background()
	started, err := app.RunCommand(ctx, tools.Call{SessionID: session.ID, RunID: run.ID}, tools.Command{Command: "sleep 30", Background: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = app.CancelTask(context.Background(), started.TaskID) })
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if toolResultContent(request, "call_await") == "" {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call_await", Name: "await", Arguments: json.RawMessage(`{"ids":["` + started.TaskID + `"]}`)}}}, nil
		}
		return providers.Response{Text: "Done."}, nil
	})})
	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingEvents)
	now := time.Now().UTC()
	if err := db.CreateTask(ctx, core.Task{ID: "task_other", SessionID: session.ID, RunID: run.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "make", Background: true, StartedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	code := 0
	if _, err := db.FinishTask(ctx, "task_other", core.TaskStatusCompleted, &code, "", now); err != nil {
		t.Fatal(err)
	}

	if _, err := app.CancelRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.RecoverTaskEvents(ctx); err != nil {
		t.Fatal(err)
	}
	starter.wait(t)

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCanceled)
	if task, err := db.GetTask(ctx, started.TaskID); err != nil || task.Status != core.TaskStatusCanceled {
		t.Fatalf("awaited task = %+v, %v", task, err)
	}
	if _, err := db.GetRunWakeup(ctx, run.ID); err != core.ErrNotFound {
		t.Fatalf("wakeup after cancel: %v", err)
	}
	if runs, err := db.ListSessionRuns(ctx, session.ID, 0); err != nil || len(runs) != 1 {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
}

func TestCancelingARunWaitingForApprovalStartsTheQueuedMessage(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	session, run := saveCrashRecoveryRun(t, db, "cancel-approval", core.RunStatusWaitingApproval, false)
	ctx := context.Background()
	queued, err := app.AcceptRun(ctx, core.HandleMessageInput{SessionID: session.ID, Text: "next task", BusyMode: core.BusyInputModeQueue})
	if err != nil || queued.Status != core.AcceptRunStatusQueued {
		t.Fatalf("queued = %+v, %v", queued, err)
	}

	if _, err := app.CancelRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}

	runs, err := db.ListSessionRuns(ctx, session.ID, 0)
	if err != nil || len(runs) != 2 || starter.count(runs[0].ID) != 1 {
		t.Fatalf("runs = %+v, starts = %v, %v", runs, starter.ids, err)
	}
}

func TestCanceledRunRecordsFinishedAt(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunStarter(&recordingRunStarter{})
	_, run := saveCrashRecoveryRun(t, db, "cancel-finished", core.RunStatusWaitingApproval, false)
	ctx := context.Background()

	if _, err := app.CancelRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}

	stored, err := db.GetRun(ctx, run.ID)
	if err != nil || stored.Status != core.RunStatusCanceled || stored.FinishedAt == nil {
		t.Fatalf("canceled run = %+v, %v", stored, err)
	}
}

type failingRunStarter struct{}

func (failingRunStarter) StartRun(context.Context, string) error {
	return errors.New("run starter down")
}

func TestWakeupStaysUntilItsRunExecutes(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunStarter(failingRunStarter{})
	session, run := saveCrashRecoveryRun(t, db, "wake-retry", core.RunStatusWaitingEvents, false)
	ctx := context.Background()
	if err := db.SaveRunWakeup(ctx, core.RunWakeup{RunID: run.ID, SessionID: session.ID, WakeAt: time.Now().UTC().Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}

	if err := app.WakeDueRuns(ctx); err == nil {
		t.Fatal("waking with a broken run starter did not fail")
	}
	if _, err := db.GetRunWakeup(ctx, run.ID); err != nil {
		t.Fatalf("wakeup after a failed start: %v", err)
	}
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	if err := app.WakeDueRuns(ctx); err != nil {
		t.Fatal(err)
	}

	if starter.count(run.ID) != 1 {
		t.Fatalf("starts = %v", starter.ids)
	}
	if _, err := db.GetRunWakeup(ctx, run.ID); err != nil {
		t.Fatalf("wakeup before the run executes: %v", err)
	}
	app.WithSessionLLMs(recoveryLLMs{runtime: &recoveryRuntime{text: "Woke."}})
	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetRunWakeup(ctx, run.ID); err != core.ErrNotFound {
		t.Fatalf("wakeup after the run executed: %v", err)
	}
}

func TestLostTasksWakeNothingBeforeRunsCanStart(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	app := core.New(db).WithSessionFiles(t.TempDir())
	session := permissionSession(t, db, "session_restart", t.TempDir(), core.PermissionModeDefault, "")
	ctx := context.Background()
	now := time.Now().UTC()
	for _, task := range []core.Task{
		{ID: "task_subagent", SessionID: session.ID, Kind: core.TaskKindSubagent, Status: core.TaskStatusRunning, Command: "scan", Background: true, StartedAt: now, UpdatedAt: now},
		{ID: "task_shell", SessionID: session.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "make", Background: true, StartedAt: now, UpdatedAt: now},
	} {
		if err := db.CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.FinishTask(ctx, "task_subagent", core.TaskStatusCompleted, nil, "", now); err != nil {
		t.Fatal(err)
	}

	if err := app.RecoverTasks(ctx); err != nil {
		t.Fatal(err)
	}
	if runs, err := db.ListSessionRuns(ctx, session.ID, 0); err != nil || len(runs) != 0 {
		t.Fatalf("runs before the run starter = %+v, %v", runs, err)
	}
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	if err := app.RecoverTaskEvents(ctx); err != nil {
		t.Fatal(err)
	}

	if len(starter.ids) != 1 {
		t.Fatalf("starts = %v", starter.ids)
	}
}

// wakeupFailingStore fails reading the wakeup of one run.
type wakeupFailingStore struct {
	*store.SQLiteStore
	runID string
}

func (s wakeupFailingStore) GetRunWakeup(ctx context.Context, runID string) (core.RunWakeup, error) {
	if runID == s.runID {
		return core.RunWakeup{}, errors.New("disk error")
	}
	return s.SQLiteStore.GetRunWakeup(ctx, runID)
}

func TestRecoverActiveRunsGoesOnPastAFailedWake(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	_, broken := saveCrashRecoveryRun(t, db, "wake-broken", core.RunStatusWaitingEvents, false)
	_, fine := saveCrashRecoveryRun(t, db, "wake-fine", core.RunStatusWaitingEvents, false)
	app := core.New(wakeupFailingStore{SQLiteStore: db, runID: broken.ID})
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)

	if err := app.RecoverActiveRuns(context.Background()); err == nil {
		t.Fatal("recovery hid the failed wake")
	}

	if starter.count(fine.ID) != 1 {
		t.Fatalf("starts = %v", starter.ids)
	}
}

func TestDeletingASessionWakesNothingInIt(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	s, _ := newAwaitScenario(t, db, db, func(id string) string { return `{"ids":["` + id + `"]}` })
	if err := s.app.ExecuteRun(context.Background(), s.run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, s.run.ID, core.RunStatusWaitingEvents)

	if err := s.app.DeleteSession(context.Background(), s.session.ID); err != nil {
		t.Fatal(err)
	}

	if len(s.starter.ids) != 0 {
		t.Fatalf("starts = %v", s.starter.ids)
	}
}

func TestAwaitedTaskReadElsewhereStillWakesTheRun(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	ctx := context.Background()
	session, run := saveCrashRecoveryRun(t, db, "await-read", core.RunStatusWaitingEvents, false)
	now := runRecoveryTestTime()
	if err := db.CreateTask(ctx, core.Task{ID: "task_read", SessionID: session.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "make", Background: true, StartedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishTask(ctx, "task_read", core.TaskStatusCompleted, nil, "", now); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkTasksDelivered(ctx, []string{"task_read"}, run.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveRunWakeup(ctx, core.RunWakeup{RunID: run.ID, SessionID: session.ID, WakeAt: time.Now().UTC().Add(time.Hour), TaskIDs: []string{"task_read"}}); err != nil {
		t.Fatal(err)
	}

	if err := app.RecoverActiveRuns(ctx); err != nil {
		t.Fatal(err)
	}

	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("run starts = %d, want 1", got)
	}
}
