package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type preparedToolCall struct {
	SessionID  string
	RunID      string
	ToolName   string
	Spec       tools.Spec
	ToolCallID string
	WorkingDir string
	Message    transcript.Message
}

// checkToolCall validates a tool call against its session before anything is written;
// ErrInvalidInput errors are returned to the model, others fail the run.
func (c *Core) checkToolCall(ctx context.Context, sessionID string, toolName string) (Session, tools.Spec, error) {
	if c.tools == nil {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: tools are not configured", ErrExecutionUnavailable)
	}
	sessionID = normalizeText(sessionID)
	toolName = normalizeText(toolName)
	if sessionID == "" {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: session_id is required", ErrInvalidInput)
	}
	if toolName == "" {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: tool_name is required", ErrInvalidInput)
	}
	spec, ok := c.tools.Spec(toolName)
	if !ok {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: unknown tool %q", ErrInvalidInput, toolName)
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, tools.Spec{}, err
	}
	if toolName == agentToolName && CoreSessionIsExternalAgent(session) {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: the agent tool is available for Matrixclaw sessions only", ErrInvalidInput)
	}
	if !isSubagentSession(session) {
		return session, spec, nil
	}
	if !subagentToolAllowed(spec) {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: tool %q is not available to child subagents", ErrInvalidInput, toolName)
	}
	if spec.Mutates() {
		readonly, err := c.readonlySubagent(ctx, session.ID)
		if err != nil {
			return Session{}, tools.Spec{}, err
		}
		if readonly {
			return Session{}, tools.Spec{}, fmt.Errorf("%w: tool %q changes things, and this subagent is read-only", ErrInvalidInput, toolName)
		}
	}
	return session, spec, nil
}

func (c *Core) prepareToolCall(ctx context.Context, input ExecuteToolInput) (preparedToolCall, error) {
	session, spec, err := c.checkToolCall(ctx, input.SessionID, input.ToolName)
	if err != nil {
		return preparedToolCall{}, err
	}
	sessionID := normalizeText(input.SessionID)
	toolName := normalizeText(input.ToolName)
	workingDir := normalizeWorkingDir(input.WorkingDir)
	if workingDir == "" {
		workingDir = session.WorkingDir
	}

	toolCallID := normalizeText(input.ToolCallID)
	if toolCallID == "" {
		toolCallID = c.newID("tool")
	}
	runID := normalizeText(input.RunID)
	message := agent.ToolCallMessage(toolCallID, sessionID, runID, toolName, input.Args, false, c.now().UTC())
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
		if err := c.saveRunCheckpoint(ctx, runID); err != nil {
			return preparedToolCall{}, err
		}
		return prepared, nil
	}
	if _, err := c.store.AppendMessage(ctx, message); err != nil {
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
	if err := c.saveRunCheckpoint(ctx, runID); err != nil {
		return preparedToolCall{}, err
	}
	return prepared, nil
}

func (c *Core) isNewToolCallMessage(ctx context.Context, sessionID string, toolCallID string) (bool, error) {
	message, err := c.store.GetMessage(ctx, toolCallID)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if message.SessionID != sessionID {
		return false, fmt.Errorf("tool call id %q already belongs to another session", toolCallID)
	}
	return false, nil
}
