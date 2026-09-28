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
