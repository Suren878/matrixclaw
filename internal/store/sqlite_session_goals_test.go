package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestSessionGoalRoundTripAndActiveList(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	createTestSession(t, st, "s2")
	goal := core.SessionGoal{
		SessionID: "s1", Objective: "Port the parser", CheckCommand: "go test ./...", MaxRuns: 10, TokenBudget: 5000,
		Status: core.GoalActive, RunsUsed: 2, TokensUsed: 900, LastRunID: "run-2", LastStop: "replied and stopped",
		LastCheck: "failed (exit 1)", CheckFailure: "abc", CheckFailures: 2, LastBlocker: "no database", BlockerRepeats: 1,
		BlockerRunID: "run-2", CreatedAt: testEpoch, UpdatedAt: testEpoch,
	}
	if err := st.SaveSessionGoal(ctx, goal); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSessionGoal(ctx, core.SessionGoal{SessionID: "s2", Objective: "Done", MaxRuns: 1, Status: core.GoalComplete, CreatedAt: testEpoch, UpdatedAt: testEpoch}); err != nil {
		t.Fatal(err)
	}

	if stored, err := st.GetSessionGoal(ctx, "s1"); err != nil || !reflect.DeepEqual(stored, goal) {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
	if active, err := st.ListActiveSessionGoals(ctx); err != nil || len(active) != 1 || active[0].SessionID != "s1" {
		t.Fatalf("active = %+v err = %v", active, err)
	}
	if err := st.DeleteSessionGoal(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSessionGoal(ctx, "s1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("deleted goal err = %v", err)
	}
}
