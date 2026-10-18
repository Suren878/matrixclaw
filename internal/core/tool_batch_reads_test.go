package core_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// readCountingStore counts the reads a tool call can make.
type readCountingStore struct {
	*store.SQLiteStore
	reads atomic.Int64
}

func (s *readCountingStore) GetSession(ctx context.Context, id string) (core.Session, error) {
	s.reads.Add(1)
	return s.SQLiteStore.GetSession(ctx, id)
}

func (s *readCountingStore) GetMessage(ctx context.Context, id string) (transcript.Message, error) {
	s.reads.Add(1)
	return s.SQLiteStore.GetMessage(ctx, id)
}

func (s *readCountingStore) HasToolResult(ctx context.Context, sessionID, runID, callID string) (bool, error) {
	s.reads.Add(1)
	return s.SQLiteStore.HasToolResult(ctx, sessionID, runID, callID)
}

func (s *readCountingStore) ListPendingSteerInputs(ctx context.Context, sessionID, runID string) ([]core.SessionInput, error) {
	s.reads.Add(1)
	return s.SQLiteStore.ListPendingSteerInputs(ctx, sessionID, runID)
}

func (s *readCountingStore) ListRunApprovals(ctx context.Context, sessionID, runID string) ([]core.Approval, error) {
	s.reads.Add(1)
	return s.SQLiteStore.ListRunApprovals(ctx, sessionID, runID)
}

func (s *readCountingStore) ListPermissionRules(ctx context.Context, sessionIDs []string) ([]permission.Rule, error) {
	s.reads.Add(1)
	return s.SQLiteStore.ListPermissionRules(ctx, sessionIDs)
}

func (s *readCountingStore) ListTasks(ctx context.Context, filter core.TaskFilter) ([]core.Task, error) {
	s.reads.Add(1)
	return s.SQLiteStore.ListTasks(ctx, filter)
}

// batchReads runs a run whose first reply calls a read-only tool n times and
// counts its reads.
func batchReads(t *testing.T, n int, subagent bool) int64 {
	t.Helper()
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	counting := &readCountingStore{SQLiteStore: db}
	app := core.New(counting)
	app.WithTools(tools.NewRegistry(&recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}))
	replies := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		if replies++; replies > 1 {
			return providers.Response{Text: "Done."}, nil
		}
		var calls []providers.ToolCall
		for i := range n {
			calls = append(calls, providers.ToolCall{ID: fmt.Sprintf("call_%d", i), Name: "inspect_state", Arguments: []byte(`{}`)})
		}
		return providers.Response{ToolCalls: calls}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "reads", core.RunStatusAccepted, subagent)
	counting.reads.Store(0)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	return counting.reads.Load()
}

func TestAToolBatchReadsTheSessionAndItsRulesOnce(t *testing.T) {
	t.Parallel()
	for _, subagent := range []bool{false, true} {
		if one, four := batchReads(t, 1, subagent), batchReads(t, 4, subagent); one != four {
			t.Fatalf("subagent %v: a batch of one call read %d times, of four %d", subagent, one, four)
		}
	}
}
