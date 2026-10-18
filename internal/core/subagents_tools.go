package core

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const agentToolName = "agent"

type agentToolInput struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Background  bool   `json:"background,omitempty"`
	Isolation   string `json:"isolation,omitempty"`
	Readonly    bool   `json:"readonly,omitempty"`
	Runtime     string `json:"runtime,omitempty"`
	Model       string `json:"model,omitempty"`
}

type agentTool struct {
	app *Core
}

// AgentToolExecutors returns the agent tool.
func AgentToolExecutors(app *Core) []tools.Executor {
	return []tools.Executor{&agentTool{app: app}}
}

func (t *agentTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              agentToolName,
		Description:     "Start a child agent on a self-contained task. The child sees none of this conversation, so prompt must hold the goal, the relevant paths and findings, the constraints, what done means and what to report back. By default the call waits and returns the child's final report. readonly: true gives the child read-only tools for research and review; read-only children in one reply run in parallel. A writing child works in your directory (isolation shared; blocking writers take turns) or in its own git worktree started from the last commit (isolation worktree; several run at once and you bring their changes over). background: true returns a task id at once; the report arrives later as a message, and await waits for it.",
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
	input := parseAgentInput(call.Args)
	if !agentCallWritesSharedDir(input) {
		return ""
	}
	return "subagents:" + filepath.Clean(call.WorkingDir)
}

// agentCallWritesSharedDir reports whether the call's child runs inside the
// call and may change the parent's working directory.
func agentCallWritesSharedDir(input agentToolInput) bool {
	return !input.Background && !input.Readonly && normalizeSubagentIsolation(input.Isolation) == SubagentIsolationShared
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
    "background": {"type": "boolean", "description": "Return a task id at once and keep working; the child's report arrives as a message when it finishes."},
    "isolation": {"type": "string", "enum": ["shared", "worktree"], "description": "For a writing child: shared (default) works in your directory, one blocking writer at a time; worktree gives it its own git worktree from the last commit, so several run at once."},
    "readonly": {"type": "boolean", "description": "Give the child read-only tools, for research and review; read-only children run in parallel."},
    "runtime": {"type": "string", "enum": ["matrixclaw"], "description": "Child runtime; omit for the native matrixclaw runtime."},
    "model": {"type": "string", "description": "Model for the child; omit to use your own model."}
  },
  "required": ["description", "prompt"],
  "additionalProperties": false
}`)
