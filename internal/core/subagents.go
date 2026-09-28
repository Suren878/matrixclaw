package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const subagentApprovalBridgeSource = "subagent_approval_bridge"

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

// DefaultBackgroundAgents is how many background subagents one session may run
// at once unless the daemon configures another limit.
const DefaultBackgroundAgents = 4

// WithBackgroundAgents bounds the background subagents one session runs at
// once; 0 or less keeps the default.
func (c *Core) WithBackgroundAgents(n int) *Core {
	if n <= 0 {
		n = DefaultBackgroundAgents
	}
	c.backgroundAgents = n
	return c
}

// AgentInput is one agent tool call: a child agent on a bounded task.
type AgentInput struct {
	ParentSessionID  string
	ParentRunID      string
	ParentToolCallID string
	Description      string
	Prompt           string
	Background       bool
	Isolation        string
	Readonly         bool
	Runtime          string
	Model            string
}

// AgentResult is how a blocking child ended, or the task a background child
// runs as; Approval asks the parent for a child's pending approval.
type AgentResult struct {
	Task     SubagentTask
	Summary  string
	IsError  bool
	Approval *tools.ApprovalRequest
	Replayed bool
}

// RunAgent runs a child agent for a parent's agent call. A blocking child runs
// inside the call and its result is the call's; a background child becomes a
// task whose result reaches the parent as an event. A repeated call resumes
// the child it started.
func (c *Core) RunAgent(ctx context.Context, input AgentInput) (AgentResult, error) {
	prompt := normalizeText(input.Prompt)
	if prompt == "" {
		return AgentResult{}, fmt.Errorf("%w: prompt is required", ErrInvalidInput)
	}
	parent, err := c.store.GetSession(ctx, normalizeText(input.ParentSessionID))
	if err != nil {
		return AgentResult{}, err
	}
	parent = c.decorateSessionLLM(parent)
	if CoreSessionIsExternalAgent(parent) {
		return AgentResult{}, fmt.Errorf("%w: the agent tool is available for Matrixclaw sessions only", ErrInvalidInput)
	}
	if isSubagentSession(parent) {
		return AgentResult{}, fmt.Errorf("%w: child subagents cannot start agents", ErrInvalidInput)
	}
	parentRunID := normalizeText(input.ParentRunID)
	parentToolCallID := normalizeText(input.ParentToolCallID)
	if parentRunID != "" && parentToolCallID != "" {
		existing, err := c.store.GetSubagentTaskByParentToolCall(ctx, parent.ID, parentRunID, parentToolCallID)
		switch {
		case err == nil && existing.Mode == SubagentTaskModeAsync:
			return AgentResult{Task: existing, Replayed: true}, nil
		case err == nil:
			return c.resumeSubagentTask(ctx, existing)
		case !errors.Is(err, ErrNotFound):
			return AgentResult{}, err
		}
	}
	task, run, err := c.startSubagent(ctx, parent, input, prompt, parentRunID, parentToolCallID)
	if err != nil {
		return AgentResult{}, err
	}
	if input.Background {
		if err := c.startRun(ctx, run.ID); err != nil {
			summary := "Subagent failed to start: " + err.Error()
			_, _ = c.finishSubagentTaskRecord(ctx, task, TaskStatusFailed, summary, summary, false)
			return AgentResult{}, err
		}
		return AgentResult{Task: task}, nil
	}
	execErr := c.ExecuteRun(ctx, run.ID)
	return c.finishOrBridgeSubagentTask(ctx, task, execErr)
}

// startSubagent creates the child session, its run and the task that links
// them to the parent's call; a worktree child gets its own git worktree.
func (c *Core) startSubagent(ctx context.Context, parent Session, input AgentInput, prompt string, parentRunID string, parentToolCallID string) (SubagentTask, Run, error) {
	runtime := normalizeSubagentRuntime(input.Runtime)
	isolation := normalizeSubagentIsolation(input.Isolation)
	if input.Readonly {
		isolation = SubagentIsolationShared
	}
	taskID := c.newID("task")
	workingDir := parent.WorkingDir
	if isolation == SubagentIsolationWorktree {
		dir, err := prepareSubagentWorktree(ctx, workingDir, taskID)
		if err != nil {
			return SubagentTask{}, Run{}, err
		}
		workingDir = dir
	}
	// Children started at once by one reply get different names and count
	// toward the background limit one after another.
	gate := c.sessionGate(parent.ID)
	gate.Lock()
	defer gate.Unlock()
	if input.Background {
		active, err := c.store.ListActiveSubagentTasksByParent(ctx, parent.ID)
		if err != nil {
			return SubagentTask{}, Run{}, err
		}
		if len(active) >= c.backgroundAgents {
			return SubagentTask{}, Run{}, fmt.Errorf("%w: at most %d background subagents run at once in a session; await one of them first", ErrInvalidInput, c.backgroundAgents)
		}
	}
	agentName, err := c.assignSubagentAgentName(ctx, parent.ID)
	if err != nil {
		return SubagentTask{}, Run{}, err
	}
	child, err := c.createSubagentSession(ctx, parent, runtime, input.Model, workingDir, agentName, input.Readonly)
	if err != nil {
		return SubagentTask{}, Run{}, err
	}
	run, err := c.createSubagentRun(ctx, child, subagentUserPrompt(prompt, workingDir, isolation, input.Readonly))
	if err != nil {
		return SubagentTask{}, Run{}, err
	}
	mode := SubagentTaskModeBlocking
	if input.Background {
		mode = SubagentTaskModeAsync
	}
	now := c.now().UTC()
	task := SubagentTask{
		ID:               taskID,
		AgentName:        agentName,
		DisplayName:      subagentDisplayName(input.Description, prompt),
		Mode:             mode,
		Isolation:        isolation,
		Readonly:         input.Readonly,
		ParentSessionID:  parent.ID,
		ParentRunID:      parentRunID,
		ParentToolCallID: parentToolCallID,
		ChildSessionID:   child.ID,
		ChildRunID:       run.ID,
		Runtime:          subagentTaskRuntimeLabel(runtime, child),
		Model:            normalizeText(input.Model),
		Goal:             prompt,
		Status:           TaskStatusRunning,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := c.createSubagentTaskRecord(ctx, task); err != nil {
		return SubagentTask{}, Run{}, err
	}
	return task, run, nil
}

func (c *Core) resumeSubagentTask(ctx context.Context, task SubagentTask) (AgentResult, error) {
	if task.Status == TaskStatusCompleted {
		return AgentResult{Task: task, Summary: task.Summary}, nil
	}
	if task.Status == TaskStatusFailed && task.FinishedAt != nil {
		summary := strings.TrimSpace(task.Summary)
		if summary == "" {
			summary = strings.TrimSpace(task.Error)
		}
		return AgentResult{Task: task, Summary: summary, IsError: true}, nil
	}
	return c.finishOrBridgeSubagentTask(ctx, task, nil)
}

func (c *Core) finishOrBridgeSubagentTask(ctx context.Context, task SubagentTask, execErr error) (AgentResult, error) {
	run, err := c.store.GetRun(ctx, task.ChildRunID)
	if err != nil {
		if execErr != nil {
			return c.finishSubagentTask(ctx, task, "Subagent failed: "+execErr.Error(), true)
		}
		return AgentResult{}, err
	}
	if execErr == nil && run.Status == RunStatusWaitingApproval {
		approval, err := c.pendingApprovalForRun(ctx, task.ChildSessionID, task.ChildRunID)
		if err == nil {
			return c.bridgeSubagentApproval(ctx, task, approval)
		}
		if !errors.Is(err, ErrNotFound) {
			return AgentResult{}, err
		}
	}
	if execErr == nil && !subagentRunStatusTerminal(run.Status) {
		if err := c.waitForSubagentStep(ctx, task); err != nil {
			return AgentResult{}, err
		}
		return c.finishOrBridgeSubagentTask(ctx, task, nil)
	}
	summary, failed := c.subagentRunSummary(ctx, task.ChildSessionID, task.ChildRunID, execErr)
	return c.finishSubagentTask(ctx, task, summary, failed)
}

// bridgeSubagentApproval asks the parent for a blocking child's approval; a
// read-only child's is refused and the call goes on waiting for the child.
func (c *Core) bridgeSubagentApproval(ctx context.Context, task SubagentTask, approval Approval) (AgentResult, error) {
	if task.Readonly {
		if err := c.refuseReadonlySubagentApproval(ctx, approval); err != nil {
			return AgentResult{}, err
		}
		return c.finishOrBridgeSubagentTask(ctx, task, nil)
	}
	task, err := c.markSubagentTaskWaitingApproval(ctx, task)
	if err != nil {
		return AgentResult{}, err
	}
	request, err := c.subagentApprovalRequest(ctx, task, approval)
	if err != nil {
		return AgentResult{}, err
	}
	return AgentResult{
		Task:     task,
		Summary:  "Subagent is waiting for permission.",
		Approval: request,
	}, nil
}

// waitForSubagentStep waits while the child is still running, including right
// after its approval was decided, until it finishes or asks for approval again.
func (c *Core) waitForSubagentStep(ctx context.Context, task SubagentTask) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := c.SubscribeEvents(ctx, task.ChildSessionID)
	for {
		run, err := c.store.GetRun(ctx, task.ChildRunID)
		if err != nil {
			return err
		}
		if subagentRunStatusTerminal(run.Status) {
			return nil
		}
		if run.Status == RunStatusWaitingApproval {
			_, err := c.pendingApprovalForRun(ctx, task.ChildSessionID, task.ChildRunID)
			if !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-events:
		}
	}
}

func (c *Core) finishSubagentTask(ctx context.Context, task SubagentTask, summary string, failed bool) (AgentResult, error) {
	status := TaskStatusCompleted
	errText := ""
	if failed {
		status = TaskStatusFailed
		errText = summary
	}
	task, err := c.finishSubagentTaskRecord(ctx, task, status, summary, errText, false)
	if err != nil {
		return AgentResult{}, err
	}
	return AgentResult{Task: task, Summary: summary, IsError: failed}, nil
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
		ToolID:      agentToolName,
		ToolCallID:  task.ParentToolCallID,
		Action:      childApproval.Action,
		Path:        childApproval.Path,
		Description: description,
		Params:      params,
		Suggestion:  childApproval.Suggestion,
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

// resumeParentForSubagentStatus starts the parent of a finished child and asks
// it for a child's pending approval; a child still running resumes its parent
// when its run ends (syncBlockingSubagentTaskAfterRun).
func (c *Core) resumeParentForSubagentStatus(ctx context.Context, task SubagentTask) error {
	status, err := c.subagentTaskRunStatus(ctx, task)
	if err != nil {
		return err
	}
	if subagentRunStatusTerminal(status) {
		return c.startRun(ctx, task.ParentRunID)
	}
	if status == RunStatusWaitingApproval {
		_, err = c.mirrorPendingSubagentApproval(ctx, task)
	}
	return err
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

// mirrorPendingSubagentApproval asks the parent for the child's pending approval;
// only a blocking child's parent run waits for it. It reports false when the
// child has none (its approval was just decided and the child has not resumed
// yet) or is read-only, whose approval is refused.
func (c *Core) mirrorPendingSubagentApproval(ctx context.Context, task SubagentTask) (bool, error) {
	childApproval, err := c.pendingApprovalForRun(ctx, task.ChildSessionID, task.ChildRunID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if task.Readonly {
		return false, c.refuseReadonlySubagentApproval(ctx, childApproval)
	}
	existing, err := c.subagentBridgeApprovalForChild(ctx, task, childApproval.ID)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(existing.ID) != "" {
		return true, nil
	}
	task, err = c.markSubagentTaskWaitingApproval(ctx, task)
	if err != nil {
		return false, err
	}
	request, err := c.subagentApprovalRequest(ctx, task, childApproval)
	if err != nil {
		return false, err
	}
	prepared := preparedToolCall{
		SessionID:  task.ParentSessionID,
		RunID:      task.ParentRunID,
		ToolName:   agentToolName,
		ToolCallID: task.ParentToolCallID,
	}
	if _, err := c.requestApproval(ctx, prepared, *request); err != nil {
		return true, err
	}
	if task.Mode == SubagentTaskModeAsync || strings.TrimSpace(task.ParentRunID) == "" {
		return true, nil
	}
	run, err := c.store.GetRun(ctx, task.ParentRunID)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	if subagentRunStatusTerminal(run.Status) {
		return true, nil
	}
	return true, c.setRunStatus(ctx, &run, RunStatusWaitingApproval, "")
}

// refuseReadonlySubagentApproval denies a read-only child's approval without
// asking anyone; the child reads the denial and goes on.
func (c *Core) refuseReadonlySubagentApproval(ctx context.Context, approval Approval) error {
	reason := "read-only subagent cannot run " + firstNonEmpty(strings.TrimSpace(approval.ToolName), "this tool")
	_, err := c.ResolveApproval(ctx, approval.ID, ApprovalResolveRequest{Reason: reason})
	return err
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
