package controlplane

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func budgetDaemon(t *testing.T, report core.SessionBudgetReport) (*fakeDaemon, *[]core.SessionBudget) {
	var saved []core.SessionBudget
	daemon := newFakeDaemon(t).
		on("GET /v1/sessions/{id}/budget", func(*http.Request) any { return core.SessionBudgetResponse{Budget: report} }).
		on("PUT /v1/sessions/{id}/budget", func(r *http.Request) any {
			budget := decode[core.SessionBudget](r)
			saved = append(saved, budget)
			report.Override = budget
			if budget.Steps != nil {
				report.Steps = *budget.Steps
			}
			return core.SessionBudgetResponse{Budget: report}
		})
	return daemon, &saved
}

func TestBudgetCommandShowsTheEffectiveBudget(t *testing.T) {
	daemon, saved := budgetDaemon(t, core.SessionBudgetReport{SessionID: "s1", Steps: 32, ActiveSeconds: 4 * 3600})

	result := daemon.run("/budget")

	want := "Steps: 32 (default)\nActive time: 4h (default)\nTokens: unlimited (default)"
	if result.Info == nil || !strings.HasPrefix(result.Info.Text, want) || len(*saved) != 0 {
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
		daemon, saved := budgetDaemon(t, core.SessionBudgetReport{SessionID: "s1", Override: core.SessionBudget{Steps: &steps}})

		result := daemon.run(tc.command)

		if result.Info == nil || len(*saved) != 1 || !tc.check((*saved)[0]) {
			t.Errorf("%s: result = %+v saved = %+v", tc.command, result, *saved)
		}
	}
}

func TestBudgetCommandRejectsBadInput(t *testing.T) {
	for _, command := range []string{"/budget steps 0", "/budget time soon", "/budget speed 3"} {
		daemon, saved := budgetDaemon(t, core.SessionBudgetReport{})

		if result := daemon.run(command); result.Text != budgetUsage || len(*saved) != 0 {
			t.Errorf("%s: result = %+v saved = %+v", command, result, *saved)
		}
	}
}
