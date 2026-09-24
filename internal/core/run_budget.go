package core

import (
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
)

// RunBudgets are the daemon's default run budgets per trigger.
type RunBudgets struct {
	User       agent.Budget
	Subagent   agent.Budget
	Automation agent.Budget
}

// DefaultRunBudgets apply when the daemon configuration names none.
func DefaultRunBudgets() RunBudgets {
	return RunBudgets{
		User:       agent.Budget{Steps: 32, ActiveTime: 4 * time.Hour},
		Subagent:   agent.Budget{Steps: 32, ActiveTime: time.Hour},
		Automation: agent.Budget{Steps: 32, ActiveTime: 30 * time.Minute},
	}
}

// WithRunBudgets sets the default run budgets per trigger.
func (c *Core) WithRunBudgets(budgets RunBudgets) *Core {
	c.budgets = budgets
	return c
}

// defaultRunBudget is the daemon default for what started the run.
func (c *Core) defaultRunBudget(run Run, session Session) agent.Budget {
	switch {
	case isSubagentSession(session):
		return c.budgets.Subagent
	case run.Trigger == RunTriggerAutomation:
		return c.budgets.Automation
	default:
		return c.budgets.User
	}
}
