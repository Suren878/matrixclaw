package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

const orphanedRunError = "run was left running without an active executor; daemon restarted or the worker event stream was lost. Retry the task to start a fresh run"

func (c *Core) failOrphanedRun(ctx context.Context, run Run) error {
	return c.failRunByID(ctx, run, errors.New(orphanedRunError))
}

func (c *Core) persistAssistantError(ctx context.Context, run Run, assistant *transcript.Message, assistantSaved bool, cause error) error {
	if assistantSaved {
		if updateErr := c.markAssistantErrored(ctx, assistant, cause); updateErr != nil {
			return c.failRunByID(ctx, run, fmt.Errorf("%w (assistant update failed: %w)", cause, updateErr))
		}
	} else {
		if saveErr := c.saveAssistantErrored(ctx, assistant, cause); saveErr != nil {
			return c.failRunByID(ctx, run, fmt.Errorf("%w (assistant save failed: %w)", cause, saveErr))
		}
	}
	return c.failRunByID(ctx, run, cause)
}

func (c *Core) setRunStatus(ctx context.Context, run *Run, status RunStatus, errText string) error {
	if run == nil {
		return nil
	}
	if run.Status == status && run.Error == errText {
		return nil
	}
	run.Status = status
	run.Error = errText
	run.UpdatedAt = c.now().UTC()
	if subagentRunStatusTerminal(status) {
		finishedAt := run.UpdatedAt
		run.FinishedAt = &finishedAt
	} else {
		run.FinishedAt = nil
	}
	if err := c.store.UpdateRun(ctx, *run); err != nil {
		return err
	}
	if subagentRunStatusTerminal(status) {
		c.clearRunCheckpoint(ctx, run.ID)
	}
	c.publishEvent(Event{
		Type:      EventRunUpdated,
		SessionID: run.SessionID,
		RunID:     run.ID,
		Payload:   *run,
	})
	return nil
}

func (c *Core) markAssistantErrored(ctx context.Context, assistant *transcript.Message, cause error) error {
	if assistant == nil {
		return nil
	}
	assistant.UpdatedAt = c.now().UTC()
	assistant.Parts = appendErrorFinishPart(assistant.Content, cause.Error())
	if err := c.store.UpdateMessage(ctx, *assistant); err != nil {
		return err
	}
	c.publishEvent(Event{
		Type:      EventMessageUpdated,
		SessionID: assistant.SessionID,
		RunID:     assistant.RunID,
		Payload:   *assistant,
	})
	return nil
}

func (c *Core) saveAssistantErrored(ctx context.Context, assistant *transcript.Message, cause error) error {
	if assistant == nil {
		return nil
	}
	now := c.now().UTC()
	if assistant.CreatedAt.IsZero() {
		assistant.CreatedAt = now
	}
	assistant.UpdatedAt = now
	assistant.Parts = appendErrorFinishPart(assistant.Content, cause.Error())
	if _, err := c.store.AppendMessage(ctx, *assistant); err != nil {
		return err
	}
	c.publishEvent(Event{
		Type:      EventMessageCreated,
		SessionID: assistant.SessionID,
		RunID:     assistant.RunID,
		Payload:   *assistant,
	})
	return nil
}

func appendErrorFinishPart(content string, message string) []transcript.MessagePart {
	parts := transcript.NormalizeMessageParts(content, nil)
	parts = append(parts, transcript.MessagePart{
		Kind: transcript.MessagePartKindFinish,
		Finish: &transcript.FinishPart{
			Reason:  transcript.FinishReasonError,
			Message: message,
		},
	})
	return parts
}

func appendCanceledFinishPart(content string, message string) []transcript.MessagePart {
	parts := transcript.NormalizeMessageParts(content, nil)
	parts = append(parts, transcript.MessagePart{
		Kind: transcript.MessagePartKindFinish,
		Finish: &transcript.FinishPart{
			Reason:  transcript.FinishReasonCanceled,
			Message: message,
		},
	})
	return parts
}

func (c *Core) isRunCanceled(ctx context.Context, runID string) (bool, error) {
	runID = normalizeText(runID)
	if runID == "" {
		return false, nil
	}
	run, err := c.store.GetRun(ctx, runID)
	if err != nil {
		return false, err
	}
	return run.Status == RunStatusCanceled, nil
}

func (c *Core) finishCanceledAssistant(ctx context.Context, assistant *transcript.Message, assistantSaved bool) error {
	if assistant == nil {
		return nil
	}
	message := "Canceled by user."
	now := c.now().UTC()
	if assistantSaved {
		assistant.UpdatedAt = now
		assistant.Parts = appendCanceledFinishPart(assistant.Content, message)
		if err := c.store.UpdateMessage(ctx, *assistant); err != nil {
			return err
		}
		c.publishEvent(Event{
			Type:      EventMessageUpdated,
			SessionID: assistant.SessionID,
			RunID:     assistant.RunID,
			Payload:   *assistant,
		})
		return nil
	}
	if assistant.CreatedAt.IsZero() {
		assistant.CreatedAt = now
	}
	assistant.UpdatedAt = now
	assistant.Parts = appendCanceledFinishPart(assistant.Content, message)
	if _, err := c.store.AppendMessage(ctx, *assistant); err != nil {
		return err
	}
	c.publishEvent(Event{
		Type:      EventMessageCreated,
		SessionID: assistant.SessionID,
		RunID:     assistant.RunID,
		Payload:   *assistant,
	})
	return nil
}

func (c *Core) failAcceptedRun(ctx context.Context, result AcceptRunResult, cause error) (AcceptRunResult, error) {
	if err := c.failRunByID(ctx, result.Run, cause); err != nil {
		return result, err
	}

	result.Run.Status = RunStatusFailed
	result.Run.Error = cause.Error()
	finishedAt := c.now().UTC()
	result.Run.FinishedAt = &finishedAt
	result.Run.UpdatedAt = finishedAt
	return result, cause
}

func (c *Core) failRunByID(ctx context.Context, run Run, cause error) error {
	finishedAt := c.now().UTC()
	run.Status = RunStatusFailed
	run.Error = cause.Error()
	run.FinishedAt = &finishedAt
	run.UpdatedAt = finishedAt

	if err := c.store.UpdateRun(ctx, run); err != nil {
		return err
	}
	c.clearRunCheckpoint(ctx, run.ID)
	c.publishEvent(Event{
		Type:      EventRunUpdated,
		SessionID: run.SessionID,
		RunID:     run.ID,
		Payload:   run,
	})
	return cause
}
