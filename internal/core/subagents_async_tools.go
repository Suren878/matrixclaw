package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const (
	spawnSubagentToolName      = "spawn_subagent"
	listSubagentsToolName      = "list_subagents"
	readSubagentResultToolName = "read_subagent_result"
	maxActiveAsyncSubagents    = 4
)

type spawnSubagentInput struct {
	Name       string `json:"name,omitempty"`
	Goal       string `json:"goal"`
	Context    string `json:"context,omitempty"`
	Runtime    string `json:"runtime,omitempty"`
	Model      string `json:"model,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`
	Isolation  string `json:"isolation,omitempty"`
}

type listSubagentsInput struct {
	IncludeRecent bool `json:"include_recent,omitempty"`
	Limit         int  `json:"limit,omitempty"`
}

type readSubagentResultInput struct {
	TaskID string `json:"task_id,omitempty"`
	Name   string `json:"name,omitempty"`
}

type spawnSubagentTool struct {
	app *Core
}

type listSubagentsTool struct {
	app *Core
}

type readSubagentResultTool struct {
	app *Core
}

func SubagentToolExecutors(app *Core) []tools.Executor {
	return []tools.Executor{
		DelegateTaskToolExecutor(app),
		SpawnSubagentToolExecutor(app),
		ListSubagentsToolExecutor(app),
		ReadSubagentResultToolExecutor(app),
	}
}

func SpawnSubagentToolExecutor(app *Core) tools.Executor {
	return &spawnSubagentTool{app: app}
}

func ListSubagentsToolExecutor(app *Core) tools.Executor {
	return &listSubagentsTool{app: app}
}

func ReadSubagentResultToolExecutor(app *Core) tools.Executor {
	return &readSubagentResultTool{app: app}
}

func (t *spawnSubagentTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              spawnSubagentToolName,
		Name:            "SpawnSubagent",
		Description:     "Start an independent hidden child subagent in the background and return a task handle immediately.",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectMutation,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "core.subagents",
		Category:        tools.CategoryAutomation,
		Profiles:        []tools.Profile{tools.ProfileCoding},
		OutputKind:      tools.OutputText,
		InputJSONSchema: spawnSubagentToolSchema,
	}
}

func (t *spawnSubagentTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.app == nil {
		return tools.Result{}, fmt.Errorf("%w: spawn subagent core unavailable", ErrExecutionUnavailable)
	}
	var input spawnSubagentInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{}, tools.InvalidArgs(spawnSubagentToolName, err)
	}
	result, err := t.app.SpawnSubagent(ctx, SpawnSubagentInput{
		ParentSessionID:  call.SessionID,
		ParentRunID:      call.RunID,
		ParentToolCallID: call.ToolCallID,
		Name:             input.Name,
		Goal:             input.Goal,
		Context:          input.Context,
		Runtime:          input.Runtime,
		Model:            input.Model,
		WorkingDir:       input.WorkingDir,
		Isolation:        input.Isolation,
	})
	if err != nil {
		return tools.Result{}, err
	}
	content := spawnSubagentResultContent(result)
	return tools.Result{
		Content:  content,
		Metadata: result.Task,
		Status:   tools.ResultStatusNeutral,
	}, nil
}

func (t *listSubagentsTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              listSubagentsToolName,
		Name:            "ListSubagents",
		Description:     "List active and optionally recent async subagents for the current parent session.",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "core.subagents",
		Category:        tools.CategoryAutomation,
		Profiles:        []tools.Profile{tools.ProfileCoding, tools.ProfileReadOnly},
		OutputKind:      tools.OutputText,
		InputJSONSchema: listSubagentsToolSchema,
	}
}

func (t *listSubagentsTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.app == nil {
		return tools.Result{}, fmt.Errorf("%w: list subagents core unavailable", ErrExecutionUnavailable)
	}
	var input listSubagentsInput
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &input); err != nil {
			return tools.Result{}, tools.InvalidArgs(listSubagentsToolName, err)
		}
	}
	tasks, err := t.app.ListSubagents(ctx, call.SessionID, input.IncludeRecent, input.Limit)
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		Content:  formatSubagentTaskList(tasks),
		Metadata: tasks,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

func (t *readSubagentResultTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              readSubagentResultToolName,
		Name:            "ReadSubagentResult",
		Description:     "Read status, summary, and recent transcript details for one async subagent task.",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "core.subagents",
		Category:        tools.CategoryAutomation,
		Profiles:        []tools.Profile{tools.ProfileCoding, tools.ProfileReadOnly},
		OutputKind:      tools.OutputText,
		InputJSONSchema: readSubagentResultToolSchema,
	}
}

func (t *readSubagentResultTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.app == nil {
		return tools.Result{}, fmt.Errorf("%w: read subagent core unavailable", ErrExecutionUnavailable)
	}
	var input readSubagentResultInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{}, tools.InvalidArgs(readSubagentResultToolName, err)
	}
	task, detail, err := t.app.ReadSubagentResult(ctx, call.SessionID, input.TaskID, input.Name)
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		Content:  detail,
		Metadata: task,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

func spawnSubagentResultContent(result SpawnSubagentResult) string {
	task := result.Task
	prefix := "started"
	if result.Replayed {
		prefix = "already running"
		if subagentTaskTerminalStatus(task.Status) {
			prefix = "already finished"
		}
	}
	lines := []string{
		fmt.Sprintf("Subagent %s %s", subagentTaskAgentName(task), prefix),
		"Task ID: " + task.ID,
		"Status: " + string(task.Status),
	}
	if taskLabel := strings.TrimSpace(task.DisplayName); taskLabel != "" {
		lines = append(lines, "Task: "+taskLabel)
	}
	if goal := strings.TrimSpace(task.Goal); goal != "" {
		lines = append(lines, "Goal: "+goal)
	}
	return strings.Join(lines, "\n")
}

func formatSubagentTaskList(tasks []SubagentTask) string {
	if len(tasks) == 0 {
		return "No subagents."
	}
	var lines []string
	for _, task := range tasks {
		line := fmt.Sprintf("- %s [%s] %s", subagentTaskAgentName(task), task.Status, task.ID)
		if taskLabel := strings.TrimSpace(task.DisplayName); taskLabel != "" {
			line += " - " + taskLabel
		}
		if task.Summary != "" && subagentTaskTerminalStatus(task.Status) {
			line += ": " + strings.TrimSpace(task.Summary)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (c *Core) subagentTaskDetail(task SubagentTask) string {
	lines := []string{
		fmt.Sprintf("Subagent: %s", subagentTaskAgentName(task)),
		"Task ID: " + task.ID,
		"Work job: " + task.ID,
		"Status: " + string(task.Status),
		"Runtime: " + task.Runtime,
	}
	if taskLabel := strings.TrimSpace(task.DisplayName); taskLabel != "" {
		lines = append(lines, "Task: "+taskLabel)
	}
	lines = append(lines, "Goal: "+task.Goal)
	if task.Summary != "" {
		lines = append(lines, "", "Summary:", task.Summary)
	}
	if task.Error != "" {
		lines = append(lines, "", "Error:", task.Error)
	}
	return strings.Join(lines, "\n")
}

func subagentTaskToolResultStatus(task SubagentTask) tools.ResultStatus {
	if subagentTaskFailed(task) {
		return tools.ResultStatusError
	}
	return tools.ResultStatusSuccess
}

func subagentTaskToolLifecycleState(task SubagentTask) ToolLifecycleState {
	if subagentTaskFailed(task) {
		return ToolLifecycleFailed
	}
	if subagentTaskTerminalStatus(task.Status) {
		return ToolLifecycleCompleted
	}
	if task.Status == SubagentTaskStatusWaitingApproval {
		return ToolLifecycleWaitingApproval
	}
	return ToolLifecycleRequested
}

var spawnSubagentToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "Short display name for the subagent in the UI."},
    "goal": {"type": "string", "description": "The bounded task for the child subagent."},
    "context": {"type": "string", "description": "Optional minimal context to include in the child prompt."},
    "runtime": {"type": "string", "enum": ["matrixclaw", "codex", "claude", "auto"], "description": "Subagent runtime. Defaults to matrixclaw."},
    "model": {"type": "string", "description": "Optional model override for the child runtime."},
    "working_dir": {"type": "string", "description": "Optional working directory for the child session."},
    "isolation": {"type": "string", "enum": ["shared", "worktree"], "description": "Use shared for read-only/research tasks; use worktree for independent write-heavy tasks."}
  },
  "required": ["goal"],
  "additionalProperties": false
}`)

var listSubagentsToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "include_recent": {"type": "boolean", "description": "Include recently completed or failed subagents as well as active ones."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Maximum number of subagents to return."}
  },
  "additionalProperties": false
}`)

var readSubagentResultToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {"type": "string", "description": "Subagent task id returned by spawn_subagent or list_subagents."},
    "name": {"type": "string", "description": "Display name to resolve within the current parent session when task_id is unknown."}
  },
  "additionalProperties": false
}`)
