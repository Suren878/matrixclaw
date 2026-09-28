package core

import (
	"context"
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
		User:       agent.Budget{Steps: 300, ActiveTime: 4 * time.Hour},
		Subagent:   agent.Budget{Steps: 100, ActiveTime: time.Hour},
		Automation: agent.Budget{Steps: 50, ActiveTime: 30 * time.Minute},
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

// runBudget is the budget a run starts with: the default for what started it with
// the session's overrides applied.
func (c *Core) runBudget(ctx context.Context, run Run, session Session) (agent.Budget, error) {
	override, err := c.store.GetSessionBudget(ctx, session.ID)
	if err != nil {
		return agent.Budget{}, err
	}
	return override.apply(c.defaultRunBudget(run, session)), nil
}

// SessionBudget reports the session's overrides and the budget of its next run.
func (c *Core) SessionBudget(ctx context.Context, sessionID string) (SessionBudgetReport, error) {
	session, err := c.store.GetSession(ctx, normalizeText(sessionID))
	if err != nil {
		return SessionBudgetReport{}, err
	}
	override, err := c.store.GetSessionBudget(ctx, session.ID)
	if err != nil {
		return SessionBudgetReport{}, err
	}
	return c.sessionBudgetReport(session, override), nil
}

// UpdateSessionBudget replaces the session's overrides; an empty budget restores
// the defaults.
func (c *Core) UpdateSessionBudget(ctx context.Context, sessionID string, budget SessionBudget) (SessionBudgetReport, error) {
	if err := budget.validate(); err != nil {
		return SessionBudgetReport{}, err
	}
	session, err := c.store.GetSession(ctx, normalizeText(sessionID))
	if err != nil {
		return SessionBudgetReport{}, err
	}
	if err := c.store.SaveSessionBudget(ctx, session.ID, budget, c.now().UTC()); err != nil {
		return SessionBudgetReport{}, err
	}
	return c.sessionBudgetReport(session, budget), nil
}

func (c *Core) sessionBudgetReport(session Session, override SessionBudget) SessionBudgetReport {
	budget := override.apply(c.defaultRunBudget(Run{}, session))
	return SessionBudgetReport{
		SessionID:     session.ID,
		Override:      override,
		Steps:         budget.Steps,
		ActiveSeconds: int64(budget.ActiveTime / time.Second),
		Tokens:        budget.Tokens,
	}
}
