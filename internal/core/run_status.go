package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

const orphanedRunError = "run was left running without an active executor; daemon restarted or the worker event stream was lost. Retry the task to start a fresh run"

func (c *Core) failOrphanedRun(ctx context.Context, run Run) error {
	return c.failRun(ctx, run, errors.New(orphanedRunError))
}

// persistAssistantError seals the reply with the error and fails the run.
func (c *Core) persistAssistantError(ctx context.Context, run Run, assistant *transcript.Message, assistantSaved bool, cause error) error {
	if assistant != nil {
		assistant.Parts = withFinishPart(assistant.Content, transcript.FinishReasonError, cause.Error())
		if err := c.sealAssistant(ctx, assistant, assistantSaved); err != nil {
			cause = fmt.Errorf("%w (assistant save failed: %w)", cause, err)
		}
	}
	return c.failRun(ctx, run, cause)
}

// setRunStatus moves the run to status: it keeps FinishedAt,
// clears the checkpoint of an ended run and publishes the change.
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
	if status.Terminal() {
		finishedAt := run.UpdatedAt
		run.FinishedAt = &finishedAt
	} else {
		run.FinishedAt = nil
	}
	if err := c.store.UpdateRun(ctx, *run); err != nil {
		return err
	}
	if status.Terminal() {
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

// sealAssistant saves the assistant reply: created when it was not saved yet,
// updated otherwise.
func (c *Core) sealAssistant(ctx context.Context, assistant *transcript.Message, saved bool) error {
	now := c.now().UTC()
	if assistant.CreatedAt.IsZero() {
		assistant.CreatedAt = now
	}
	assistant.UpdatedAt = now
	event := EventMessageUpdated
	var err error
	if saved {
		err = c.store.UpdateMessage(ctx, *assistant)
	} else {
		event = EventMessageCreated
		_, err = c.store.AppendMessage(ctx, *assistant)
	}
	if err != nil {
		return err
	}
	c.publishEvent(Event{Type: event, SessionID: assistant.SessionID, RunID: assistant.RunID, Payload: *assistant})
	return nil
}

// withFinishPart is the reply's text followed by a finish part.
func withFinishPart(content string, reason string, message string) []transcript.MessagePart {
	return append(transcript.NormalizeMessageParts(content, nil), transcript.MessagePart{
		Kind:   transcript.MessagePartKindFinish,
		Finish: &transcript.FinishPart{Reason: reason, Message: message},
	})
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
	assistant.Parts = withFinishPart(assistant.Content, transcript.FinishReasonCanceled, "Canceled by user.")
	return c.sealAssistant(ctx, assistant, assistantSaved)
}

func (c *Core) failAcceptedRun(ctx context.Context, result AcceptRunResult, cause error) (AcceptRunResult, error) {
	run := result.Run
	if err := c.setRunStatus(ctx, &run, RunStatusFailed, cause.Error()); err != nil {
		return result, err
	}
	result.Run = run
	return result, cause
}

// failRun ends the run as failed and returns cause.
func (c *Core) failRun(ctx context.Context, run Run, cause error) error {
	if err := c.setRunStatus(ctx, &run, RunStatusFailed, cause.Error()); err != nil {
		return err
	}
	return cause
}
