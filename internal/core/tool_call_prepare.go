package core

import (
	"context"
	"fmt"
	"time"

	"github.com/Suren878/matrixclaw/internal/tools"
)

type preparedToolCall struct {
	SessionID  string
	RunID      string
	ToolName   string
	Spec       tools.Spec
	ToolCallID string
	WorkingDir string
	Message    Message
}

func (c *Core) prepareToolCall(ctx context.Context, input ExecuteToolInput) (preparedToolCall, error) {
	if c.tools == nil {
		return preparedToolCall{}, fmt.Errorf("%w: tools are not configured", ErrExecutionUnavailable)
	}
	sessionID := normalizeText(input.SessionID)
	toolName := normalizeText(input.ToolName)
	if sessionID == "" {
		return preparedToolCall{}, fmt.Errorf("%w: session_id is required", ErrInvalidInput)
	}
	if toolName == "" {
		return preparedToolCall{}, fmt.Errorf("%w: tool_name is required", ErrInvalidInput)
	}
	spec, ok := c.tools.Spec(toolName)
	if !ok {
		return preparedToolCall{}, fmt.Errorf("%w: unknown tool %q", ErrInvalidInput, toolName)
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return preparedToolCall{}, err
	}
	if toolName == delegateTaskToolName && CoreSessionIsExternalAgent(session) {
		return preparedToolCall{}, fmt.Errorf("%w: delegate_task is available for Matrixclaw sessions only", ErrInvalidInput)
	}
	if isSubagentSession(session) && !subagentToolAllowed(spec) {
		return preparedToolCall{}, fmt.Errorf("%w: tool %q is not available to child subagents", ErrInvalidInput, toolName)
	}
	workingDir := normalizeWorkingDir(input.WorkingDir)
	if workingDir == "" {
		workingDir = session.WorkingDir
	}

	toolCallID := normalizeText(input.ToolCallID)
	if toolCallID == "" {
		toolCallID = c.newID("tool")
	}
	runID := normalizeText(input.RunID)
	message := newToolCallMessage(toolCallID, sessionID, runID, toolName, input.Args, false, c.now().UTC())
	prepared := preparedToolCall{
		SessionID:  sessionID,
		RunID:      runID,
		ToolName:   toolName,
		Spec:       spec,
		ToolCallID: toolCallID,
		WorkingDir: workingDir,
		Message:    message,
	}

	isNewCall, err := c.isNewToolCallMessage(ctx, sessionID, toolCallID)
	if err != nil {
		return preparedToolCall{}, err
	}
	if !isNewCall {
		return prepared, nil
	}
	if err := c.store.SaveMessage(ctx, message); err != nil {
		return preparedToolCall{}, err
	}
	c.publishEvent(Event{
		Type:      EventMessageCreated,
		SessionID: sessionID,
		RunID:     message.RunID,
		Payload:   message,
	})
	c.publishToolUpdate(sessionID, message.RunID, ToolUpdate{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		State:      ToolLifecycleRequested,
		RunID:      message.RunID,
		SessionID:  sessionID,
	})
	_ = c.touchAsyncSubagentTaskActivity(ctx, message.RunID, message.UpdatedAt)
	return prepared, nil
}

func newToolCallMessage(id string, sessionID string, runID string, toolName string, args []byte, finished bool, createdAt time.Time) Message {
	return Message{
		ID:        id,
		SessionID: sessionID,
		RunID:     runID,
		Role:      MessageRoleAssistant,
		Content:   "",
		Parts: []MessagePart{{
			Kind: MessagePartKindToolCall,
			ToolCall: &ToolCallPart{
				ID:       id,
				Name:     toolName,
				Input:    string(args),
				Finished: finished,
			},
		}},
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
}

func (c *Core) isNewToolCallMessage(ctx context.Context, sessionID string, toolCallID string) (bool, error) {
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return false, err
	}
	for _, message := range messages {
		if message.ID == toolCallID {
			return false, nil
		}
	}
	return true, nil
}
