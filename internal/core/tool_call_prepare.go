package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/permission"
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

// callScope is what checking a session's tool calls reads from the store:
// the session, its subagent task when it is a subagent's, and the permission
// rules over it. A run reads it once for each group of calls it admits.
type callScope struct {
	session  Session
	task     Task
	subagent bool
	rules    []permission.Rule
}

// callScope reads the scope of a session's calls.
func (c *Core) callScope(ctx context.Context, sessionID string) (callScope, error) {
	if sessionID == "" {
		return callScope{}, fmt.Errorf("%w: session_id is required", ErrInvalidInput)
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return callScope{}, err
	}
	scope := callScope{session: session, subagent: isSubagentSession(session)}
	if scope.subagent {
		if scope.task, _, err = c.subagentTaskOf(ctx, session.ID); err != nil {
			return callScope{}, err
		}
	}
	if scope.rules, err = c.permissionRules(ctx, session); err != nil {
		return callScope{}, err
	}
	return scope, nil
}

// checkToolCall validates a tool call of the scope's session before anything
// is written; ErrInvalidInput errors are returned to the model, others fail
// the run. A read-only subagent's call of bash gets bash's read-only spec.
func (c *Core) checkToolCall(scope callScope, toolName string, call tools.Call) (tools.Spec, error) {
	if c.tools == nil {
		return tools.Spec{}, fmt.Errorf("%w: tools are not configured", ErrExecutionUnavailable)
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return tools.Spec{}, fmt.Errorf("%w: tool_name is required", ErrInvalidInput)
	}
	spec, ok := c.tools.Spec(toolName)
	if !ok {
		return tools.Spec{}, fmt.Errorf("%w: unknown tool %q", ErrInvalidInput, toolName)
	}
	session := scope.session
	if toolName == agentToolName && CoreSessionIsExternalAgent(session) {
		return tools.Spec{}, fmt.Errorf("%w: the agent tool is available for Matrixclaw sessions only", ErrInvalidInput)
	}
	if !scope.subagent {
		return spec, nil
	}
	task := scope.task
	if !subagentToolAllowed(spec) || !task.Agent.allows(spec.ID) {
		return tools.Spec{}, fmt.Errorf("%w: tool %q is not available to this subagent", ErrInvalidInput, toolName)
	}
	if call.WorkingDir = normalizeWorkingDir(call.WorkingDir); call.WorkingDir == "" {
		call.WorkingDir = session.WorkingDir
	}
	if task.Isolation == SubagentIsolationWorktree && task.WorkingDir != "" {
		if err := tools.CheckConfinedCall(spec, call, c.tools.Subjects(spec.ID, call), !c.tools.ReadsOnly(spec.ID, call), task.WorkingDir); err != nil {
			return tools.Spec{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
	}
	if spec.Mutates() && task.Readonly {
		return readOnlyCall(spec, call)
	}
	return spec, nil
}

// readOnlyCall is how a read-only subagent may call a mutating tool: only
// through the tool's read-only spec, with arguments that change nothing.
func readOnlyCall(spec tools.Spec, call tools.Call) (tools.Spec, error) {
	readOnly, ok := tools.ReadOnlySpec(spec)
	if !ok {
		return tools.Spec{}, fmt.Errorf("%w: tool %q changes things, and this subagent is read-only", ErrInvalidInput, spec.ID)
	}
	if err := tools.CheckReadOnlyCall(call); err != nil {
		return tools.Spec{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	return readOnly, nil
}

// prepareToolCall checks a call made outside a run and journals it unless it
// was journaled before; it returns the scope it checked the call in.
func (c *Core) prepareToolCall(ctx context.Context, input ExecuteToolInput) (preparedToolCall, callScope, error) {
	if c.tools == nil {
		return preparedToolCall{}, callScope{}, fmt.Errorf("%w: tools are not configured", ErrExecutionUnavailable)
	}
	scope, err := c.callScope(ctx, input.SessionID)
	if err != nil {
		return preparedToolCall{}, callScope{}, err
	}
	session := scope.session
	spec, err := c.checkToolCall(scope, input.ToolName, tools.Call{SessionID: input.SessionID, WorkingDir: input.WorkingDir, Args: input.Args})
	if err != nil {
		return preparedToolCall{}, callScope{}, err
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
		return preparedToolCall{}, callScope{}, err
	}
	if !isNewCall {
		return prepared, scope, nil
	}
	if _, err := c.store.AppendMessage(ctx, message); err != nil {
		return preparedToolCall{}, callScope{}, err
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
	return prepared, scope, nil
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
