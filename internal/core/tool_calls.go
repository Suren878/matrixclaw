package core

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/events"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ExecuteTool runs one tool call outside the agent loop.
func (c *Core) ExecuteTool(ctx context.Context, input ExecuteToolInput) (ExecuteToolResult, error) {
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.ToolName = strings.TrimSpace(input.ToolName)
	input.ToolCallID = strings.TrimSpace(input.ToolCallID)
	prepared, scope, err := c.prepareToolCall(ctx, input)
	if err != nil {
		return ExecuteToolResult{}, err
	}

	var toolResult tools.Result
	var ask *tools.ApprovalRequest
	check, execErr := c.permitCall(ctx, scope, prepared.ToolName, prepared.Spec, prepared.call(input))
	if execErr == nil {
		toolResult, ask, execErr = c.runToolCall(ctx, prepared, input, check)
	}
	if ask != nil {
		approval, err := c.requestApproval(ctx, prepared, *ask)
		if err != nil {
			return ExecuteToolResult{}, err
		}
		return ExecuteToolResult{ToolCallMessage: prepared.Message, Approval: &approval}, nil
	}

	finalResult := toolResult
	if execErr != nil {
		finalResult = c.toolFailure(prepared.SessionID, execErr)
	}

	toolCallMessage, resultMessage, err := c.finishToolCall(ctx, prepared, input, finalResult)
	if err != nil {
		return ExecuteToolResult{}, err
	}
	if execErr != nil {
		return ExecuteToolResult{
			ToolCallMessage:   toolCallMessage,
			ToolResultMessage: resultMessage,
		}, execErr
	}
	return ExecuteToolResult{
		ToolCallMessage:   toolCallMessage,
		ToolResultMessage: resultMessage,
	}, nil
}

func (c *Core) ListToolSpecs() []tools.Spec {
	if c.tools == nil {
		return nil
	}
	return c.tools.List()
}

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
	c.publishEvent(events.Event{
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

// runToolCall runs a call as its permission check says: a deny rule or a hook
// blocks it; a call that waits for approval returns the tool's preview as its
// request instead of a result; any other call runs, with the arguments its
// hooks rewrote, and may recheck the rules for subjects it reaches later. What
// hooks and checks after it say ends its result.
func (c *Core) runToolCall(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput, check callPermission) (tools.Result, *tools.ApprovalRequest, error) {
	call := check.input(prepared.call(input))
	if refusal, refused := check.refusal(); refused {
		return tools.Result{Content: refusal, Status: tools.ResultStatusError}, nil, nil
	}
	if check.ask {
		request, refused := c.tools.Preview(ctx, prepared.ToolName, call)
		if refused != nil {
			return *refused, nil, nil
		}
		if check.secret {
			request.Description += "\nThis path may hold secrets."
		}
		switch {
		case check.hook.asked != "":
			request.Description += "\nAsked by hook: " + check.hook.asked
		case check.verdict.Effect == permission.Ask:
			request.Description += "\nAsked by rule " + check.verdict.Rule.String() + "."
		default:
			request.Suggestion = check.suggestion()
		}
		return tools.Result{}, &request, nil
	}
	result, err := c.tools.Execute(ctx, prepared.ToolName, check.guard(call))
	if err != nil {
		return result, nil, err
	}
	result = c.keepLargeOutput(prepared.SessionID, result)
	return c.afterToolUse(ctx, check, call, prepared.ToolName, result), nil, nil
}

func (prepared preparedToolCall) call(input ExecuteToolInput) tools.Call {
	return tools.Call{
		SessionID:   prepared.SessionID,
		RunID:       prepared.RunID,
		ToolCallID:  prepared.ToolCallID,
		Client:      input.Client,
		ExternalKey: input.ExternalKey,
		WorkingDir:  prepared.WorkingDir,
		Args:        input.Args,
	}
}

// requestApproval stores a pending approval for the call and announces it; it
// leaves the run's checkpoint to the engine. A subagent's approval is announced
// to its parent's session and chat; a read-only subagent's is refused at once.
func (c *Core) requestApproval(ctx context.Context, prepared preparedToolCall, request tools.ApprovalRequest) (Approval, error) {
	paramsRaw, err := marshalJSONRaw(request.Params)
	if err != nil {
		return Approval{}, err
	}
	task, subagent, err := c.subagentTaskOf(ctx, prepared.SessionID)
	if err != nil {
		return Approval{}, err
	}
	approval := Approval{
		ID:          c.newID("approval"),
		SessionID:   prepared.SessionID,
		RunID:       prepared.RunID,
		TaskID:      task.ID,
		AgentName:   task.AgentName,
		ToolCallRef: prepared.ToolCallID,
		ToolName:    prepared.ToolName,
		Description: request.Description,
		Params:      paramsRaw,
		Path:        request.Path,
		Suggestion:  request.Suggestion,
		State:       ApprovalStatePending,
		RequestedAt: c.now().UTC(),
	}
	if subagent && task.Readonly {
		approval.State, approval.DecidedAt = ApprovalStateRejected, &approval.RequestedAt
		approval.Reason = "read-only subagent cannot run " + cmp.Or(prepared.ToolName, "this tool")
		return approval, c.store.CreateApproval(ctx, approval)
	}
	if err := c.store.CreateApproval(ctx, approval); err != nil {
		return Approval{}, err
	}
	c.notifyApproval(approval)
	audience, audienceRun := approval.SessionID, approval.RunID
	if subagent {
		audience, audienceRun = task.SessionID, task.RunID
	}
	c.publishEvent(events.Event{
		Type:      EventApprovalRequest,
		SessionID: audience,
		RunID:     audienceRun,
		Payload: PermissionRequest{
			ID:          approval.ID,
			SessionID:   approval.SessionID,
			TaskID:      approval.TaskID,
			AgentName:   approval.AgentName,
			ToolCallID:  prepared.ToolCallID,
			ToolName:    approval.ToolName,
			Description: approval.Description,
			Params:      approval.Params,
			Path:        approval.Path,
			Suggestion:  approval.Suggestion,
		},
	})
	c.publishToolUpdate(prepared.SessionID, approval.RunID, ToolUpdate{
		ToolCallID: prepared.ToolCallID,
		ToolName:   prepared.ToolName,
		State:      ToolLifecycleWaitingApproval,
		RunID:      approval.RunID,
		SessionID:  prepared.SessionID,
		ApprovalID: approval.ID,
	})
	if subagent {
		return approval, c.deliverSubagentApproval(ctx, approval, task)
	}
	return approval, nil
}

func (c *Core) finishToolCall(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput, result tools.Result) (transcript.Message, *transcript.Message, error) {
	toolCallMessage := agent.ToolCallMessage(prepared.ToolCallID, prepared.SessionID, prepared.RunID, prepared.ToolName, input.Args, true, prepared.Message.CreatedAt)
	toolCallMessage.UpdatedAt = c.now().UTC()
	if err := c.store.UpdateMessage(ctx, toolCallMessage); err != nil {
		return transcript.Message{}, nil, err
	}
	c.publishEvent(events.Event{
		Type:      EventMessageUpdated,
		SessionID: prepared.SessionID,
		RunID:     toolCallMessage.RunID,
		Payload:   toolCallMessage,
	})

	resultMessage, err := c.saveToolResultMessage(ctx, prepared, result)
	if err != nil {
		return transcript.Message{}, nil, err
	}
	c.publishFinishedToolUpdate(prepared, resultMessage.ID, result)
	return toolCallMessage, resultMessage, nil
}

func (c *Core) saveToolResultMessage(ctx context.Context, prepared preparedToolCall, result tools.Result) (*transcript.Message, error) {
	message, err := agent.ToolResultMessage(c.newID("tool_result"), prepared.SessionID, prepared.RunID, prepared.ToolCallID, prepared.ToolName, result, c.now().UTC())
	if err != nil {
		return nil, err
	}
	if _, err := c.store.AppendMessage(ctx, message); err != nil {
		return nil, err
	}
	c.publishEvent(events.Event{Type: EventMessageCreated, SessionID: prepared.SessionID, RunID: message.RunID, Payload: message})
	return &message, nil
}

func (c *Core) publishFinishedToolUpdate(prepared preparedToolCall, resultMessageID string, result tools.Result) {
	toolState := ToolLifecycleCompleted
	if result.IsError() {
		toolState = ToolLifecycleFailed
	}
	c.publishToolUpdate(prepared.SessionID, prepared.RunID, ToolUpdate{
		ToolCallID:      prepared.ToolCallID,
		ToolName:        prepared.ToolName,
		State:           toolState,
		ResultStatus:    string(agent.ToolResultStatus(result)),
		RunID:           prepared.RunID,
		SessionID:       prepared.SessionID,
		ResultMessageID: resultMessageID,
		Error:           errorText(result),
	})
}

func (c *Core) publishToolUpdate(sessionID string, runID string, update ToolUpdate) {
	c.publishEvent(events.Event{
		Type:      EventToolUpdated,
		SessionID: sessionID,
		RunID:     runID,
		Payload:   update,
	})
}

func marshalJSONRaw(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

func errorText(result tools.Result) string {
	if result.IsError() {
		return strings.TrimSpace(result.Content)
	}
	return ""
}
