package core

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const (
	agentToolName           = "agent"
	agentSteerToolName      = "agent_steer"
	worktreeMergeToolName   = "worktree_merge"
	worktreeDiscardToolName = "worktree_discard"
)

type agentToolInput struct {
	Description  string          `json:"description"`
	Prompt       string          `json:"prompt"`
	AgentType    string          `json:"agent_type,omitempty"`
	Background   bool            `json:"background,omitempty"`
	Isolation    string          `json:"isolation,omitempty"`
	Readonly     bool            `json:"readonly,omitempty"`
	Runtime      string          `json:"runtime,omitempty"`
	Model        string          `json:"model,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
}

type agentTool struct {
	app *Core
}

// AgentToolExecutors returns the agent tool and the tools that steer its
// background children and merge or drop its worktree children's work.
func AgentToolExecutors(app *Core) []tools.Executor {
	return []tools.Executor{&agentTool{app: app}, &agentSteerTool{app: app}, &worktreeMergeTool{app: app}, &worktreeDiscardTool{app: app}}
}

func (t *agentTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              agentToolName,
		Description:     "Start a child agent on a self-contained task. The child sees none of this conversation, so prompt must hold the goal, the relevant paths and findings, the constraints, what done means and what to report back. By default the call waits and returns the child's final report. agent_type picks the kind of child (see your Subagents guidance); readonly: true gives any type read-only tools; read-only children in one reply run in parallel. A writing child works in your directory (isolation shared; blocking writers take turns) or in its own git worktree started from your current files (isolation worktree; several run at once, and the result names the branch to merge with worktree_merge). background: true returns a task id at once; the report arrives later as a message, and await waits for it. output_schema makes the report a JSON value matching that schema.",
		Effect:          tools.EffectMutation,
		Namespace:       "core.subagents",
		Category:        tools.CategoryAutomation,
		InputJSONSchema: agentToolSchema,
	}
}

// ConcurrencyKey lets one blocking child at a time change the parent's
// directory; it is not the directory's own key, which the child's calls take.
// A background call ends once its child starts, so a key would not hold it.
func (t *agentTool) ConcurrencyKey(call tools.Call) string {
	if !t.app.agentCallWritesSharedDir(call) {
		return ""
	}
	return "subagents:" + filepath.Clean(call.WorkingDir)
}

// agentCallWritesSharedDir reports whether the call's child runs inside the
// call and may change the parent's working directory.
func (c *Core) agentCallWritesSharedDir(call tools.Call) bool {
	input := parseAgentInput(call.Args)
	return !input.Background && c.agentCallWrites(call)
}

// agentCallWrites reports whether the call's child may change the parent's
// working directory: it is neither read-only (by readonly or by its type)
// nor in a worktree of its own.
func (c *Core) agentCallWrites(call tools.Call) bool {
	input := parseAgentInput(call.Args)
	if input.Readonly || normalizeSubagentIsolation(input.Isolation) != SubagentIsolationShared {
		return false
	}
	profile, ok := c.agentProfile(call.WorkingDir, true, input.AgentType)
	return !ok || !profile.Readonly
}

func parseAgentInput(args json.RawMessage) agentToolInput {
	var input agentToolInput
	_ = json.Unmarshal(args, &input)
	return input
}

func (t *agentTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.app == nil {
		return tools.Result{}, fmt.Errorf("%w: agent core unavailable", ErrExecutionUnavailable)
	}
	var input agentToolInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{}, tools.InvalidArgs(agentToolName, err)
	}
	result, err := t.app.RunAgent(ctx, AgentInput{
		ParentSessionID:  call.SessionID,
		ParentRunID:      call.RunID,
		ParentToolCallID: call.ToolCallID,
		Description:      input.Description,
		Prompt:           input.Prompt,
		Background:       input.Background,
		Isolation:        input.Isolation,
		Readonly:         input.Readonly,
		Runtime:          input.Runtime,
		Model:            input.Model,
		AgentType:        input.AgentType,
		OutputSchema:     input.OutputSchema,
	})
	if err != nil {
		return tools.Result{}, err
	}
	if result.Task.Background {
		return tools.Result{Content: backgroundAgentContent(result), Metadata: result.Task, Status: tools.ResultStatusNeutral}, nil
	}
	return tools.Result{Content: agentResultContent(result), Metadata: result.Task, Status: agentResultStatus(result)}, nil
}

var agentToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "description": {"type": "string", "description": "A short label for the task, 3-5 words, shown to the user."},
    "prompt": {"type": "string", "description": "The complete brief: goal, relevant paths and findings, constraints, what done means and what to report back. The child sees nothing else."},
    "agent_type": {"type": "string", "enum": ["general"], "description": "The kind of child; omit for general."},
    "background": {"type": "boolean", "description": "Return a task id at once and keep working; the child's report arrives as a message when it finishes."},
    "isolation": {"type": "string", "enum": ["shared", "worktree"], "description": "For a writing child: shared (default) works in your directory, one blocking writer at a time; worktree gives it its own git worktree and branch from your current files, so several run at once."},
    "readonly": {"type": "boolean", "description": "Give the child read-only tools, for research and review; read-only children run in parallel."},
    "runtime": {"type": "string", "enum": ["matrixclaw"], "description": "Child runtime; omit for the native matrixclaw runtime."},
    "model": {"type": "string", "description": "Model for the child; omit for its type's model or else your own."},
    "output_schema": {"type": "object", "description": "A JSON schema the child's final answer must match; the report is then that JSON value."}
  },
  "required": ["description", "prompt"],
  "additionalProperties": false
}`)

// agentSteerTool sends a running background child a message it reads
// before its next step.
type agentSteerTool struct {
	app *Core
}

type agentSteerInput struct {
	TaskID  string `json:"task_id"`
	Message string `json:"message"`
}

func (t *agentSteerTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          agentSteerToolName,
		Description: "Send a running background child new instructions or information; it reads the message before its next step. Use it to correct course, narrow the task or pass on what you learned.",
		Effect:      tools.EffectMutation,
		Namespace:   "core.subagents",
		Category:    tools.CategoryAutomation,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {"type": "string", "description": "The background child's task id."},
    "message": {"type": "string", "description": "What the child should know or do now."}
  },
  "required": ["task_id", "message"],
  "additionalProperties": false
}`),
	}
}

func (t *agentSteerTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input agentSteerInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{}, tools.InvalidArgs(agentSteerToolName, err)
	}
	task, err := t.app.SteerSubagent(ctx, call.SessionID, input.TaskID, input.Message)
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Content: fmt.Sprintf("Sent to %s (task %s); it reads the message before its next step.", subagentTaskAgentName(task), task.ID), Metadata: task}, nil
}

// worktreeMergeTool applies a worktree child's branch to the parent's tree.
type worktreeMergeTool struct {
	app *Core
}

type worktreeMergeInput struct {
	TaskID string   `json:"task_id"`
	Paths  []string `json:"paths,omitempty"`
}

func (t *worktreeMergeTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          worktreeMergeToolName,
		Description: "Apply what an ended worktree child changed on its branch to your working tree, with a three-way merge; your index is left alone. paths (relative to your working directory) merges only those files or directories. Conflicting files get conflict markers to resolve. A full merge without conflicts removes the child's worktree and branch. Run the checks afterwards.",
		Effect:      tools.EffectMutation,
		Asks:        true,
		Namespace:   "core.subagents",
		Category:    tools.CategoryAutomation,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {"type": "string", "description": "The worktree child's task id."},
    "paths": {"type": "array", "items": {"type": "string"}, "description": "Merge only these paths; omit for everything."}
  },
  "required": ["task_id"],
  "additionalProperties": false
}`),
	}
}

// ConcurrencyKey makes a merge take turns with the edits in the parent's
// directory.
func (t *worktreeMergeTool) ConcurrencyKey(call tools.Call) string {
	return tools.WorkingDirKey(call.WorkingDir)
}

func (t *worktreeMergeTool) Preview(ctx context.Context, call tools.Call) (tools.ApprovalRequest, error) {
	var input worktreeMergeInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.ApprovalRequest{}, tools.InvalidArgs(worktreeMergeToolName, err)
	}
	task, err := t.app.keptWorktreeTask(ctx, call.SessionID, input.TaskID)
	if err != nil {
		return tools.ApprovalRequest{}, err
	}
	stat, files, err := worktreeChanges(ctx, call.WorkingDir, task.WorktreeBase, subagentBranch(task.ID), input.Paths)
	if err != nil {
		return tools.ApprovalRequest{}, err
	}
	return tools.ApprovalRequest{
		Description: fmt.Sprintf("Merge branch %s of %s into %s: %s (%s)", subagentBranch(task.ID), subagentTaskAgentName(task), call.WorkingDir, stat, fileList(files, 10)),
		Path:        call.WorkingDir,
	}, nil
}

func (t *worktreeMergeTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input worktreeMergeInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{}, tools.InvalidArgs(worktreeMergeToolName, err)
	}
	merge, err := t.app.mergeSubagentWorktree(ctx, call.SessionID, call.WorkingDir, input.TaskID, input.Paths)
	if err != nil {
		return tools.Result{}, err
	}
	branch := subagentBranch(strings.TrimSpace(input.TaskID))
	if len(merge.Conflicts) > 0 {
		return tools.Result{
			Content: fmt.Sprintf("Merged %s with conflicts (%s). Resolve the conflict markers in: %s. The worktree and branch stay until worktree_discard.", branch, merge.Stat, strings.Join(merge.Conflicts, ", ")),
			Status:  tools.ResultStatusError,
		}, nil
	}
	content := fmt.Sprintf("Merged %s into %s: %s.", branch, call.WorkingDir, merge.Stat)
	if merge.Removed {
		content += " Its worktree and branch were removed."
	}
	return tools.Result{Content: content}, nil
}

// worktreeDiscardTool drops a worktree child's worktree and branch.
type worktreeDiscardTool struct {
	app *Core
}

func (t *worktreeDiscardTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          worktreeDiscardToolName,
		Description: "Delete an ended worktree child's worktree and branch, dropping the changes you did not merge.",
		Effect:      tools.EffectMutation,
		Namespace:   "core.subagents",
		Category:    tools.CategoryAutomation,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {"type": "string", "description": "The worktree child's task id."}
  },
  "required": ["task_id"],
  "additionalProperties": false
}`),
	}
}

func (t *worktreeDiscardTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{}, tools.InvalidArgs(worktreeDiscardToolName, err)
	}
	task, err := t.app.discardSubagentWorktree(ctx, call.SessionID, input.TaskID)
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Content: fmt.Sprintf("Removed the worktree and branch %s of task %s.", subagentBranch(task.ID), task.ID)}, nil
}
