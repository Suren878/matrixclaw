package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestRunStopContinuationAndTriggerAreStored(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	run := core.Run{ID: "r2", SessionID: "s1", UserMessageID: "m2", Trigger: core.RunTriggerAutomation, ContinuesRunID: "r1", Status: core.RunStatusRunning, StartedAt: testEpoch, UpdatedAt: testEpoch}
	acceptTestRun(t, st, run)
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
	checkpoint := core.RunCheckpoint{RunID: "r1", EngineState: json.RawMessage(`{"steps":3}`), UpdatedAt: testEpoch}
	if err := st.SaveRunCheckpoint(ctx, checkpoint); err != nil {
		t.Fatal(err)
	}

	stored, err := st.GetRunCheckpoint(ctx, "r1")
	if err != nil || string(stored.EngineState) != `{"steps":3}` {
		t.Fatalf("stored checkpoint = %+v err = %v", stored, err)
	}
}

func TestRunCheckpointKeepsTheToolBatch(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	createTestRun(t, st, "s1", "r1")
	batch := &agent.ToolBatch{CallIDs: []string{"c1", "c2", "c3"}, DeferredIDs: []string{"c3"}}
	if err := st.SaveRunCheckpoint(ctx, core.RunCheckpoint{RunID: "r1", Batch: batch, UpdatedAt: testEpoch}); err != nil {
		t.Fatal(err)
	}
	stored, err := st.GetRunCheckpoint(ctx, "r1")
	if err != nil || !reflect.DeepEqual(stored.Batch, batch) {
		t.Fatalf("stored checkpoint = %+v err = %v", stored, err)
	}

	if err := st.SaveRunCheckpoint(ctx, core.RunCheckpoint{RunID: "r1", UpdatedAt: testEpoch}); err != nil {
		t.Fatal(err)
	}
	if stored, err := st.GetRunCheckpoint(ctx, "r1"); err != nil || stored.Batch != nil {
		t.Fatalf("checkpoint after the batch = %+v err = %v", stored, err)
	}
}

func TestLatestRunFollowsTheUserMessageOrder(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	if _, err := st.GetLatestRunBySession(ctx, "s1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("empty session error = %v, want ErrNotFound", err)
	}
	for _, id := range []string{"r1", "r2"} {
		message := transcript.Message{ID: "m_" + id, SessionID: "s1", RunID: id, Role: transcript.MessageRoleUser, Content: id, CreatedAt: testEpoch}
		run := core.Run{ID: id, SessionID: "s1", UserMessageID: message.ID, Status: core.RunStatusCompleted, StartedAt: testEpoch, UpdatedAt: testEpoch}
		if err := st.AcceptMessage(ctx, message, run); err != nil {
			t.Fatal(err)
		}
	}

	latest, err := st.GetLatestRunBySession(ctx, "s1")
	if err != nil || latest.ID != "r2" {
		t.Fatalf("latest = %+v err = %v", latest, err)
	}
}
