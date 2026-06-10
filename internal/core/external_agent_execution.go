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
	runtime, err := c.externalRuntime(attachment.AgentID)
	if err != nil {
		return true, c.failRunByID(ctx, run, err)
	}

	if err := c.setRunStatus(ctx, &run, RunStatusRunning, ""); err != nil {
		return true, err
	}

	runCtx, unregisterRun := c.activeRunContext(ctx, run.ID)
	defer unregisterRun()

	return true, c.executeExternalAgentRun(ctx, runCtx, run, runtime, attachment)
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

func (c *Core) executeExternalAgentRun(ctx context.Context, runCtx context.Context, run Run, runtime externalagents.RuntimeAgent, attachment externalagents.SessionAttachment) error {
	userMessage, err := c.findRunUserMessage(ctx, run)
	if err != nil {
		return c.failRunByID(ctx, run, err)
	}
	externalSession := attachment.ExternalSession()
	if strings.TrimSpace(externalSession.Model) == "" {
		externalSession.Model = c.externalAgentDefaultModel(ctx, attachment.AgentID)
		attachment.Model = externalSession.Model
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
		case externalagents.EventTurnStarted:
			if err := c.updateExternalAgentSessionFromEvent(ctx, &attachment, &externalSession, event); err != nil {
				return c.failRunByID(ctx, run, err)
			}
		case externalagents.EventMessageDelta:
			if err := c.applyExternalMessageDelta(ctx, &assistant, &assistantSaved, event.Text); err != nil {
				return c.failRunByID(ctx, run, err)
			}
		case externalagents.EventReasoningDelta:
			if err := c.applyExternalReasoningDelta(ctx, &assistant, &assistantSaved, event.Text); err != nil {
				return c.failRunByID(ctx, run, err)
			}
		case externalagents.EventToolStarted:
			if err := c.applyExternalToolStarted(ctx, &assistant, &assistantSaved, event); err != nil {
				return c.failRunByID(ctx, run, err)
			}
		case externalagents.EventToolOutputDelta, externalagents.EventDiffUpdated:
			if err := c.applyExternalToolOutputDelta(ctx, &assistant, &assistantSaved, event); err != nil {
				return c.failRunByID(ctx, run, err)
			}
		case externalagents.EventToolCompleted:
			if err := c.applyExternalToolCompleted(ctx, &assistant, &assistantSaved, event); err != nil {
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

func (c *Core) updateExternalAgentSessionFromEvent(ctx context.Context, attachment *externalagents.SessionAttachment, session *externalagents.ExternalSession, event externalagents.Event) error {
	if c == nil || c.externalStore == nil || attachment == nil || session == nil {
		return nil
	}
	threadID := strings.TrimSpace(event.ExternalThreadID)
	sessionID := strings.TrimSpace(event.ExternalSessionID)
	if threadID == "" && sessionID == "" {
		return nil
	}
	if threadID == "" {
		threadID = sessionID
	}
	if sessionID == "" {
		sessionID = threadID
	}
	if attachment.ExternalThreadID == threadID && attachment.ExternalSessionID == sessionID {
		return nil
	}
	attachment.ExternalThreadID = threadID
	attachment.ExternalSessionID = sessionID
	attachment.UpdatedAt = c.now().UTC()
	if strings.TrimSpace(attachment.CWD) == "" {
		attachment.CWD = session.CWD
	}
	if strings.TrimSpace(attachment.Model) == "" {
		attachment.Model = session.Model
	}
	if strings.TrimSpace(attachment.ApprovalPolicy) == "" {
		attachment.ApprovalPolicy = session.ApprovalPolicy
	}
	if strings.TrimSpace(attachment.Sandbox) == "" {
		attachment.Sandbox = session.Sandbox
	}
	session.ExternalThreadID = threadID
	session.ExternalSessionID = sessionID
	return c.externalStore.SaveExternalAgentSession(ctx, *attachment)
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
	if delta == "" {
		return nil
	}
	assistant.Content += delta
	appendExternalTextDelta(assistant, delta)
	return c.saveExternalAssistantProgress(ctx, assistant, saved)
}

func (c *Core) applyExternalReasoningDelta(ctx context.Context, assistant *Message, saved *bool, delta string) error {
	if strings.TrimSpace(delta) == "" {
		return nil
	}
	if assistant == nil || saved == nil {
		return nil
	}
	appendExternalReasoningDelta(assistant, delta)
	return c.saveExternalAssistantProgress(ctx, assistant, saved)
}

func (c *Core) applyExternalToolStarted(ctx context.Context, assistant *Message, saved *bool, event externalagents.Event) error {
	if assistant == nil || saved == nil || strings.TrimSpace(event.ItemID) == "" {
		return nil
	}
	upsertExternalToolCall(assistant, event.ItemID, defaultExternalToolName(event.ToolName), event.ToolInput, false)
	return c.saveExternalAssistantProgress(ctx, assistant, saved)
}

func (c *Core) applyExternalToolOutputDelta(ctx context.Context, assistant *Message, saved *bool, event externalagents.Event) error {
	if assistant == nil || saved == nil || strings.TrimSpace(event.ItemID) == "" || event.Text == "" {
		return nil
	}
	upsertExternalToolResult(assistant, event.ItemID, defaultExternalToolName(event.ToolName), event.Text, false, true)
	return c.saveExternalAssistantProgress(ctx, assistant, saved)
}

func (c *Core) applyExternalToolCompleted(ctx context.Context, assistant *Message, saved *bool, event externalagents.Event) error {
	if assistant == nil || saved == nil || strings.TrimSpace(event.ItemID) == "" {
		return nil
	}
	name := defaultExternalToolName(event.ToolName)
	upsertExternalToolCall(assistant, event.ItemID, name, event.ToolInput, true)
	if strings.TrimSpace(event.Text) != "" || strings.TrimSpace(event.Error) != "" {
		upsertExternalToolResult(assistant, event.ItemID, name, event.Text, strings.TrimSpace(event.Error) != "", false)
	}
	return c.saveExternalAssistantProgress(ctx, assistant, saved)
}

func (c *Core) saveExternalAssistantProgress(ctx context.Context, assistant *Message, saved *bool) error {
	now := c.now().UTC()
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

func appendExternalTextDelta(assistant *Message, delta string) {
	if delta == "" {
		return
	}
	if len(assistant.Parts) > 0 {
		last := &assistant.Parts[len(assistant.Parts)-1]
		if last.Kind == MessagePartKindText && last.Text != nil {
			last.Text.Text += delta
			return
		}
	}
	assistant.Parts = append(assistant.Parts, MessagePart{
		Kind: MessagePartKindText,
		Text: &TextPart{Text: delta},
	})
}

func appendExternalReasoningDelta(assistant *Message, delta string) {
	if len(assistant.Parts) > 0 {
		last := &assistant.Parts[len(assistant.Parts)-1]
		if last.Kind == MessagePartKindReasoning && last.Reasoning != nil {
			last.Reasoning.Text += delta
			return
		}
	}
	assistant.Parts = append(assistant.Parts, MessagePart{
		Kind:      MessagePartKindReasoning,
		Reasoning: &ReasoningPart{Text: delta},
	})
}

func upsertExternalToolCall(assistant *Message, id string, name string, input string, finished bool) {
	for i := range assistant.Parts {
		if assistant.Parts[i].Kind != MessagePartKindToolCall || assistant.Parts[i].ToolCall == nil {
			continue
		}
		if assistant.Parts[i].ToolCall.ID != id {
			continue
		}
		if name != "" {
			assistant.Parts[i].ToolCall.Name = name
		}
		if strings.TrimSpace(input) != "" {
			assistant.Parts[i].ToolCall.Input = input
		}
		if finished {
			assistant.Parts[i].ToolCall.Finished = true
		}
		return
	}
	assistant.Parts = append(assistant.Parts, MessagePart{
		Kind: MessagePartKindToolCall,
		ToolCall: &ToolCallPart{
			ID:       id,
			Name:     name,
			Input:    input,
			Finished: finished,
		},
	})
}

func upsertExternalToolResult(assistant *Message, id string, name string, content string, isError bool, appendContent bool) {
	name = externalToolResultName(assistant, id, name)
	for i := range assistant.Parts {
		if assistant.Parts[i].Kind != MessagePartKindToolResult || assistant.Parts[i].ToolResult == nil {
			continue
		}
		if assistant.Parts[i].ToolResult.ToolCallID != id {
			continue
		}
		if appendContent {
			assistant.Parts[i].ToolResult.Content += content
		} else if strings.TrimSpace(content) != "" {
			assistant.Parts[i].ToolResult.Content = content
		}
		if name != "" {
			assistant.Parts[i].ToolResult.Name = name
		}
		if isError {
			assistant.Parts[i].ToolResult.IsError = true
			assistant.Parts[i].ToolResult.Status = "error"
		} else if assistant.Parts[i].ToolResult.Status == "" {
			assistant.Parts[i].ToolResult.Status = "success"
		}
		return
	}
	status := "success"
	if isError {
		status = "error"
	}
	assistant.Parts = append(assistant.Parts, MessagePart{
		Kind: MessagePartKindToolResult,
		ToolResult: &ToolResultPart{
			ToolCallID: id,
			Name:       name,
			Content:    content,
			Status:     status,
			IsError:    isError,
		},
	})
}

func externalToolResultName(assistant *Message, id string, fallback string) string {
	fallback = defaultExternalToolName(fallback)
	for _, part := range assistant.Parts {
		if part.Kind != MessagePartKindToolCall || part.ToolCall == nil {
			continue
		}
		if part.ToolCall.ID == id && strings.TrimSpace(part.ToolCall.Name) != "" {
			return part.ToolCall.Name
		}
	}
	return fallback
}

func defaultExternalToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "external_tool"
	}
	return name
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
