package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

func (c *Core) tryExecuteExternalAgentRun(ctx context.Context, runID string) (bool, error) {
	if c.externalStore == nil {
		return false, nil
	}

	run, err := c.store.GetRun(ctx, normalizeText(runID))
	if err != nil {
		return false, err
	}
	switch run.Status {
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled, RunStatusRunning:
		return true, nil
	}

	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		return true, c.failRunByID(ctx, run, err)
	}
	attachment, err := c.externalStore.GetExternalAgentSession(ctx, session.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return true, c.failRunByID(ctx, run, err)
	}
	if _, err := c.externalRuntime(attachment.AgentID); err != nil {
		return true, c.failRunByID(ctx, run, err)
	}

	if err := c.setRunStatus(ctx, &run, RunStatusRunning, ""); err != nil {
		return true, err
	}

	runCtx, unregisterRun := c.activeRunContext(ctx, run.ID)
	defer unregisterRun()

	return true, c.executeExternalAgentRun(ctx, runCtx, run, attachment)
}

func (c *Core) externalRuntime(agentID string) (externalagents.RuntimeAgent, error) {
	if c.externalAgents == nil {
		return nil, fmt.Errorf("%w: external agent registry unavailable", ErrExecutionUnavailable)
	}
	agent, ok := c.externalAgents.Get(agentID)
	if !ok {
		return nil, fmt.Errorf("%w: external agent %q is not configured", ErrExecutionUnavailable, agentID)
	}
	runtime, ok := agent.(externalagents.RuntimeAgent)
	if !ok {
		return nil, fmt.Errorf("%w: external agent %q cannot execute runs", ErrExecutionUnavailable, agentID)
	}
	return runtime, nil
}

func (c *Core) executeExternalAgentRun(ctx context.Context, runCtx context.Context, run Run, attachment externalagents.SessionAttachment) error {
	runtime, err := c.externalRuntime(attachment.AgentID)
	if err != nil {
		return c.failRunByID(ctx, run, err)
	}
	userMessage, err := c.findRunUserMessage(ctx, run)
	if err != nil {
		return c.failRunByID(ctx, run, err)
	}
	externalSession := externalagents.ExternalSession{
		AgentID:           attachment.AgentID,
		ExternalThreadID:  attachment.ExternalThreadID,
		ExternalSessionID: attachment.ExternalSessionID,
		CWD:               attachment.CWD,
		Model:             attachment.Model,
		ApprovalPolicy:    attachment.ApprovalPolicy,
		Sandbox:           attachment.Sandbox,
	}
	events, err := runtime.Send(runCtx, externalSession, externalagents.Input{Text: userMessage.Content})
	if err != nil {
		return c.failRunByID(ctx, run, err)
	}

	assistant := Message{
		ID:        c.newID("msg"),
		SessionID: run.SessionID,
		RunID:     run.ID,
		Role:      MessageRoleAssistant,
		Model:     attachment.Model,
		Provider:  attachment.AgentID,
	}
	assistantSaved := false
	for event := range events {
		if handled, cancelErr := c.checkExternalRunCanceled(ctx, run, &assistant, assistantSaved, runtime, externalSession); handled {
			return cancelErr
		}
		switch event.Kind {
		case externalagents.EventMessageDelta:
			if err := c.applyExternalMessageDelta(ctx, &assistant, &assistantSaved, event.Text); err != nil {
				return c.failRunByID(ctx, run, err)
			}
		case externalagents.EventReasoningDelta:
			if err := c.applyExternalReasoningDelta(ctx, &assistant, &assistantSaved, event.Text); err != nil {
				return c.failRunByID(ctx, run, err)
			}
		case externalagents.EventTurnCompleted:
			return c.completeExternalAgentRun(ctx, &run, &assistant, assistantSaved)
		case externalagents.EventTurnFailed:
			if handled, cancelErr := c.checkExternalRunCanceled(ctx, run, &assistant, assistantSaved, runtime, externalSession); handled {
				return cancelErr
			}
			return c.persistAssistantError(ctx, run, &assistant, assistantSaved, errors.New(event.Error))
		}
	}
	if handled, cancelErr := c.checkExternalRunCanceled(ctx, run, &assistant, assistantSaved, runtime, externalSession); handled {
		return cancelErr
	}
	return c.failRunByID(ctx, run, errors.New("external agent event stream ended before turn completed"))
}

func (c *Core) findRunUserMessage(ctx context.Context, run Run) (Message, error) {
	messages, err := c.store.ListMessages(ctx, run.SessionID, 0)
	if err != nil {
		return Message{}, err
	}
	for _, message := range messages {
		if message.ID == run.UserMessageID {
			return message, nil
		}
	}
	return Message{}, ErrNotFound
}

func (c *Core) applyExternalMessageDelta(ctx context.Context, assistant *Message, saved *bool, delta string) error {
	if assistant == nil || saved == nil {
		return nil
	}
	if delta == "" && *saved {
		return nil
	}
	now := c.now().UTC()
	assistant.Content += delta
	assistant.Parts = NormalizeMessageParts(assistant.Content, nil)
	if !*saved {
		assistant.CreatedAt = now
		assistant.UpdatedAt = now
		if err := c.store.SaveMessage(ctx, *assistant); err != nil {
			return err
		}
		*saved = true
		c.publishEvent(Event{Type: EventMessageCreated, SessionID: assistant.SessionID, RunID: assistant.RunID, Payload: *assistant})
		return nil
	}
	assistant.UpdatedAt = now
	if err := c.store.UpdateMessage(ctx, *assistant); err != nil {
		return err
	}
	c.publishEvent(Event{Type: EventMessageUpdated, SessionID: assistant.SessionID, RunID: assistant.RunID, Payload: *assistant})
	return nil
}

func (c *Core) applyExternalReasoningDelta(ctx context.Context, assistant *Message, saved *bool, delta string) error {
	if strings.TrimSpace(delta) == "" {
		return nil
	}
	if assistant == nil || saved == nil {
		return nil
	}
	now := c.now().UTC()
	assistant.Parts = append(assistant.Parts, MessagePart{
		Kind:      MessagePartKindReasoning,
		Reasoning: &ReasoningPart{Text: delta},
	})
	if !*saved {
		assistant.CreatedAt = now
		assistant.UpdatedAt = now
		if err := c.store.SaveMessage(ctx, *assistant); err != nil {
			return err
		}
		*saved = true
		c.publishEvent(Event{Type: EventMessageCreated, SessionID: assistant.SessionID, RunID: assistant.RunID, Payload: *assistant})
		return nil
	}
	assistant.UpdatedAt = now
	if err := c.store.UpdateMessage(ctx, *assistant); err != nil {
		return err
	}
	c.publishEvent(Event{Type: EventMessageUpdated, SessionID: assistant.SessionID, RunID: assistant.RunID, Payload: *assistant})
	return nil
}

func (c *Core) completeExternalAgentRun(ctx context.Context, run *Run, assistant *Message, assistantSaved bool) error {
	if assistant == nil || run == nil {
		return nil
	}
	finishedAt := c.now().UTC()
	if assistant.Parts == nil {
		assistant.Parts = NormalizeMessageParts(assistant.Content, nil)
	}
	assistant.Parts = append(assistant.Parts, MessagePart{
		Kind:   MessagePartKindFinish,
		Finish: &FinishPart{Reason: "end_turn"},
	})
	run.Status = RunStatusCompleted
	run.Error = ""
	run.FinishedAt = &finishedAt
	run.UpdatedAt = finishedAt
	if !assistantSaved {
		assistant.CreatedAt = finishedAt
		assistant.UpdatedAt = finishedAt
		if err := c.store.CompleteRun(ctx, *assistant, *run); err != nil {
			return err
		}
		c.publishEvent(Event{Type: EventMessageCreated, SessionID: run.SessionID, RunID: run.ID, Payload: *assistant})
		c.publishEvent(Event{Type: EventRunUpdated, SessionID: run.SessionID, RunID: run.ID, Payload: *run})
		return nil
	}
	assistant.UpdatedAt = finishedAt
	if err := c.store.UpdateMessage(ctx, *assistant); err != nil {
		return err
	}
	if err := c.store.UpdateRun(ctx, *run); err != nil {
		return err
	}
	c.publishEvent(Event{Type: EventMessageUpdated, SessionID: run.SessionID, RunID: run.ID, Payload: *assistant})
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: run.SessionID, RunID: run.ID, Payload: *run})
	return nil
}

func (c *Core) checkExternalRunCanceled(ctx context.Context, run Run, assistant *Message, assistantSaved bool, runtime externalagents.RuntimeAgent, session externalagents.ExternalSession) (bool, error) {
	canceled, err := c.isRunCanceled(ctx, run.ID)
	if err != nil || !canceled {
		return false, nil
	}
	if runtime != nil {
		_ = runtime.Interrupt(ctx, session)
	}
	return true, c.finishCanceledAssistant(ctx, assistant, assistantSaved)
}
