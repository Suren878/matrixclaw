package daemoncmd

import (
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestRunBudgetsFromConfigOverrideOnlyTheGivenFields(t *testing.T) {
	budgets, err := runBudgetsFromConfig(setup.RunBudgetsConfig{
		User:       setup.RunBudgetConfig{Steps: 300, ActiveTime: "2h"},
		Automation: setup.RunBudgetConfig{Tokens: 50000},
	})
	if err != nil {
		t.Fatal(err)
	}
	defaults := core.DefaultRunBudgets()
	if budgets.User.Steps != 300 || budgets.User.ActiveTime != 2*time.Hour || budgets.User.Tokens != 0 {
		t.Fatalf("user budget = %+v", budgets.User)
	}
	if budgets.Subagent != defaults.Subagent {
		t.Fatalf("subagent budget = %+v, want the default %+v", budgets.Subagent, defaults.Subagent)
	}
	if budgets.Automation.Tokens != 50000 || budgets.Automation.Steps != defaults.Automation.Steps || budgets.Automation.ActiveTime != defaults.Automation.ActiveTime {
		t.Fatalf("automation budget = %+v", budgets.Automation)
	}
}

func TestRunBudgetsFromConfigRejectsBadValues(t *testing.T) {
	for _, cfg := range []setup.RunBudgetsConfig{
		{User: setup.RunBudgetConfig{ActiveTime: "soon"}},
		{Subagent: setup.RunBudgetConfig{ActiveTime: "10s"}},
		{Automation: setup.RunBudgetConfig{Steps: -1}},
	} {
		if _, err := runBudgetsFromConfig(cfg); err == nil {
			t.Errorf("config %+v accepted", cfg)
		}
	}
}
