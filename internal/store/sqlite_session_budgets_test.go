package store_test

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestSessionBudgetOverridesRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	steps, tokens := 7, int64(0)
	if err := st.SaveSessionBudget(ctx, "s1", core.SessionBudget{Steps: &steps, Tokens: &tokens}, testEpoch); err != nil {
		t.Fatal(err)
	}

	stored, err := st.GetSessionBudget(ctx, "s1")
	if err != nil || stored.Steps == nil || *stored.Steps != 7 || stored.ActiveSeconds != nil || stored.Tokens == nil || *stored.Tokens != 0 {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
	if err := st.SaveSessionBudget(ctx, "s1", core.SessionBudget{}, testEpoch); err != nil {
		t.Fatal(err)
	}
	if cleared, err := st.GetSessionBudget(ctx, "s1"); err != nil || !cleared.IsZero() {
		t.Fatalf("cleared = %+v err = %v", cleared, err)
	}
}
