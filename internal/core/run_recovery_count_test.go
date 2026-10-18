package core_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestARunRestartedTooOftenWithoutProgressFails(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &recoveryRuntime{text: "never asked"}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	_, run := saveCrashRecoveryRun(t, db, "restart-loop", core.RunStatusRunning, false)
	if err := db.SaveRunCheckpoint(context.Background(), core.RunCheckpoint{RunID: run.ID, RecoveryCount: 8, UpdatedAt: run.UpdatedAt}); err != nil {
		t.Fatal(err)
	}

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	stored, err := db.GetRun(context.Background(), run.ID)
	if err != nil || stored.Status != core.RunStatusFailed || !strings.Contains(stored.Error, "8 daemon-restart recovery attempts") || runtime.requestCount() != 0 {
		t.Fatalf("run = %s %q (%v), requests = %d", stored.Status, stored.Error, err, runtime.requestCount())
	}
}

func TestAFinishedStepResetsTheRecoveryCount(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(changingTool("inspect_state")))
	_, run := saveCrashRecoveryRun(t, db, "restart-progress", core.RunStatusRunning, false)
	if err := db.SaveRunCheckpoint(context.Background(), core.RunCheckpoint{RunID: run.ID, RecoveryCount: 7, UpdatedAt: run.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	var counts []int
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
		checkpoint, err := db.GetRunCheckpoint(ctx, run.ID)
		if err != nil && !errors.Is(err, core.ErrNotFound) {
			return providers.Response{}, err
		}
		counts = append(counts, checkpoint.RecoveryCount)
		if len(counts) == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-inspect", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		return providers.Response{Text: "Done."}, nil
	})})

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if len(counts) != 2 || counts[0] != 8 || counts[1] != 0 {
		t.Fatalf("recovery counts seen by the model = %v, want [8 0]", counts)
	}
}

// failingTaskStore cannot record tasks.
type failingTaskStore struct {
	*store.SQLiteStore
}

func (failingTaskStore) CreateTask(context.Context, core.Task) error {
	return errors.New("disk full")
}

func TestASubagentWhoseTaskCannotBeRecordedLeavesNoSessionBehind(t *testing.T) {
	t.Parallel()
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	app := core.New(failingTaskStore{SQLiteStore: db})
	app.WithRunStarter(&recordingRunStarter{})
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	var spawnResult string
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if calls++; calls > 1 {
			spawnResult = request.Messages[len(request.Messages)-1].Content
			return providers.Response{Text: "Could not delegate."}, nil
		}
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-spawn", Name: "agent", Arguments: []byte(`{"description":"Scanner","prompt":"scan","runtime":"matrixclaw"}`)}}}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "discard", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(spawnResult, "disk full") {
		t.Fatalf("spawn result = %q, want the failed start", spawnResult)
	}
	sessions, err := db.ListSessions(context.Background(), core.SessionListFilter{IncludeHidden: true})
	if err != nil || len(sessions) != 1 || sessions[0].ID != session.ID {
		t.Fatalf("sessions = %+v (%v), want only the parent", sessions, err)
	}
	active, err := db.ListActiveRuns(context.Background())
	if err != nil || len(active) != 0 {
		t.Fatalf("active runs = %+v (%v)", active, err)
	}
}
