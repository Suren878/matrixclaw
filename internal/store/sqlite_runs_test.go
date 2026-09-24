package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestRunStopContinuationAndTriggerAreStored(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	run := core.Run{ID: "r2", SessionID: "s1", UserMessageID: "m2", Trigger: core.RunTriggerAutomation, ContinuesRunID: "r1", Status: core.RunStatusRunning, StartedAt: testEpoch, UpdatedAt: testEpoch}
	if err := st.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	run.Status = core.RunStatusCompleted
	run.StopReason = agent.StopBudgetExhausted
	if err := st.UpdateRun(ctx, run); err != nil {
		t.Fatal(err)
	}

	stored, err := st.GetRun(ctx, "r2")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Trigger != core.RunTriggerAutomation || stored.ContinuesRunID != "r1" || stored.StopReason != agent.StopBudgetExhausted {
		t.Fatalf("stored run = %+v", stored)
	}
}

func TestRunCheckpointKeepsEngineState(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	createTestRun(t, st, "s1", "r1")
	checkpoint := core.RunCheckpoint{RunID: "r1", Phase: core.RunCheckpointPhaseModel, EngineState: json.RawMessage(`{"steps":3}`), UpdatedAt: testEpoch}
	if err := st.SaveRunCheckpoint(ctx, checkpoint); err != nil {
		t.Fatal(err)
	}

	stored, err := st.GetRunCheckpoint(ctx, "r1")
	if err != nil || string(stored.EngineState) != `{"steps":3}` {
		t.Fatalf("stored checkpoint = %+v err = %v", stored, err)
	}
}
