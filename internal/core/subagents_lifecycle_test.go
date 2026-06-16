package core

import (
	"context"
	"errors"
	"testing"
)

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
