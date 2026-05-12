package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (c *Core) AcceptRun(ctx context.Context, input HandleMessageInput) (AcceptRunResult, error) {
	text := normalizeText(input.Text)
	parts := NormalizeMessageParts(text, input.Parts)
	if text == "" && !messagePartsHaveUserContent(parts) {
		return AcceptRunResult{}, fmt.Errorf("%w: message text is required", ErrInvalidInput)
	}

	session, err := c.resolveSession(ctx, input)
	if err != nil {
		return AcceptRunResult{}, err
	}

	now := c.now().UTC()
	runID := c.newID("run")
	messageID := c.newID("msg")

	message := Message{
		ID:        messageID,
		SessionID: session.ID,
		RunID:     runID,
		Role:      MessageRoleUser,
		Content:   text,
		Parts:     parts,
		CreatedAt: now,
		UpdatedAt: now,
	}
	run := Run{
		ID:            runID,
		SessionID:     session.ID,
		UserMessageID: messageID,
		Client:        normalizeText(input.Client),
		ExternalKey:   normalizeText(input.ExternalKey),
		Status:        RunStatusAccepted,
		StartedAt:     now,
		UpdatedAt:     now,
	}
	if err := c.store.AcceptMessage(ctx, message, run); err != nil {
		return AcceptRunResult{}, err
	}
	c.publishEvent(Event{
		Type:      EventMessageCreated,
		SessionID: session.ID,
		RunID:     run.ID,
		Payload:   message,
	})
	c.publishEvent(Event{
		Type:      EventRunUpdated,
		SessionID: session.ID,
		RunID:     run.ID,
		Payload:   run,
	})

	result := AcceptRunResult{
		SessionID:   session.ID,
		UserMessage: message,
		Run:         run,
	}

	// The daemon hands execution off here; transport is already out of the picture.
	if err := c.startRun(ctx, run.ID); err != nil {
		return c.failAcceptedRun(ctx, result, err)
	}

	return result, nil
}

func messagePartsHaveUserContent(parts []MessagePart) bool {
	for _, part := range parts {
		if part.Text != nil && strings.TrimSpace(part.Text.Text) != "" {
			return true
		}
		if part.Image != nil && (strings.TrimSpace(part.Image.DataBase64) != "" || strings.TrimSpace(part.Image.StoragePath) != "") {
			return true
		}
	}
	return false
}

func (c *Core) AcceptTriggeredRun(ctx context.Context, input HandleTriggeredRunInput) (AcceptRunResult, error) {
	text := normalizeText(input.Text)
	if text == "" {
		return AcceptRunResult{}, fmt.Errorf("%w: message text is required", ErrInvalidInput)
	}
	triggerID := normalizeText(input.TriggerID)
	if triggerID == "" {
		return AcceptRunResult{}, fmt.Errorf("%w: trigger id is required", ErrInvalidInput)
	}
	session, err := c.resolveSession(ctx, HandleMessageInput{
		SessionID:  normalizeText(input.SessionID),
		WorkingDir: input.WorkingDir,
	})
	if err != nil {
		return AcceptRunResult{}, err
	}

	runID := deterministicRunID(triggerID)
	messageID := deterministicMessageID(triggerID)
	if existing, err := c.store.GetRun(ctx, runID); err == nil {
		message := Message{ID: existing.UserMessageID, SessionID: existing.SessionID, RunID: existing.ID, Role: MessageRoleUser, Content: text}
		if messages, listErr := c.store.ListMessages(ctx, existing.SessionID, 0); listErr == nil {
			for _, candidate := range messages {
				if candidate.ID == existing.UserMessageID {
					message = candidate
					break
				}
			}
		}
		return AcceptRunResult{SessionID: existing.SessionID, UserMessage: message, Run: existing}, nil
	} else if !errors.Is(err, ErrNotFound) {
		return AcceptRunResult{}, err
	}

	now := c.now().UTC()
	message := Message{
		ID:        messageID,
		SessionID: session.ID,
		RunID:     runID,
		Role:      MessageRoleUser,
		Content:   text,
		Parts:     NormalizeMessageParts(text, nil),
		CreatedAt: now,
		UpdatedAt: now,
	}
	run := Run{
		ID:            runID,
		SessionID:     session.ID,
		UserMessageID: messageID,
		Client:        normalizeText(input.Client),
		ExternalKey:   normalizeText(input.ExternalKey),
		Status:        RunStatusAccepted,
		StartedAt:     now,
		UpdatedAt:     now,
	}
	if err := c.store.AcceptMessage(ctx, message, run); err != nil {
		if existing, loadErr := c.store.GetRun(ctx, runID); loadErr == nil {
			return AcceptRunResult{SessionID: existing.SessionID, UserMessage: message, Run: existing}, nil
		}
		return AcceptRunResult{}, err
	}
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: session.ID, RunID: run.ID, Payload: message})
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: session.ID, RunID: run.ID, Payload: run})
	result := AcceptRunResult{SessionID: session.ID, UserMessage: message, Run: run}
	if err := c.startRun(ctx, run.ID); err != nil {
		return c.failAcceptedRun(ctx, result, err)
	}
	return result, nil
}

func (c *Core) startRun(ctx context.Context, runID string) error {
	runID = normalizeText(runID)
	if runID == "" {
		return fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}
	if c.runStarter == nil {
		return fmt.Errorf("%w: run starter not configured", ErrExecutionUnavailable)
	}
	return c.runStarter.StartRun(ctx, runID)
}

func deterministicRunID(triggerID string) string {
	return "run_" + stableIDPart(triggerID)
}

func deterministicMessageID(triggerID string) string {
	return "msg_" + stableIDPart(triggerID)
}

func stableIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "automation"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func (c *Core) GetRun(ctx context.Context, runID string) (Run, error) {
	if normalizeText(runID) == "" {
		return Run{}, fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}
	return c.store.GetRun(ctx, normalizeText(runID))
}

func (c *Core) CancelRun(ctx context.Context, runID string) (Run, error) {
	runID = normalizeText(runID)
	if runID == "" {
		return Run{}, fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}

	run, err := c.store.GetRun(ctx, runID)
	if err != nil {
		return Run{}, err
	}

	switch run.Status {
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
		return run, nil
	}

	if run.SessionID != "" {
		approvals, err := c.store.ListApprovals(ctx, run.SessionID, ApprovalStatePending)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return Run{}, err
		}
		for _, approval := range approvals {
			if approval.RunID != run.ID {
				continue
			}
			approval.State = ApprovalStateRejected
			decidedAt := c.now().UTC()
			approval.DecidedAt = &decidedAt
			if err := c.store.UpdateApproval(ctx, approval); err != nil {
				return Run{}, err
			}
			c.publishEvent(Event{
				Type:      EventApprovalResult,
				SessionID: approval.SessionID,
				RunID:     approval.RunID,
				Payload: PermissionNotification{
					ApprovalID: approval.ID,
					ToolCallID: approval.ToolCallRef,
					Granted:    false,
					Denied:     true,
				},
			})
			c.publishEvent(Event{
				Type:      EventToolUpdated,
				SessionID: approval.SessionID,
				RunID:     approval.RunID,
				Payload: ToolUpdate{
					ToolCallID: approval.ToolCallRef,
					ToolName:   approval.ToolName,
					State:      ToolLifecycleFailed,
					RunID:      approval.RunID,
					SessionID:  approval.SessionID,
					ApprovalID: approval.ID,
					Error:      "canceled by user",
				},
			})
		}
	}

	if err := c.setRunStatus(ctx, &run, RunStatusCanceled, "canceled by user"); err != nil {
		return Run{}, err
	}
	c.cancelActiveRun(run.ID)
	return run, nil
}
