package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	Task     Task
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
		existing, err := c.subagentTaskOfCall(ctx, parent.ID, parentRunID, parentToolCallID)
		switch {
		case err == nil && existing.Background:
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
// them to the parent's call; a worktree child gets its own git worktree. The
// parent's session gate is held only to reserve the child and record its task.
func (c *Core) startSubagent(ctx context.Context, parent Session, input AgentInput, prompt string, parentRunID string, parentToolCallID string) (Task, Run, error) {
	agentName, err := c.reserveSubagent(ctx, parent.ID, input.Background)
	if err != nil {
		return Task{}, Run{}, err
	}
	task, run, err := c.createSubagent(ctx, parent, input, prompt, parentRunID, parentToolCallID, agentName)
	if err != nil {
		_ = c.endSubagentStart(ctx, parent.ID, agentName, nil)
		return Task{}, Run{}, err
	}
	return task, run, c.endSubagentStart(ctx, parent.ID, agentName, &task)
}

// reserveSubagent names a child its parent starts and takes a background slot
// for a background one, so children started at once by one reply get
// different names and keep the limit; the reservation lasts until endSubagentStart.
func (c *Core) reserveSubagent(ctx context.Context, parentID string, background bool) (string, error) {
	gate := c.sessionGate(parentID)
	gate.Lock()
	defer gate.Unlock()
	c.mu.RLock()
	starting := maps.Clone(c.startingAgents[parentID])
	c.mu.RUnlock()
	if background {
		active, err := c.store.ListTasks(ctx, TaskFilter{SessionID: parentID, Kind: TaskKindSubagent, Background: true, Statuses: activeTaskStatuses()})
		if err != nil {
			return "", err
		}
		count := len(active)
		for _, startingBackground := range starting {
			if startingBackground {
				count++
			}
		}
		if count >= c.backgroundAgents {
			return "", fmt.Errorf("%w: at most %d background subagents run at once in a session; await one of them first", ErrInvalidInput, c.backgroundAgents)
		}
	}
	name, err := c.assignSubagentAgentName(ctx, parentID, starting)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	if c.startingAgents == nil {
		c.startingAgents = map[string]map[string]bool{}
	}
	if c.startingAgents[parentID] == nil {
		c.startingAgents[parentID] = map[string]bool{}
	}
	c.startingAgents[parentID][name] = background
	c.mu.Unlock()
	return name, nil
}

// endSubagentStart records the started child's task, if any, and drops its
// reservation in one step under the parent's gate.
func (c *Core) endSubagentStart(ctx context.Context, parentID string, name string, task *Task) error {
	gate := c.sessionGate(parentID)
	gate.Lock()
	defer gate.Unlock()
	var err error
	if task != nil {
		err = c.createSubagentTaskRecord(ctx, *task)
	}
	c.mu.Lock()
	delete(c.startingAgents[parentID], name)
	if len(c.startingAgents[parentID]) == 0 {
		delete(c.startingAgents, parentID)
	}
	c.mu.Unlock()
	return err
}

func (c *Core) createSubagent(ctx context.Context, parent Session, input AgentInput, prompt string, parentRunID string, parentToolCallID string, agentName string) (Task, Run, error) {
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
			return Task{}, Run{}, err
		}
		workingDir = dir
	}
	child, err := c.createSubagentSession(ctx, parent, runtime, input.Model, workingDir, agentName, input.Readonly)
	if err != nil {
		return Task{}, Run{}, err
	}
	run, err := c.createSubagentRun(ctx, child, subagentUserPrompt(prompt, workingDir, isolation, input.Readonly))
	if err != nil {
		return Task{}, Run{}, err
	}
	now := c.now().UTC()
	return Task{
		ID:               taskID,
		SessionID:        parent.ID,
		RunID:            parentRunID,
		ParentToolCallID: parentToolCallID,
		Kind:             TaskKindSubagent,
		Status:           TaskStatusRunning,
		Command:          prompt,
		Description:      subagentDisplayName(input.Description, prompt),
		Background:       input.Background,
		AgentName:        agentName,
		Runtime:          subagentTaskRuntimeLabel(runtime, child),
		Model:            normalizeText(input.Model),
		Isolation:        isolation,
		Readonly:         input.Readonly,
		ChildSessionID:   child.ID,
		ChildRunID:       run.ID,
		StartedAt:        now,
		UpdatedAt:        now,
	}, run, nil
}

func (c *Core) resumeSubagentTask(ctx context.Context, task Task) (AgentResult, error) {
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

func (c *Core) finishOrBridgeSubagentTask(ctx context.Context, task Task, execErr error) (AgentResult, error) {
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
	if execErr == nil && !run.Status.Terminal() {
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
func (c *Core) bridgeSubagentApproval(ctx context.Context, task Task, approval Approval) (AgentResult, error) {
	if task.Readonly {
		if err := c.refuseReadonlySubagentApproval(ctx, approval); err != nil {
			return AgentResult{}, err
		}
		return c.finishOrBridgeSubagentTask(ctx, task, nil)
	}
	task, err := c.setSubagentTaskStatus(ctx, task, TaskStatusWaitingApproval)
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
func (c *Core) waitForSubagentStep(ctx context.Context, task Task) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := c.SubscribeEvents(ctx, task.ChildSessionID)
	for {
		run, err := c.store.GetRun(ctx, task.ChildRunID)
		if err != nil {
			return err
		}
		if run.Status.Terminal() {
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

func (c *Core) finishSubagentTask(ctx context.Context, task Task, summary string, failed bool) (AgentResult, error) {
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

func (c *Core) subagentApprovalRequest(ctx context.Context, task Task, childApproval Approval) (*tools.ApprovalRequest, error) {
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
		SubagentTitle:       firstNonEmpty(strings.TrimSpace(task.AgentName), firstNonEmpty(strings.TrimSpace(task.Description), strings.TrimSpace(child.Title))),
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

// resumeParentForSubagentStatus starts the parent of a finished child once none
// of its approvals is pending, and asks it for a child's pending approval; a
// child still running resumes its parent when its run ends
// (syncBlockingSubagentTaskAfterRun).
func (c *Core) resumeParentForSubagentStatus(ctx context.Context, task Task) error {
	status, err := c.subagentTaskRunStatus(ctx, task)
	if err != nil {
		return err
	}
	if status.Terminal() {
		return c.resumeDecidedRun(ctx, task.SessionID, task.RunID)
	}
	if status == RunStatusWaitingApproval {
		_, err = c.mirrorPendingSubagentApproval(ctx, task)
	}
	return err
}

func (c *Core) subagentTaskTerminal(ctx context.Context, task Task) (bool, error) {
	status, err := c.subagentTaskRunStatus(ctx, task)
	if err != nil {
		return false, err
	}
	return status.Terminal(), nil
}

func (c *Core) subagentTaskRunStatus(ctx context.Context, task Task) (RunStatus, error) {
	run, err := c.store.GetRun(ctx, task.ChildRunID)
	if err != nil {
		return "", err
	}
	return run.Status, nil
}

// mirrorPendingSubagentApproval asks the parent for the child's pending approval;
// only a blocking child's parent run waits for it. It reports false when the
// child has none (its approval was just decided and the child has not resumed
// yet) or is read-only, whose approval is refused.
func (c *Core) mirrorPendingSubagentApproval(ctx context.Context, task Task) (bool, error) {
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
	task, err = c.setSubagentTaskStatus(ctx, task, TaskStatusWaitingApproval)
	if err != nil {
		return false, err
	}
	request, err := c.subagentApprovalRequest(ctx, task, childApproval)
	if err != nil {
		return false, err
	}
	prepared := preparedToolCall{
		SessionID:  task.SessionID,
		RunID:      task.RunID,
		ToolName:   agentToolName,
		ToolCallID: task.ParentToolCallID,
	}
	approval, err := c.requestApproval(ctx, prepared, *request)
	if err != nil {
		return true, err
	}
	if task.Background {
		return true, c.deliverBackgroundApproval(ctx, approval)
	}
	if strings.TrimSpace(task.RunID) == "" {
		return true, nil
	}
	run, err := c.store.GetRun(ctx, task.RunID)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	if run.Status.Terminal() {
		return true, nil
	}
	if run.Status == RunStatusWaitingApproval {
		return true, nil
	}
	return true, c.transition(ctx, &run, runChange{To: RunStatusWaitingApproval})
}

// deliverBackgroundApproval sends a background subagent's approval to the chat
// the parent session's own runs answer in, as no run of it may be delivering.
func (c *Core) deliverBackgroundApproval(ctx context.Context, approval Approval) error {
	runs, err := c.store.ListSessionRuns(ctx, approval.SessionID, wakeChainWindow)
	if err != nil {
		return err
	}
	target, err := c.latestWakeTarget(ctx, runs)
	if err != nil || target.client == "" {
		return err
	}
	payload, err := json.Marshal(ApprovalDeliveryPayload{ApprovalID: approval.ID})
	if err != nil {
		return err
	}
	_, err = c.CreateClientDelivery(ctx, ClientDelivery{
		Type:        ClientDeliveryTypeApproval,
		Client:      target.client,
		ExternalKey: target.externalKey,
		SessionID:   approval.SessionID,
		RunID:       approval.RunID,
		Summary:     approval.Description,
		Address:     target.address,
		Payload:     payload,
	})
	return err
}

// refuseReadonlySubagentApproval denies a read-only child's approval without
// asking anyone; the child reads the denial and goes on.
func (c *Core) refuseReadonlySubagentApproval(ctx context.Context, approval Approval) error {
	reason := "read-only subagent cannot run " + firstNonEmpty(strings.TrimSpace(approval.ToolName), "this tool")
	_, err := c.ResolveApproval(ctx, approval.ID, ApprovalResolveRequest{Reason: reason})
	return err
}

func (c *Core) subagentBridgeApprovalForChild(ctx context.Context, task Task, childApprovalID string) (Approval, error) {
	approvals, err := c.store.ListApprovals(ctx, task.SessionID, "")
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
