package controlplane

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

type budgetRuntime struct {
	tokenReportRuntime
	report core.SessionBudgetReport
	saved  []core.SessionBudget
}

func (r *budgetRuntime) SessionBudget(context.Context, string) (core.SessionBudgetReport, error) {
	return r.report, nil
}

func (r *budgetRuntime) UpdateSessionBudget(_ context.Context, _ string, budget core.SessionBudget) (core.SessionBudgetReport, error) {
	r.saved = append(r.saved, budget)
	r.report.Override = budget
	if budget.Steps != nil {
		r.report.Steps = *budget.Steps
	}
	return r.report, nil
}

func TestBudgetCommandShowsTheEffectiveBudget(t *testing.T) {
	runtime := &budgetRuntime{report: core.SessionBudgetReport{SessionID: "s1", Steps: 32, ActiveSeconds: 4 * 3600}}

	result, err := New(runtime, "").Handle(context.Background(), "key", "/budget")

	if err != nil {
		t.Fatal(err)
	}
	want := "Steps: 32 (default)\nActive time: 4h (default)\nTokens: unlimited (default)"
	if result.Info == nil || !strings.HasPrefix(result.Info.Text, want) || len(runtime.saved) != 0 {
		t.Fatalf("info = %+v, want text starting with:\n%s", result.Info, want)
	}
}

func TestBudgetCommandChangesOneLimit(t *testing.T) {
	steps := 50
	for _, tc := range []struct {
		command string
		check   func(core.SessionBudget) bool
	}{
		{"/budget steps 50", func(b core.SessionBudget) bool { return b.Steps != nil && *b.Steps == 50 }},
		{"/budget time 90m", func(b core.SessionBudget) bool {
			return b.ActiveSeconds != nil && *b.ActiveSeconds == 5400 && b.Steps != nil
		}},
		{"/budget tokens off", func(b core.SessionBudget) bool { return b.Tokens != nil && *b.Tokens == 0 }},
		{"/budget reset", func(b core.SessionBudget) bool { return b.IsZero() }},
	} {
		runtime := &budgetRuntime{report: core.SessionBudgetReport{SessionID: "s1", Override: core.SessionBudget{Steps: &steps}}}

		result, err := New(runtime, "").Handle(context.Background(), "key", tc.command)

		if err != nil || result.Info == nil || len(runtime.saved) != 1 || !tc.check(runtime.saved[0]) {
			t.Errorf("%s: result = %+v saved = %+v err = %v", tc.command, result, runtime.saved, err)
		}
	}
}

func TestBudgetCommandRejectsBadInput(t *testing.T) {
	for _, command := range []string{"/budget steps 0", "/budget time soon", "/budget speed 3"} {
		runtime := &budgetRuntime{}

		result, err := New(runtime, "").Handle(context.Background(), "key", command)

		if err != nil || result.Text != budgetUsage || len(runtime.saved) != 0 {
			t.Errorf("%s: result = %+v saved = %+v err = %v", command, result, runtime.saved, err)
		}
	}
}
