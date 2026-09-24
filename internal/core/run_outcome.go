package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// applyOutcome persists how the engine left a native run and reports whether
// the run was kept for recovery.
func (c *Core) applyOutcome(ctx context.Context, run Run, outcome agent.Outcome) (bool, error) {
	switch outcome.Status {
	case agent.StatusCompleted:
		if outcome.Assistant == nil {
			return false, nil
		}
		run.StopReason = outcome.StopReason
		return false, c.completeAssistantTurn(ctx, &run, run.SessionID, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusWaitingApproval:
		return false, c.setRunStatus(ctx, &run, RunStatusWaitingApproval, "")
	case agent.StatusCanceled:
		return false, c.finishCanceledAssistant(ctx, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusFailed:
		if outcome.MarkErrored && outcome.Assistant != nil {
			return false, c.persistAssistantError(ctx, run, outcome.Assistant, outcome.AssistantSaved, outcome.Err)
		}
		return false, c.failRunByID(ctx, run, outcome.Err)
	case agent.StatusInterrupted:
		return c.applyInterruptedOutcome(run, outcome)
	default:
		return false, fmt.Errorf("core: unknown run outcome %q", outcome.Status)
	}
}

// applyInterruptedOutcome commits what the run reached before its context stopped,
// or keeps it running with a recovery checkpoint (reported as true).
func (c *Core) applyInterruptedOutcome(run Run, outcome agent.Outcome) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runInterruptionPersistenceTimeout)
	defer cancel()
	latest, err := c.store.GetRun(ctx, run.ID)
	if err != nil {
		return false, err
	}
	if latest.Status == RunStatusCanceled {
		return false, c.finishCanceledAssistant(ctx, outcome.Assistant, outcome.AssistantSaved)
	}
	if subagentRunStatusTerminal(latest.Status) {
		return false, nil
	}
	switch outcome.Reached {
	case agent.StatusCompleted:
		latest.StopReason = outcome.StopReason
		return false, c.completeAssistantTurn(ctx, &latest, latest.SessionID, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusWaitingApproval:
		pending, err := c.runHasPendingApprovals(ctx, latest.SessionID, latest.ID)
		if err != nil {
			return false, err
		}
		if pending {
			return false, c.setRunStatus(ctx, &latest, RunStatusWaitingApproval, "")
		}
	}
	if err := c.preserveRunForRecovery(ctx, latest, outcome.Assistant, outcome.AssistantSaved); err != nil {
		return false, err
	}
	current, err := c.store.GetRun(ctx, latest.ID)
	if err != nil {
		return false, err
	}
	return !subagentRunStatusTerminal(current.Status), nil
}

func (c *Core) completeAssistantTurn(ctx context.Context, run *Run, sessionID string, assistant *transcript.Message, assistantSaved bool) error {
	if run == nil || assistant == nil {
		return errors.New("core: complete assistant turn requires run and assistant")
	}
	finishedAt := c.now().UTC()
	if err := c.CompleteSessionPlanRunStep(ctx, *run, assistant.Content); err != nil {
		return fmt.Errorf("complete plan run step: %w", err)
	}
	if err := c.completeActivePlanItemsIfRunFinished(ctx, sessionID); err != nil {
		return fmt.Errorf("complete active plan items: %w", err)
	}
	if !assistantSaved {
		assistant.CreatedAt = finishedAt
		assistant.UpdatedAt = finishedAt
		run.Status = RunStatusCompleted
		run.Error = ""
		run.FinishedAt = &finishedAt
		run.UpdatedAt = finishedAt

		if err := c.store.CompleteRun(ctx, *assistant, *run); err != nil {
			return fmt.Errorf("complete run: %w", err)
		}
		c.clearRunCheckpoint(ctx, run.ID)
		c.publishEvent(Event{
			Type:      EventMessageCreated,
			SessionID: sessionID,
			RunID:     run.ID,
			Payload:   *assistant,
		})
		c.publishEvent(Event{
			Type:      EventRunUpdated,
			SessionID: sessionID,
			RunID:     run.ID,
			Payload:   *run,
		})
		return nil
	}

	assistant.UpdatedAt = finishedAt
	run.Status = RunStatusCompleted
	run.Error = ""
	run.FinishedAt = &finishedAt
	run.UpdatedAt = finishedAt

	if err := c.store.UpdateMessage(ctx, *assistant); err != nil {
		return fmt.Errorf("update assistant message: %w", err)
	}
	if err := c.store.UpdateRun(ctx, *run); err != nil {
		return err
	}
	c.clearRunCheckpoint(ctx, run.ID)
	c.publishEvent(Event{
		Type:      EventMessageUpdated,
		SessionID: sessionID,
		RunID:     run.ID,
		Payload:   *assistant,
	})
	c.publishEvent(Event{
		Type:      EventRunUpdated,
		SessionID: sessionID,
		RunID:     run.ID,
		Payload:   *run,
	})
	return nil
}
