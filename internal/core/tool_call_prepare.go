package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// checkToolCall validates a tool call against its session before anything is
// written; ErrInvalidInput errors are returned to the model, others fail the
// run. A read-only subagent's call of bash gets bash's read-only spec.
func (c *Core) checkToolCall(ctx context.Context, toolName string, call tools.Call) (Session, tools.Spec, error) {
	sessionID := call.SessionID
	if c.tools == nil {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: tools are not configured", ErrExecutionUnavailable)
	}
	toolName = strings.TrimSpace(toolName)
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
	task, _, err := c.subagentTaskOf(ctx, session.ID)
	if err != nil {
		return Session{}, tools.Spec{}, err
	}
	if !subagentToolAllowed(spec) || !task.Agent.allows(spec.ID) {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: tool %q is not available to this subagent", ErrInvalidInput, toolName)
	}
	if call.WorkingDir = normalizeWorkingDir(call.WorkingDir); call.WorkingDir == "" {
		call.WorkingDir = session.WorkingDir
	}
	if task.Isolation == SubagentIsolationWorktree && task.WorkingDir != "" {
		if err := tools.CheckConfinedCall(spec, call, c.tools.Subject(spec.ID, call), task.WorkingDir); err != nil {
			return Session{}, tools.Spec{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
	}
	if spec.Mutates() && task.Readonly {
		return readOnlyCall(session, spec, call)
	}
	return session, spec, nil
}

// readOnlyCall is how a read-only subagent may call a mutating tool: only
// through the tool's read-only spec, with arguments that change nothing.
func readOnlyCall(session Session, spec tools.Spec, call tools.Call) (Session, tools.Spec, error) {
	readOnly, ok := tools.ReadOnlySpec(spec)
	if !ok {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: tool %q changes things, and this subagent is read-only", ErrInvalidInput, spec.ID)
	}
	if err := tools.CheckReadOnlyCall(call); err != nil {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	return session, readOnly, nil
}

func (c *Core) prepareToolCall(ctx context.Context, input ExecuteToolInput) (preparedToolCall, error) {
	session, spec, err := c.checkToolCall(ctx, input.ToolName, tools.Call{SessionID: input.SessionID, WorkingDir: input.WorkingDir, Args: input.Args})
	if err != nil {
		return preparedToolCall{}, err
	}
	sessionID := input.SessionID
	toolName := input.ToolName
	workingDir := normalizeWorkingDir(input.WorkingDir)
	if workingDir == "" {
		workingDir = session.WorkingDir
	}

	toolCallID := input.ToolCallID
	if toolCallID == "" {
		toolCallID = c.newID("tool")
	}
	message := agent.ToolCallMessage(toolCallID, sessionID, "", toolName, input.Args, false, c.now().UTC())
	prepared := preparedToolCall{
		SessionID:  sessionID,
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
