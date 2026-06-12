package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const delegateTaskToolName = "delegate_task"
const subagentApprovalBridgeSource = "subagent_approval_bridge"

var subagentAgentNamePool = []string{"Neo", "Trinity", "Morpheus", "Niobe", "Seraph", "Oracle", "Link", "Switch", "Apoc", "Tank", "Dozer", "Mouse"}

type delegateTaskInput struct {
	Goal       string `json:"goal"`
	Context    string `json:"context,omitempty"`
	Runtime    string `json:"runtime,omitempty"`
	Model      string `json:"model,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`
}

type delegateTaskTool struct {
	app *Core
}

func DelegateTaskToolExecutor(app *Core) tools.Executor {
	return &delegateTaskTool{app: app}
}

func (t *delegateTaskTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              delegateTaskToolName,
		Name:            "DelegateTask",
		Description:     "Delegate a bounded task to a hidden child subagent and return only its summary.",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectMutation,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "core.subagents",
		Category:        tools.CategoryAutomation,
		Profiles:        []tools.Profile{tools.ProfileCoding},
		OutputKind:      tools.OutputText,
		InputJSONSchema: delegateTaskToolSchema,
	}
}

func (t *delegateTaskTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.app == nil {
		return tools.Result{}, fmt.Errorf("%w: delegate task core unavailable", ErrExecutionUnavailable)
	}
	var input delegateTaskInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{}, tools.InvalidArgs(delegateTaskToolName, err)
	}
	result, err := t.app.DelegateTask(ctx, DelegateTaskInput{
		ParentSessionID:  call.SessionID,
		ParentRunID:      call.RunID,
		ParentToolCallID: call.ToolCallID,
		Goal:             input.Goal,
		Context:          input.Context,
		Runtime:          input.Runtime,
		Model:            input.Model,
		WorkingDir:       input.WorkingDir,
		Approved:         call.Approved,
	})
	if err != nil {
		return tools.Result{}, err
	}
	out := tools.Result{
		Content:  delegateTaskResultContent(result),
		Metadata: result.Task,
		IsError:  result.IsError,
		Status:   delegateTaskResultStatus(result),
	}
	if result.Approval != nil {
		out.Approval = result.Approval
	}
	return out, nil
}

type DelegateTaskInput struct {
	ParentSessionID  string
	ParentRunID      string
	ParentToolCallID string
	Goal             string
	Context          string
	Runtime          string
	Model            string
	WorkingDir       string
	Approved         bool
}

type DelegateTaskResult struct {
	Task     SubagentTask
	Summary  string
	IsError  bool
	Approval *tools.ApprovalRequest
}

type subagentApprovalBridgeParams struct {
	Source              string          `json:"source"`
	TaskID              string          `json:"task_id"`
	ChildSessionID      string          `json:"child_session_id"`
	ChildRunID          string          `json:"child_run_id"`
	ChildApprovalID     string          `json:"child_approval_id"`
	ChildToolCallID     string          `json:"child_tool_call_id"`
	ChildToolName       string          `json:"child_tool_name,omitempty"`
	SubagentTitle       string          `json:"subagent_title,omitempty"`
	Runtime             string          `json:"runtime,omitempty"`
	OriginalAction      string          `json:"original_action,omitempty"`
	OriginalDescription string          `json:"original_description,omitempty"`
	OriginalParams      json.RawMessage `json:"original_params,omitempty"`
}

func (c *Core) DelegateTask(ctx context.Context, input DelegateTaskInput) (DelegateTaskResult, error) {
	parentSessionID := normalizeText(input.ParentSessionID)
	if parentSessionID == "" {
		return DelegateTaskResult{}, fmt.Errorf("%w: parent session id is required", ErrInvalidInput)
	}
	goal := normalizeText(input.Goal)
	if goal == "" {
		return DelegateTaskResult{}, fmt.Errorf("%w: goal is required", ErrInvalidInput)
	}
	parent, err := c.store.GetSession(ctx, parentSessionID)
	if err != nil {
		return DelegateTaskResult{}, err
	}
	parent = c.decorateSessionLLM(parent)
	if CoreSessionIsExternalAgent(parent) {
		return DelegateTaskResult{}, fmt.Errorf("%w: delegate_task is available for Matrixclaw sessions only", ErrInvalidInput)
	}
	if isSubagentSession(parent) {
		return DelegateTaskResult{}, fmt.Errorf("%w: child subagents cannot delegate tasks", ErrInvalidInput)
	}

	parentRunID := normalizeText(input.ParentRunID)
	parentToolCallID := normalizeText(input.ParentToolCallID)
	if parentRunID != "" && parentToolCallID != "" {
		existing, err := c.store.GetSubagentTaskByParentToolCall(ctx, parent.ID, parentRunID, parentToolCallID)
		if err == nil {
			return c.resumeSubagentTask(ctx, existing)
		}
		if !errors.Is(err, ErrNotFound) {
			return DelegateTaskResult{}, err
		}
	}

	runtime := normalizeSubagentRuntime(input.Runtime)
	workingDir := normalizeWorkingDir(input.WorkingDir)
	if workingDir == "" {
		workingDir = parent.WorkingDir
	}
	displayName := generatedSubagentDisplayName(goal)
	agentName, err := c.assignSubagentAgentName(ctx, parent.ID)
	if err != nil {
		return DelegateTaskResult{}, err
	}

	child, err := c.createSubagentSession(ctx, parent, runtime, input.Model, workingDir, agentName)
	if err != nil {
		return DelegateTaskResult{}, err
	}
	run, err := c.createSubagentRun(ctx, child, subagentUserPrompt(goal, input.Context, workingDir))
	if err != nil {
		return DelegateTaskResult{}, err
	}

	task := SubagentTask{
		ID:               c.newID("subagent"),
		AgentName:        agentName,
		DisplayName:      displayName,
		Mode:             SubagentTaskModeBlocking,
		Isolation:        SubagentIsolationShared,
		ParentSessionID:  parent.ID,
		ParentRunID:      parentRunID,
		ParentToolCallID: parentToolCallID,
		ChildSessionID:   child.ID,
		ChildRunID:       run.ID,
		Runtime:          subagentTaskRuntimeLabel(runtime, child),
		Goal:             goal,
		Status:           SubagentTaskStatusRunning,
		CreatedAt:        c.now().UTC(),
		UpdatedAt:        c.now().UTC(),
	}
	if err := c.store.CreateSubagentTask(ctx, task); err != nil {
		return DelegateTaskResult{}, err
	}
	c.createSubagentWorkJob(ctx, task)
	c.publishSubagentTaskUpdated(task)

	execErr := c.ExecuteRun(ctx, run.ID)
	return c.finishOrBridgeSubagentTask(ctx, task, execErr)
}

func (c *Core) resumeSubagentTask(ctx context.Context, task SubagentTask) (DelegateTaskResult, error) {
	if task.Status == SubagentTaskStatusCompleted {
		return DelegateTaskResult{Task: task, Summary: task.Summary}, nil
	}
	if task.Status == SubagentTaskStatusFailed && task.FinishedAt != nil {
		summary := strings.TrimSpace(task.Summary)
		if summary == "" {
			summary = strings.TrimSpace(task.Error)
		}
		return DelegateTaskResult{Task: task, Summary: summary, IsError: true}, nil
	}
	return c.finishOrBridgeSubagentTask(ctx, task, nil)
}

func (c *Core) finishOrBridgeSubagentTask(ctx context.Context, task SubagentTask, execErr error) (DelegateTaskResult, error) {
	run, err := c.store.GetRun(ctx, task.ChildRunID)
	if err != nil {
		if execErr != nil {
			return c.finishSubagentTask(ctx, task, "Subagent failed: "+execErr.Error(), true)
		}
		return DelegateTaskResult{}, err
	}
	if execErr == nil && run.Status == RunStatusWaitingApproval {
		approval, err := c.pendingApprovalForRun(ctx, task.ChildSessionID, task.ChildRunID)
		if err != nil {
			return DelegateTaskResult{}, err
		}
		task.Status = SubagentTaskStatusWaitingApproval
		task.UpdatedAt = c.now().UTC()
		if err := c.store.UpdateSubagentTask(ctx, task); err != nil {
			return DelegateTaskResult{}, err
		}
		c.updateSubagentWorkJob(ctx, task)
		request, err := c.subagentApprovalRequest(ctx, task, approval)
		if err != nil {
			return DelegateTaskResult{}, err
		}
		return DelegateTaskResult{
			Task:     task,
			Summary:  "Subagent is waiting for permission.",
			Approval: request,
		}, nil
	}
	summary, failed := c.subagentRunSummary(ctx, task.ChildSessionID, task.ChildRunID, execErr)
	return c.finishSubagentTask(ctx, task, summary, failed)
}

func (c *Core) finishSubagentTask(ctx context.Context, task SubagentTask, summary string, failed bool) (DelegateTaskResult, error) {
	task.Summary = summary
	task.UpdatedAt = c.now().UTC()
	finishedAt := task.UpdatedAt
	task.FinishedAt = &finishedAt
	if failed {
		task.Status = SubagentTaskStatusFailed
		task.Error = summary
	} else {
		task.Status = SubagentTaskStatusCompleted
	}
	if err := c.store.UpdateSubagentTask(ctx, task); err != nil {
		return DelegateTaskResult{}, err
	}
	c.updateSubagentWorkJob(ctx, task)
	return DelegateTaskResult{Task: task, Summary: summary, IsError: failed}, nil
}

func (c *Core) pendingApprovalForRun(ctx context.Context, sessionID string, runID string) (Approval, error) {
	approvals, err := c.store.ListApprovals(ctx, sessionID, ApprovalStatePending)
	if err != nil {
		return Approval{}, err
	}
	for _, approval := range approvals {
		if strings.TrimSpace(approval.RunID) == strings.TrimSpace(runID) {
			return approval, nil
		}
	}
	return Approval{}, ErrNotFound
}

func (c *Core) subagentApprovalRequest(ctx context.Context, task SubagentTask, childApproval Approval) (*tools.ApprovalRequest, error) {
	child, err := c.store.GetSession(ctx, task.ChildSessionID)
	if err != nil {
		return nil, err
	}
	params := subagentApprovalBridgeParams{
		Source:              subagentApprovalBridgeSource,
		TaskID:              task.ID,
		ChildSessionID:      task.ChildSessionID,
		ChildRunID:          task.ChildRunID,
		ChildApprovalID:     childApproval.ID,
		ChildToolCallID:     childApproval.ToolCallRef,
		ChildToolName:       childApproval.ToolName,
		SubagentTitle:       firstNonEmpty(strings.TrimSpace(task.AgentName), firstNonEmpty(strings.TrimSpace(task.DisplayName), strings.TrimSpace(child.Title))),
		Runtime:             task.Runtime,
		OriginalAction:      childApproval.Action,
		OriginalDescription: childApproval.Description,
		OriginalParams:      childApproval.Params,
	}
	description := fmt.Sprintf("Subagent %q requested approval for %s", firstNonEmpty(subagentTaskAgentName(task), firstNonEmpty(child.Title, task.Runtime)), firstNonEmpty(childApproval.ToolName, "a tool"))
	if detail := strings.TrimSpace(childApproval.Description); detail != "" {
		description += ": " + detail
	}
	return &tools.ApprovalRequest{
		ToolID:      subagentParentToolName(task),
		ToolCallID:  task.ParentToolCallID,
		Action:      childApproval.Action,
		Path:        childApproval.Path,
		Description: description,
		Params:      params,
	}, nil
}

func decodeSubagentApprovalBridge(approval Approval) (subagentApprovalBridgeParams, bool) {
	var params subagentApprovalBridgeParams
	if len(approval.Params) == 0 {
		return params, false
	}
	if err := json.Unmarshal(approval.Params, &params); err != nil {
		return params, false
	}
	if params.Source != subagentApprovalBridgeSource {
		return params, false
	}
	if strings.TrimSpace(params.ChildApprovalID) == "" || strings.TrimSpace(params.ChildRunID) == "" {
		return params, false
	}
	return params, true
}

func (c *Core) resumeParentAfterSubagentTerminal(ctx context.Context, task SubagentTask) error {
	status, err := c.subagentTaskRunStatus(ctx, task)
	if err != nil {
		return err
	}
	if subagentRunStatusTerminal(status) {
		return c.startRun(ctx, task.ParentRunID)
	}
	if status == RunStatusWaitingApproval {
		return c.mirrorPendingSubagentApproval(ctx, task)
	}
	safego.Go("core.waitSubagentTerminalAndResumeParent", func() {
		c.waitForSubagentTerminalAndResumeParent(task)
	})
	return nil
}

func (c *Core) waitForSubagentTerminalAndResumeParent(task SubagentTask) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	ctx := context.Background()
	for range ticker.C {
		status, err := c.subagentTaskRunStatus(ctx, task)
		if err != nil {
			continue
		}
		if subagentRunStatusTerminal(status) {
			_ = c.startRun(ctx, task.ParentRunID)
			return
		}
		if status == RunStatusWaitingApproval {
			_ = c.mirrorPendingSubagentApproval(ctx, task)
			return
		}
	}
}

func (c *Core) subagentTaskTerminal(ctx context.Context, task SubagentTask) (bool, error) {
	status, err := c.subagentTaskRunStatus(ctx, task)
	if err != nil {
		return false, err
	}
	return subagentRunStatusTerminal(status), nil
}

func (c *Core) subagentTaskRunStatus(ctx context.Context, task SubagentTask) (RunStatus, error) {
	run, err := c.store.GetRun(ctx, task.ChildRunID)
	if err != nil {
		return "", err
	}
	return run.Status, nil
}

func subagentRunStatusTerminal(status RunStatus) bool {
	return status == RunStatusCompleted || status == RunStatusFailed || status == RunStatusCanceled
}

func (c *Core) mirrorPendingSubagentApproval(ctx context.Context, task SubagentTask) error {
	childApproval, err := c.pendingApprovalForRun(ctx, task.ChildSessionID, task.ChildRunID)
	if err != nil {
		return err
	}
	existing, err := c.subagentBridgeApprovalForChild(ctx, task, childApproval.ID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(existing.ID) != "" {
		return nil
	}
	task.Status = SubagentTaskStatusWaitingApproval
	task.UpdatedAt = c.now().UTC()
	if err := c.store.UpdateSubagentTask(ctx, task); err != nil {
		return err
	}
	c.updateSubagentWorkJob(ctx, task)
	request, err := c.subagentApprovalRequest(ctx, task, childApproval)
	if err != nil {
		return err
	}
	prepared := preparedToolCall{
		SessionID:  task.ParentSessionID,
		RunID:      task.ParentRunID,
		ToolName:   subagentParentToolName(task),
		ToolCallID: task.ParentToolCallID,
	}
	if _, _, created, createErr := c.createPendingApproval(ctx, prepared, ExecuteToolInput{}, tools.Result{Approval: request}, nil); createErr != nil {
		return createErr
	} else if !created {
		return nil
	}
	if strings.TrimSpace(task.ParentRunID) == "" {
		return nil
	}
	run, err := c.store.GetRun(ctx, task.ParentRunID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if subagentRunStatusTerminal(run.Status) {
		return nil
	}
	return c.setRunStatus(ctx, &run, RunStatusWaitingApproval, "")
}

func (c *Core) subagentBridgeApprovalForChild(ctx context.Context, task SubagentTask, childApprovalID string) (Approval, error) {
	approvals, err := c.store.ListApprovals(ctx, task.ParentSessionID, "")
	if err != nil {
		return Approval{}, err
	}
	childApprovalID = strings.TrimSpace(childApprovalID)
	for _, approval := range approvals {
		bridge, ok := decodeSubagentApprovalBridge(approval)
		if !ok {
			continue
		}
		if strings.TrimSpace(bridge.ChildApprovalID) == childApprovalID {
			return approval, nil
		}
	}
	return Approval{}, nil
}

func (c *Core) createSubagentSession(ctx context.Context, parent Session, runtime SubagentRuntime, model string, workingDir string, displayName string) (Session, error) {
	title := "Subagent: " + truncateForTitle(firstNonEmpty(displayName, "Task"), 64)
	switch runtime {
	case SubagentRuntimeCodex, SubagentRuntimeClaude:
		agentID := string(runtime)
		canonical, ok := c.ResolveExternalAgentID(agentID)
		if !ok {
			return Session{}, fmt.Errorf("%w: external agent %q is not configured", ErrExecutionUnavailable, agentID)
		}
		return c.CreateSession(ctx, CreateSessionInput{
			Title:           title,
			Kind:            SessionKindExternalAgent,
			RuntimeID:       SessionRuntimeExternalAgent,
			ParentSessionID: parent.ID,
			Hidden:          true,
			WorkingDir:      workingDir,
			ModelID:         normalizeText(model),
			PermissionMode:  PermissionModeFullAuto,
			ExternalAgentID: canonical,
		})
	default:
		return c.CreateSession(ctx, CreateSessionInput{
			Title:           title,
			Kind:            SessionKindAssistant,
			RuntimeID:       SessionRuntimeMatrixClaw,
			ParentSessionID: parent.ID,
			Hidden:          true,
			WorkingDir:      workingDir,
			ProviderID:      parent.ProviderID,
			ModelID:         firstNonEmpty(normalizeText(model), parent.ModelID),
			PermissionMode:  parent.PermissionMode,
		})
	}
}

func (c *Core) assignSubagentAgentName(ctx context.Context, parentSessionID string) (string, error) {
	tasks, err := c.store.ListSubagentTasks(ctx, SubagentTaskFilter{ParentSessionID: strings.TrimSpace(parentSessionID)})
	if err != nil {
		return "", err
	}
	used := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		name := strings.TrimSpace(task.AgentName)
		if name == "" {
			continue
		}
		used[strings.ToLower(name)] = struct{}{}
	}
	for cycle := 1; ; cycle++ {
		for _, base := range subagentAgentNamePool {
			name := base
			if cycle > 1 {
				name = fmt.Sprintf("%s-%d", base, cycle)
			}
			if _, ok := used[strings.ToLower(name)]; !ok {
				return name, nil
			}
		}
	}
}

func subagentTaskAgentName(task SubagentTask) string {
	if name := strings.Join(strings.Fields(task.AgentName), " "); name != "" {
		return name
	}
	if name := strings.Join(strings.Fields(task.DisplayName), " "); name != "" {
		return name
	}
	if id := strings.TrimSpace(task.ID); id != "" {
		return id
	}
	return "subagent"
}

func (c *Core) createSubagentRun(ctx context.Context, session Session, prompt string) (Run, error) {
	now := c.now().UTC()
	run := Run{
		ID:            c.newID("run"),
		SessionID:     session.ID,
		UserMessageID: c.newID("msg"),
		Status:        RunStatusAccepted,
		StartedAt:     now,
		UpdatedAt:     now,
	}
	message := Message{
		ID:        run.UserMessageID,
		SessionID: session.ID,
		RunID:     run.ID,
		Role:      MessageRoleUser,
		Content:   prompt,
		Parts:     NormalizeMessageParts(prompt, nil),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := c.store.AcceptMessage(ctx, message, run); err != nil {
		return Run{}, err
	}
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: session.ID, RunID: run.ID, Payload: message})
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: session.ID, RunID: run.ID, Payload: run})
	return run, nil
}

func (c *Core) subagentRunSummary(ctx context.Context, sessionID string, runID string, execErr error) (string, bool) {
	if execErr != nil {
		return "Subagent failed: " + execErr.Error(), true
	}
	run, err := c.store.GetRun(ctx, runID)
	if err != nil {
		return "Subagent failed: " + err.Error(), true
	}
	switch run.Status {
	case RunStatusCompleted:
	case RunStatusWaitingApproval:
		return "Subagent requested approval and cannot continue without user interaction in this version.", true
	case RunStatusFailed, RunStatusCanceled:
		if strings.TrimSpace(run.Error) != "" {
			return "Subagent failed: " + strings.TrimSpace(run.Error), true
		}
		return "Subagent failed with status " + string(run.Status) + ".", true
	default:
		return "Subagent stopped with status " + string(run.Status) + ".", true
	}
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return "Subagent failed: " + err.Error(), true
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].RunID != runID || messages[i].Role != MessageRoleAssistant {
			continue
		}
		if summary := strings.TrimSpace(messages[i].Content); summary != "" {
			return summary, false
		}
	}
	return "Subagent completed without a text summary.", false
}

func normalizeSubagentRuntime(runtime string) SubagentRuntime {
	switch strings.ToLower(strings.TrimSpace(runtime)) {
	case "", string(SubagentRuntimeMatrixClaw), string(SubagentRuntimeAuto):
		return SubagentRuntimeMatrixClaw
	case string(SubagentRuntimeCodex), "codex-app", "openai-codex":
		return SubagentRuntimeCodex
	case string(SubagentRuntimeClaude), "claude-code", "claudecode":
		return SubagentRuntimeClaude
	default:
		return SubagentRuntime(strings.ToLower(strings.TrimSpace(runtime)))
	}
}

func subagentTaskRuntimeLabel(runtime SubagentRuntime, child Session) string {
	if CoreSessionIsExternalAgent(child) && strings.TrimSpace(child.ExternalAgentID) != "" {
		return strings.TrimSpace(child.ExternalAgentID)
	}
	return string(runtime)
}

func subagentUserPrompt(goal string, contextText string, workingDir string) string {
	lines := []string{
		"Delegated task:",
		strings.TrimSpace(goal),
	}
	if contextText = strings.TrimSpace(contextText); contextText != "" {
		lines = append(lines, "", "Context:", contextText)
	}
	if workingDir = strings.TrimSpace(workingDir); workingDir != "" {
		lines = append(lines, "", "Working directory:", workingDir)
	}
	lines = append(lines, "", "Return a concise result for the parent agent. Include important files, findings, errors, and verification output. Do not ask the user questions.")
	return strings.Join(lines, "\n")
}

func subagentSystemPrompt() string {
	return "Subagent mode:\n- You are a child agent working for a parent Matrixclaw agent.\n- Complete only the delegated task from the user message.\n- Do not ask the user for input or approval.\n- Return a concise summary for the parent agent, listing important files, findings, errors, and verification output."
}

func isSubagentSession(session Session) bool {
	return strings.TrimSpace(session.ParentSessionID) != "" || session.Hidden
}

func subagentToolAllowed(spec tools.Spec) bool {
	id := strings.ToLower(strings.TrimSpace(spec.ID))
	if id == delegateTaskToolName || id == "memory" || strings.HasPrefix(id, "plan_") || id == "text_to_speech" {
		return false
	}
	namespace := strings.ToLower(strings.TrimSpace(spec.Namespace))
	if namespace == "core.memory" || namespace == "core.plan" {
		return false
	}
	switch spec.Category {
	case tools.CategoryAutomation, tools.CategoryStorage, tools.CategorySkills:
		return false
	}
	return true
}

func delegateTaskResultContent(result DelegateTaskResult) string {
	summary := strings.TrimSpace(result.Summary)
	if summary == "" {
		summary = "Subagent completed without a text summary."
	}
	return summary
}

func delegateTaskResultStatus(result DelegateTaskResult) tools.ResultStatus {
	if result.IsError {
		return tools.ResultStatusError
	}
	return tools.ResultStatusSuccess
}

func truncateForTitle(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

var delegateTaskToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "goal": {"type": "string", "description": "The concrete task for the child subagent."},
    "context": {"type": "string", "description": "Optional context to include in the child prompt."},
    "runtime": {"type": "string", "enum": ["matrixclaw", "codex", "claude", "auto"], "description": "Subagent runtime. Defaults to matrixclaw."},
    "model": {"type": "string", "description": "Optional model override for the child runtime."},
    "working_dir": {"type": "string", "description": "Optional working directory for the child session."}
  },
  "required": ["goal"],
  "additionalProperties": false
}`)
