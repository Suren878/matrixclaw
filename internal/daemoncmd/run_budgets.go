package daemoncmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

// runBudgetsFromConfig applies the configured budget fields over the built-in defaults.
func runBudgetsFromConfig(cfg setup.RunBudgetsConfig) (core.RunBudgets, error) {
	budgets := core.DefaultRunBudgets()
	var err error
	if budgets.User, err = applyRunBudgetConfig("user", budgets.User, cfg.User); err != nil {
		return core.RunBudgets{}, err
	}
	if budgets.Subagent, err = applyRunBudgetConfig("subagent", budgets.Subagent, cfg.Subagent); err != nil {
		return core.RunBudgets{}, err
	}
	if budgets.Automation, err = applyRunBudgetConfig("automation", budgets.Automation, cfg.Automation); err != nil {
		return core.RunBudgets{}, err
	}
	return budgets, nil
}

func applyRunBudgetConfig(name string, budget agent.Budget, cfg setup.RunBudgetConfig) (agent.Budget, error) {
	if cfg.Steps < 0 || cfg.Tokens < 0 {
		return budget, fmt.Errorf("daemon.budgets.%s: steps and tokens must not be negative", name)
	}
	if cfg.Steps > 0 {
		budget.Steps = cfg.Steps
	}
	if cfg.Tokens > 0 {
		budget.Tokens = cfg.Tokens
	}
	if value := strings.TrimSpace(cfg.ActiveTime); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil || duration < time.Minute {
			return budget, fmt.Errorf("daemon.budgets.%s.active_time: want a duration of at least 1m, got %q", name, value)
		}
		budget.ActiveTime = duration
	}
	return budget, nil
}
