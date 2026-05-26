package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const delegateTaskToolName = "delegate_task"

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
	})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		Content:  delegateTaskResultContent(result),
		Metadata: result.Task,
		IsError:  result.IsError,
		Status:   delegateTaskResultStatus(result),
	}, nil
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
}

type DelegateTaskResult struct {
	Task    SubagentTask
	Summary string
	IsError bool
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

	runtime := normalizeSubagentRuntime(input.Runtime)
	workingDir := normalizeWorkingDir(input.WorkingDir)
	if workingDir == "" {
		workingDir = parent.WorkingDir
	}

	child, err := c.createSubagentSession(ctx, parent, runtime, input.Model, workingDir, goal)
	if err != nil {
		return DelegateTaskResult{}, err
	}
	run, err := c.createSubagentRun(ctx, child, subagentUserPrompt(goal, input.Context, workingDir))
	if err != nil {
		return DelegateTaskResult{}, err
	}

	task := SubagentTask{
		ID:               c.newID("subagent"),
		ParentSessionID:  parent.ID,
		ParentRunID:      normalizeText(input.ParentRunID),
		ParentToolCallID: normalizeText(input.ParentToolCallID),
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

	execErr := c.ExecuteRun(ctx, run.ID)
	summary, failed := c.subagentRunSummary(ctx, child.ID, run.ID, execErr)
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
	return DelegateTaskResult{Task: task, Summary: summary, IsError: failed}, nil
}

func (c *Core) createSubagentSession(ctx context.Context, parent Session, runtime SubagentRuntime, model string, workingDir string, goal string) (Session, error) {
	title := "Subagent: " + truncateForTitle(goal, 64)
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
