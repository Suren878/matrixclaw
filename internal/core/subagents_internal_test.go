package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResumeParentAfterSubagentTerminalWatcherRecoversPanic(t *testing.T) {
	panicReached := make(chan struct{})
	fakeStore := &panicResumeWatcherStore{panicReached: panicReached}
	app := New(fakeStore)

	err := app.resumeParentAfterSubagentTerminal(context.Background(), SubagentTask{
		ParentRunID: "run_parent",
		ChildRunID:  "run_child",
	})
	if err != nil {
		t.Fatalf("resumeParentAfterSubagentTerminal: %v", err)
	}

	select {
	case <-panicReached:
	case <-time.After(time.Second):
		t.Fatalf("watcher did not poll child run")
	}
}

func TestResumeParentAfterSubagentTerminalWatcherStopsWhenParentTerminal(t *testing.T) {
	fakeStore := &parentTerminalResumeWatcherStore{}
	starter := &recordingCoreRunStarter{started: make(chan string, 1)}
	app := New(fakeStore).WithRunStarter(starter)

	err := app.resumeParentAfterSubagentTerminal(context.Background(), SubagentTask{
		ID:          "task-1",
		ParentRunID: "run_parent",
		ChildRunID:  "run_child",
	})
	if err != nil {
		t.Fatalf("resumeParentAfterSubagentTerminal: %v", err)
	}

	select {
	case runID := <-starter.started:
		t.Fatalf("startRun called for %q, want watcher to stop because parent is terminal", runID)
	case <-time.After(700 * time.Millisecond):
	}
}

func TestResumeParentAfterSubagentTerminalWatcherStopsAfterChildNotFound(t *testing.T) {
	fakeStore := &childNotFoundResumeWatcherStore{}
	starter := &recordingCoreRunStarter{started: make(chan string, 1)}
	app := New(fakeStore).WithRunStarter(starter)

	err := app.resumeParentAfterSubagentTerminal(context.Background(), SubagentTask{
		ID:          "task-1",
		ParentRunID: "run_parent",
		ChildRunID:  "run_child",
	})
	if err != nil {
		t.Fatalf("resumeParentAfterSubagentTerminal: %v", err)
	}

	waitForAtomicAtLeast(t, &fakeStore.childCalls, 2, time.Second)
	callsAfterNotFound := fakeStore.childCalls.Load()
	time.Sleep(350 * time.Millisecond)
	if got := fakeStore.childCalls.Load(); got > callsAfterNotFound {
		t.Fatalf("child GetRun calls continued after ErrNotFound: got %d, want %d", got, callsAfterNotFound)
	}
	select {
	case runID := <-starter.started:
		t.Fatalf("startRun called for %q after child ErrNotFound", runID)
	default:
	}
}

func TestResumeParentAfterSubagentTerminalWatcherStopsAfterRepeatedStoreErrors(t *testing.T) {
	fakeStore := &storeErrorResumeWatcherStore{}
	starter := &recordingCoreRunStarter{started: make(chan string, 1)}
	app := New(fakeStore).WithRunStarter(starter)

	err := app.resumeParentAfterSubagentTerminal(context.Background(), SubagentTask{
		ID:          "task-1",
		ParentRunID: "run_parent",
		ChildRunID:  "run_child",
	})
	if err != nil {
		t.Fatalf("resumeParentAfterSubagentTerminal: %v", err)
	}

	waitForAtomicAtLeast(t, &fakeStore.childCalls, 21, 7*time.Second)
	callsAfterLimit := fakeStore.childCalls.Load()
	time.Sleep(350 * time.Millisecond)
	if got := fakeStore.childCalls.Load(); got > callsAfterLimit {
		t.Fatalf("child GetRun calls continued after error limit: got %d, want %d", got, callsAfterLimit)
	}
	select {
	case runID := <-starter.started:
		t.Fatalf("startRun called for %q after repeated store errors", runID)
	default:
	}
}

func TestAfterRunExecutionReturnsGetRunStoreError(t *testing.T) {
	want := errors.New("get run failed")
	app := New(&afterRunExecutionStore{getRunErr: want})

	err := app.afterRunExecution(context.Background(), "run_child")
	if !errors.Is(err, want) {
		t.Fatalf("afterRunExecution error = %v, want %v", err, want)
	}
}

func TestAfterRunExecutionReturnsSubagentTaskLookupStoreError(t *testing.T) {
	want := errors.New("get task failed")
	app := New(&afterRunExecutionStore{
		run:     Run{ID: "run_child", SessionID: "session_child", Status: RunStatusRunning},
		taskErr: want,
	})

	err := app.afterRunExecution(context.Background(), "run_child")
	if !errors.Is(err, want) {
		t.Fatalf("afterRunExecution error = %v, want %v", err, want)
	}
}

func TestAfterRunExecutionReturnsGetSessionStoreError(t *testing.T) {
	want := errors.New("get session failed")
	app := New(&afterRunExecutionStore{
		run:        Run{ID: "run_child", SessionID: "session_child", Status: RunStatusRunning},
		taskErr:    ErrNotFound,
		sessionErr: want,
	})

	err := app.afterRunExecution(context.Background(), "run_child")
	if !errors.Is(err, want) {
		t.Fatalf("afterRunExecution error = %v, want %v", err, want)
	}
}

func TestAfterRunExecutionIgnoresExpectedMissingRecords(t *testing.T) {
	tests := []struct {
		name  string
		store *afterRunExecutionStore
	}{
		{
			name:  "missing run",
			store: &afterRunExecutionStore{getRunErr: ErrNotFound},
		},
		{
			name: "missing subagent task",
			store: &afterRunExecutionStore{
				run:        Run{ID: "run_child", SessionID: "session_child", Status: RunStatusRunning},
				taskErr:    ErrNotFound,
				session:    Session{ID: "session_child"},
				sessionErr: nil,
			},
		},
		{
			name: "missing session",
			store: &afterRunExecutionStore{
				run:        Run{ID: "run_child", SessionID: "session_child", Status: RunStatusRunning},
				taskErr:    ErrNotFound,
				sessionErr: ErrNotFound,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := New(tt.store)
			if err := app.afterRunExecution(context.Background(), "run_child"); err != nil {
				t.Fatalf("afterRunExecution: %v", err)
			}
		})
	}
}

type panicResumeWatcherStore struct {
	Store
	calls        atomic.Int32
	once         sync.Once
	panicReached chan struct{}
}

func (s *panicResumeWatcherStore) GetRun(_ context.Context, runID string) (Run, error) {
	switch runID {
	case "run_parent":
		return Run{ID: runID, Status: RunStatusAccepted}, nil
	case "run_child":
		if s.calls.Add(1) == 1 {
			return Run{ID: runID, Status: RunStatusAccepted}, nil
		}
	default:
		return Run{}, ErrNotFound
	}
	s.once.Do(func() {
		close(s.panicReached)
	})
	panic("watcher boom")
}

type afterRunExecutionStore struct {
	Store
	run        Run
	getRunErr  error
	task       SubagentTask
	taskErr    error
	session    Session
	sessionErr error
}

func (s *afterRunExecutionStore) GetRun(_ context.Context, runID string) (Run, error) {
	if s.getRunErr != nil {
		return Run{}, s.getRunErr
	}
	run := s.run
	if run.ID == "" {
		run.ID = runID
	}
	return run, nil
}

func (s *afterRunExecutionStore) GetSubagentTaskByChildRun(context.Context, string) (SubagentTask, error) {
	if s.taskErr != nil {
		return SubagentTask{}, s.taskErr
	}
	return s.task, nil
}

func (s *afterRunExecutionStore) GetSession(_ context.Context, sessionID string) (Session, error) {
	if s.sessionErr != nil {
		return Session{}, s.sessionErr
	}
	session := s.session
	if session.ID == "" {
		session.ID = sessionID
	}
	return session, nil
}

type parentTerminalResumeWatcherStore struct {
	Store
	childCalls atomic.Int32
}

func (s *parentTerminalResumeWatcherStore) GetRun(_ context.Context, runID string) (Run, error) {
	switch runID {
	case "run_child":
		if s.childCalls.Add(1) == 1 {
			return Run{ID: runID, Status: RunStatusAccepted}, nil
		}
		return Run{ID: runID, Status: RunStatusCompleted}, nil
	case "run_parent":
		return Run{ID: runID, Status: RunStatusCompleted}, nil
	default:
		return Run{}, ErrNotFound
	}
}

type childNotFoundResumeWatcherStore struct {
	Store
	childCalls atomic.Int32
}

func (s *childNotFoundResumeWatcherStore) GetRun(_ context.Context, runID string) (Run, error) {
	switch runID {
	case "run_child":
		if s.childCalls.Add(1) == 1 {
			return Run{ID: runID, Status: RunStatusAccepted}, nil
		}
		return Run{}, ErrNotFound
	case "run_parent":
		return Run{ID: runID, Status: RunStatusAccepted}, nil
	default:
		return Run{}, ErrNotFound
	}
}

var errResumeWatcherStore = errors.New("store unavailable")

type storeErrorResumeWatcherStore struct {
	Store
	childCalls atomic.Int32
}

func (s *storeErrorResumeWatcherStore) GetRun(_ context.Context, runID string) (Run, error) {
	switch runID {
	case "run_child":
		if s.childCalls.Add(1) == 1 {
			return Run{ID: runID, Status: RunStatusAccepted}, nil
		}
		return Run{}, errResumeWatcherStore
	case "run_parent":
		return Run{ID: runID, Status: RunStatusAccepted}, nil
	default:
		return Run{}, ErrNotFound
	}
}

type recordingCoreRunStarter struct {
	started chan string
}

func (s *recordingCoreRunStarter) StartRun(_ context.Context, runID string) error {
	s.started <- runID
	return nil
}

func waitForAtomicAtLeast(t *testing.T, counter *atomic.Int32, want int32, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if got := counter.Load(); got >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("counter = %d, want at least %d", counter.Load(), want)
		case <-ticker.C:
		}
	}
}
