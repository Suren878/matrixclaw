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

const (
	subagentApprovalBridgeSource    = "subagent_approval_bridge"
	subagentParentResumeWaitTimeout = 24 * time.Hour
)

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
	if err := c.createSubagentTaskRecord(ctx, task); err != nil {
		return DelegateTaskResult{}, err
	}

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
		task, err = c.markSubagentTaskWaitingApproval(ctx, task)
		if err != nil {
			return DelegateTaskResult{}, err
		}
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
	status := SubagentTaskStatusCompleted
	errText := ""
	if failed {
		status = SubagentTaskStatusFailed
		errText = summary
	}
	task, err := c.finishSubagentTaskRecord(ctx, task, status, summary, errText, false)
	if err != nil {
		return DelegateTaskResult{}, err
	}
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
	done, err := c.resumeParentForSubagentStatus(ctx, task)
	if err != nil || done {
		return err
	}
	safego.Go("core.waitSubagentTerminalAndResumeParent", func() {
		c.waitForSubagentTerminalAndResumeParent(task)
	})
	return nil
}

func (c *Core) waitForSubagentTerminalAndResumeParent(task SubagentTask) {
	ctx, cancel := context.WithTimeout(context.Background(), subagentParentResumeWaitTimeout)
	defer cancel()

	events := c.SubscribeEvents(ctx, task.ChildSessionID)
	for {
		done, err := c.resumeParentForSubagentStatus(ctx, task)
		if err == nil && done {
			return
		}
		select {
		case <-ctx.Done():
			return
		case event := <-events:
			if event.RunID != "" && event.RunID != task.ChildRunID {
				continue
			}
			if event.Type != EventRunUpdated && event.Type != EventApprovalRequest {
				continue
			}
		}
	}
}

func (c *Core) resumeParentForSubagentStatus(ctx context.Context, task SubagentTask) (bool, error) {
	status, err := c.subagentTaskRunStatus(ctx, task)
	if err != nil {
		return false, err
	}
	if subagentRunStatusTerminal(status) {
		return true, c.startRun(ctx, task.ParentRunID)
	}
	if status == RunStatusWaitingApproval {
		return true, c.mirrorPendingSubagentApproval(ctx, task)
	}
	return false, nil
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
	task, err = c.markSubagentTaskWaitingApproval(ctx, task)
	if err != nil {
		return err
	}
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
