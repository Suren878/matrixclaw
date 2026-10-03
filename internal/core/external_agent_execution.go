package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const (
	externalRunHeartbeatInterval   = 5 * time.Second
	externalToolInputLimit         = 64 * 1024
	externalToolOutputPerItemLimit = 64 * 1024
	externalToolOutputTotalLimit   = 256 * 1024
	externalReasoningPartLimit     = 128 * 1024
	externalTruncationMarker       = "\n\n[MatrixClaw: output truncated; the external agent retains the full result]"
)

// tryExecuteExternalAgentRun executes a claimed run of an external agent's
// session and reports whether the session was one.
func (c *Core) tryExecuteExternalAgentRun(ctx context.Context, runCtx context.Context, claimed claimedRun, session Session) (bool, error) {
	if c.externalStore == nil {
		return false, nil
	}
	run := claimed.Run
	attachment, err := c.externalStore.GetExternalAgentSession(ctx, session.ID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, c.failRun(ctx, run, err)
	}
	runtime, err := c.externalRuntime(attachment.AgentID)
	if err != nil {
		return true, c.failRun(ctx, run, err)
	}
	return true, c.executeExternalAgentRun(ctx, runCtx, claimed, runtime, attachment)
}

func (c *Core) externalRuntime(agentID string) (externalagents.RuntimeAgent, error) {
	registry := c.externalAgentRegistry()
	if registry == nil {
		return nil, fmt.Errorf("%w: external agent registry unavailable", ErrExecutionUnavailable)
	}
	runtime, ok := registry.Get(agentID)
	if !ok {
		return nil, fmt.Errorf("%w: external agent %q is not configured", ErrExecutionUnavailable, agentID)
	}
	return runtime, nil
}

func (c *Core) executeExternalAgentRun(ctx context.Context, runCtx context.Context, claimed claimedRun, runtime externalagents.RuntimeAgent, attachment externalagents.SessionAttachment) error {
	run := claimed.Run
	userMessage, err := c.findRunUserMessage(ctx, run)
	if err != nil {
		return c.failRun(ctx, run, err)
	}
	inputText := userMessage.Content
	if claimed.Recovering {
		// The agent's own tools may have run; only it can say where it stopped.
		messages, err := c.store.ListRunMessages(ctx, run.SessionID, run.ID)
		if err == nil {
			err = c.sealExternalReply(ctx, run.ID, messages)
		}
		if err != nil {
			return c.failRun(ctx, run, err)
		}
		inputText = runRecoveryNotice + "\n\nContinue the existing task from where it stopped. Inspect the workspace before making further changes. Original task for reference:\n" + userMessage.Content
	}
	externalSession := attachment.ExternalSession()
	if strings.TrimSpace(externalSession.Model) == "" {
		externalSession.Model = c.externalAgentDefaultModel(ctx, attachment.AgentID)
		attachment.Model = externalSession.Model
	}
	events, err := runtime.Send(runCtx, externalSession, externalagents.Input{Text: inputText})
	if err != nil {
		if runCtx.Err() != nil {
			return c.finishExternalRunAfterContextStopped(run, nil, false, runtime, externalSession)
		}
		return c.failRun(ctx, run, err)
	}

	assistant := transcript.Message{
		ID:        c.newID("msg"),
		SessionID: run.SessionID,
		RunID:     run.ID,
		Role:      transcript.MessageRoleAssistant,
		Model:     attachment.Model,
		Provider:  attachment.AgentID,
	}
	assistantSaved := false
	progressDirty := false
	lastProgressFlush := time.Time{}
	flushProgress := func(force bool) error {
		if !progressDirty {
			return nil
		}
		now := c.now().UTC()
		if !force && assistantSaved && !lastProgressFlush.IsZero() && now.Sub(lastProgressFlush) < agent.ProgressFlushInterval {
			return nil
		}
		if err := c.saveExternalAssistantProgress(ctx, &assistant, &assistantSaved); err != nil {
			return err
		}
		progressDirty = false
		lastProgressFlush = now
		return c.touchExternalRunActivity(ctx, &run, now)
	}

	ticker := time.NewTicker(agent.ProgressFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-runCtx.Done():
			return c.finishExternalRunAfterContextStopped(run, &assistant, assistantSaved, runtime, externalSession)
		case <-ticker.C:
			if err := flushProgress(false); err != nil {
				return c.failRun(ctx, run, err)
			}
			if err := c.touchExternalRunActivity(ctx, &run, c.now().UTC()); err != nil {
				return c.failRun(ctx, run, err)
			}
			continue
		case event, ok := <-events:
			if runCtx.Err() != nil {
				return c.finishExternalRunAfterContextStopped(run, &assistant, assistantSaved, runtime, externalSession)
			}
			if !ok {
				return c.failWithReply(ctx, run, &assistant, assistantSaved, "", errors.New("external agent event stream ended before turn completed"))
			}
			switch event.Kind {
			case externalagents.EventTurnStarted:
				if err := c.updateExternalAgentSessionFromEvent(ctx, &attachment, &externalSession, event); err != nil {
					return c.failRun(ctx, run, err)
				}
				if err := c.touchExternalRunActivity(ctx, &run, event.At); err != nil {
					return c.failRun(ctx, run, err)
				}
			case externalagents.EventHeartbeat:
				if err := c.touchExternalRunActivity(ctx, &run, event.At); err != nil {
					return c.failRun(ctx, run, err)
				}
			case externalagents.EventMessageDelta:
				progressDirty = applyExternalMessageDelta(&assistant, event.Text) || progressDirty
			case externalagents.EventReasoningDelta:
				progressDirty = applyExternalReasoningDelta(&assistant, event.Text) || progressDirty
			case externalagents.EventToolStarted:
				progressDirty = applyExternalToolStarted(&assistant, event) || progressDirty
			case externalagents.EventToolOutputDelta, externalagents.EventDiffUpdated:
				progressDirty = applyExternalToolOutputDelta(&assistant, event) || progressDirty
			case externalagents.EventToolCompleted:
				progressDirty = applyExternalToolCompleted(&assistant, event) || progressDirty
			case externalagents.EventTurnCompleted:
				return c.completeExternalAgentRun(ctx, &run, &assistant, assistantSaved)
			case externalagents.EventTurnFailed:
				errText := strings.TrimSpace(event.Error)
				if errText == "" {
					errText = "external agent turn failed"
				}
				return c.failWithReply(ctx, run, &assistant, assistantSaved, "", errors.New(errText))
			}
			if progressDirty && !assistantSaved {
				if err := flushProgress(true); err != nil {
					return c.failRun(ctx, run, err)
				}
			}
		}
	}
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

func (c *Core) findRunUserMessage(ctx context.Context, run Run) (transcript.Message, error) {
	message, err := c.store.GetMessage(ctx, run.UserMessageID)
	if err == nil && message.SessionID != run.SessionID {
		err = ErrNotFound
	}
	return message, err
}

func applyExternalMessageDelta(assistant *transcript.Message, delta string) bool {
	if assistant == nil || delta == "" {
		return false
	}
	assistant.Content += delta
	appendExternalTextDelta(assistant, delta)
	return true
}

func applyExternalReasoningDelta(assistant *transcript.Message, delta string) bool {
	if assistant == nil || delta == "" {
		return false
	}
	appendExternalReasoningDelta(assistant, delta)
	return true
}

func applyExternalToolStarted(assistant *transcript.Message, event externalagents.Event) bool {
	if assistant == nil || strings.TrimSpace(event.ItemID) == "" {
		return false
	}
	upsertExternalToolCall(assistant, event.ItemID, defaultExternalToolName(event.ToolName), event.ToolInput, false)
	return true
}

func applyExternalToolOutputDelta(assistant *transcript.Message, event externalagents.Event) bool {
	if assistant == nil || strings.TrimSpace(event.ItemID) == "" || event.Text == "" {
		return false
	}
	upsertExternalToolResult(assistant, event.ItemID, defaultExternalToolName(event.ToolName), event.Text, false, true)
	return true
}

func applyExternalToolCompleted(assistant *transcript.Message, event externalagents.Event) bool {
	if assistant == nil || strings.TrimSpace(event.ItemID) == "" {
		return false
	}
	name := defaultExternalToolName(event.ToolName)
	upsertExternalToolCall(assistant, event.ItemID, name, event.ToolInput, true)
	if strings.TrimSpace(event.Text) != "" || strings.TrimSpace(event.Error) != "" {
		upsertExternalToolResult(assistant, event.ItemID, name, event.Text, strings.TrimSpace(event.Error) != "", false)
	}
	return true
}

func (c *Core) saveExternalAssistantProgress(ctx context.Context, assistant *transcript.Message, saved *bool) error {
	now := c.now().UTC()
	if !*saved {
		assistant.CreatedAt = now
		assistant.UpdatedAt = now
		if _, err := c.store.SaveMessageProgress(ctx, *assistant); err != nil {
			return err
		}
		*saved = true
		c.publishEvent(Event{Type: EventMessageCreated, SessionID: assistant.SessionID, RunID: assistant.RunID, Payload: *assistant})
		return nil
	}
	assistant.UpdatedAt = now
	if err := c.store.UpdateMessageProgress(ctx, *assistant); err != nil {
		return err
	}
	c.publishEvent(Event{Type: EventMessageUpdated, SessionID: assistant.SessionID, RunID: assistant.RunID, Payload: *assistant})
	return nil
}

func (c *Core) touchExternalRunActivity(ctx context.Context, run *Run, at time.Time) error {
	if at.IsZero() {
		at = c.now().UTC()
	} else {
		at = at.UTC()
	}
	if !run.UpdatedAt.IsZero() && at.Sub(run.UpdatedAt) < externalRunHeartbeatInterval {
		return nil
	}
	run.UpdatedAt = at
	if err := c.store.TouchRun(ctx, run.ID, at); err != nil {
		return err
	}
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: run.SessionID, RunID: run.ID, Payload: *run})
	return nil
}

func appendExternalTextDelta(assistant *transcript.Message, delta string) {
	if delta == "" {
		return
	}
	if len(assistant.Parts) > 0 {
		last := &assistant.Parts[len(assistant.Parts)-1]
		if last.Kind == transcript.MessagePartKindText && last.Text != nil {
			last.Text.Text += delta
			return
		}
	}
	assistant.Parts = append(assistant.Parts, transcript.MessagePart{
		Kind: transcript.MessagePartKindText,
		Text: &transcript.TextPart{Text: delta},
	})
}

func appendExternalReasoningDelta(assistant *transcript.Message, delta string) {
	if len(assistant.Parts) > 0 {
		last := &assistant.Parts[len(assistant.Parts)-1]
		if last.Kind == transcript.MessagePartKindReasoning && last.Reasoning != nil {
			last.Reasoning.Text = clipExternalPayload(last.Reasoning.Text+delta, externalReasoningPartLimit)
			return
		}
	}
	assistant.Parts = append(assistant.Parts, transcript.MessagePart{
		Kind:      transcript.MessagePartKindReasoning,
		Reasoning: &transcript.ReasoningPart{Text: clipExternalPayload(delta, externalReasoningPartLimit)},
	})
}

func upsertExternalToolCall(assistant *transcript.Message, id string, name string, input string, finished bool) {
	for i := range assistant.Parts {
		if assistant.Parts[i].Kind != transcript.MessagePartKindToolCall || assistant.Parts[i].ToolCall == nil {
			continue
		}
		if assistant.Parts[i].ToolCall.ID != id {
			continue
		}
		if name != "" {
			assistant.Parts[i].ToolCall.Name = name
		}
		if strings.TrimSpace(input) != "" {
			assistant.Parts[i].ToolCall.Input = clipExternalPayload(input, externalToolInputLimit)
		}
		if finished {
			assistant.Parts[i].ToolCall.Finished = true
		}
		return
	}
	assistant.Parts = append(assistant.Parts, transcript.MessagePart{
		Kind: transcript.MessagePartKindToolCall,
		ToolCall: &transcript.ToolCallPart{
			ID:       id,
			Name:     name,
			Input:    clipExternalPayload(input, externalToolInputLimit),
			Finished: finished,
		},
	})
}

func upsertExternalToolResult(assistant *transcript.Message, id string, name string, content string, isError bool, appendContent bool) {
	name = externalToolResultName(assistant, id, name)
	contentLimit := externalToolResultContentLimit(assistant, id)
	for i := range assistant.Parts {
		if assistant.Parts[i].Kind != transcript.MessagePartKindToolResult || assistant.Parts[i].ToolResult == nil {
			continue
		}
		if assistant.Parts[i].ToolResult.ToolCallID != id {
			continue
		}
		if appendContent {
			assistant.Parts[i].ToolResult.Content = clipExternalPayload(assistant.Parts[i].ToolResult.Content+content, contentLimit)
		} else if strings.TrimSpace(content) != "" {
			assistant.Parts[i].ToolResult.Content = clipExternalPayload(content, contentLimit)
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
	assistant.Parts = append(assistant.Parts, transcript.MessagePart{
		Kind: transcript.MessagePartKindToolResult,
		ToolResult: &transcript.ToolResultPart{
			ToolCallID: id,
			Name:       name,
			Content:    clipExternalPayload(content, contentLimit),
			Status:     status,
			IsError:    isError,
		},
	})
}

func externalToolResultContentLimit(assistant *transcript.Message, targetID string) int {
	remaining := externalToolOutputTotalLimit
	if assistant != nil {
		for _, part := range assistant.Parts {
			if part.ToolResult == nil || part.ToolResult.ToolCallID == targetID {
				continue
			}
			remaining -= len(part.ToolResult.Content)
			if remaining <= 0 {
				return 0
			}
		}
	}
	if remaining > externalToolOutputPerItemLimit {
		return externalToolOutputPerItemLimit
	}
	return remaining
}

func clipExternalPayload(value string, limit int) string {
	if limit <= 0 {
		return strings.TrimSpace(externalTruncationMarker)
	}
	if len(value) <= limit {
		return value
	}
	marker := externalTruncationMarker
	if limit <= len(marker) {
		return truncateUTF8Prefix(marker, limit)
	}
	available := limit - len(marker)
	prefixLimit := available * 3 / 4
	suffixLimit := available - prefixLimit
	return truncateUTF8Prefix(value, prefixLimit) + marker + truncateUTF8Suffix(value, suffixLimit)
}

func truncateUTF8Prefix(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	end := limit
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

func truncateUTF8Suffix(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	start := len(value) - limit
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}

func externalToolResultName(assistant *transcript.Message, id string, fallback string) string {
	fallback = defaultExternalToolName(fallback)
	for _, part := range assistant.Parts {
		if part.Kind != transcript.MessagePartKindToolCall || part.ToolCall == nil {
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

// completeExternalAgentRun ends the run with the agent's finished turn.
func (c *Core) completeExternalAgentRun(ctx context.Context, run *Run, assistant *transcript.Message, assistantSaved bool) error {
	assistant.Parts = append(transcript.NormalizeMessageParts(assistant.Content, assistant.Parts), transcript.MessagePart{
		Kind:   transcript.MessagePartKindFinish,
		Finish: &transcript.FinishPart{Reason: transcript.FinishReasonEndTurn},
	})
	return c.transition(ctx, run, runChange{To: RunStatusCompleted, Reply: assistant, ReplySaved: assistantSaved})
}

func (c *Core) finishExternalRunAfterContextStopped(run Run, assistant *transcript.Message, assistantSaved bool, runtime externalagents.RuntimeAgent, session externalagents.ExternalSession) error {
	ctx, cancel := context.WithTimeout(context.Background(), runInterruptionPersistenceTimeout)
	defer cancel()

	current, err := c.store.GetRun(ctx, run.ID)
	if err != nil {
		return err
	}
	if runtime != nil {
		_ = runtime.Interrupt(ctx, session)
	}
	if current.Status == RunStatusCanceled {
		return c.sealCanceledReply(ctx, assistant, assistantSaved)
	}
	if current.Status.Terminal() {
		return nil
	}
	return c.sealInterruptedReply(ctx, assistant, assistantSaved)
}
