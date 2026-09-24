package core

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/agent"
)

// applyOutcome persists how the engine left a native run.
func (c *Core) applyOutcome(ctx context.Context, run Run, outcome agent.Outcome) error {
	switch outcome.Status {
	case agent.StatusCompleted:
		if outcome.Assistant == nil {
			return nil
		}
		return c.completeAssistantTurn(ctx, &run, run.SessionID, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusWaitingApproval:
		return c.setRunStatus(ctx, &run, RunStatusWaitingApproval, "")
	case agent.StatusCanceled:
		return c.finishCanceledAssistant(ctx, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusFailed:
		if outcome.MarkErrored && outcome.Assistant != nil {
			return c.persistAssistantError(ctx, run, outcome.Assistant, outcome.AssistantSaved, outcome.Err)
		}
		return c.failRunByID(ctx, run, outcome.Err)
	case agent.StatusInterrupted:
		return c.applyInterruptedOutcome(run, outcome)
	default:
		return fmt.Errorf("core: unknown run outcome %q", outcome.Status)
	}
}

// applyInterruptedOutcome commits what the run reached before its context stopped,
// or keeps it running with a recovery checkpoint.
func (c *Core) applyInterruptedOutcome(run Run, outcome agent.Outcome) error {
	ctx, cancel := context.WithTimeout(context.Background(), runInterruptionPersistenceTimeout)
	defer cancel()
	latest, err := c.store.GetRun(ctx, run.ID)
	if err != nil {
		return err
	}
	if latest.Status == RunStatusCanceled {
		return c.finishCanceledAssistant(ctx, outcome.Assistant, outcome.AssistantSaved)
	}
	if subagentRunStatusTerminal(latest.Status) {
		return nil
	}
	switch outcome.Reached {
	case agent.StatusCompleted:
		return c.completeAssistantTurn(ctx, &latest, latest.SessionID, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusWaitingApproval:
		pending, err := c.runHasPendingApprovals(ctx, latest.SessionID, latest.ID)
		if err != nil {
			return err
		}
		if pending {
			return c.setRunStatus(ctx, &latest, RunStatusWaitingApproval, "")
		}
	}
	return c.preserveRunForRecovery(ctx, latest, outcome.Assistant, outcome.AssistantSaved)
}
