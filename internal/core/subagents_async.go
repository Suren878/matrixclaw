package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

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

type SpawnSubagentInput struct {
	ParentSessionID  string
	ParentRunID      string
	ParentToolCallID string
	Name             string
	Goal             string
	Context          string
	Runtime          string
	Model            string
	WorkingDir       string
	Isolation        string
}

type SpawnSubagentResult struct {
	Task     SubagentTask `json:"task"`
	Replayed bool         `json:"replayed,omitempty"`
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

func (c *Core) SpawnSubagent(ctx context.Context, input SpawnSubagentInput) (SpawnSubagentResult, error) {
	parentSessionID := normalizeText(input.ParentSessionID)
	if parentSessionID == "" {
		return SpawnSubagentResult{}, fmt.Errorf("%w: parent session id is required", ErrInvalidInput)
	}
	goal := normalizeText(input.Goal)
	if goal == "" {
		return SpawnSubagentResult{}, fmt.Errorf("%w: goal is required", ErrInvalidInput)
	}
	parent, err := c.store.GetSession(ctx, parentSessionID)
	if err != nil {
		return SpawnSubagentResult{}, err
	}
	parent = c.decorateSessionLLM(parent)
	if CoreSessionIsExternalAgent(parent) {
		return SpawnSubagentResult{}, fmt.Errorf("%w: spawn_subagent is available for Matrixclaw sessions only", ErrInvalidInput)
	}
	if isSubagentSession(parent) {
		return SpawnSubagentResult{}, fmt.Errorf("%w: child subagents cannot spawn subagents", ErrInvalidInput)
	}

	parentRunID := normalizeText(input.ParentRunID)
	parentToolCallID := normalizeText(input.ParentToolCallID)
	if parentRunID != "" && parentToolCallID != "" {
		existing, err := c.store.GetSubagentTaskByParentToolCall(ctx, parent.ID, parentRunID, parentToolCallID)
		if err == nil {
			return SpawnSubagentResult{Task: existing, Replayed: true}, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return SpawnSubagentResult{}, err
		}
	}

	active, err := c.store.ListActiveSubagentTasksByParent(ctx, parent.ID)
	if err != nil {
		return SpawnSubagentResult{}, err
	}
	if len(active) >= maxActiveAsyncSubagents {
		return SpawnSubagentResult{}, fmt.Errorf("%w: active async subagent limit is %d for this session", ErrInvalidInput, maxActiveAsyncSubagents)
	}

	runtime := normalizeSubagentRuntime(input.Runtime)
	isolation := normalizeSubagentIsolation(input.Isolation)
	workingDir := normalizeWorkingDir(input.WorkingDir)
	if workingDir == "" {
		workingDir = parent.WorkingDir
	}
	displayName := normalizeSubagentDisplayName(input.Name, goal)
	taskID := c.newID("subagent")
	if isolation == SubagentIsolationWorktree {
		workingDir, err = prepareSubagentWorktree(ctx, workingDir, taskID)
		if err != nil {
			return SpawnSubagentResult{}, err
		}
	}
	agentName, err := c.assignSubagentAgentName(ctx, parent.ID)
	if err != nil {
		return SpawnSubagentResult{}, err
	}

	child, err := c.createSubagentSession(ctx, parent, runtime, input.Model, workingDir, agentName)
	if err != nil {
		return SpawnSubagentResult{}, err
	}
	run, err := c.createSubagentRun(ctx, child, asyncSubagentUserPrompt(goal, input.Context, workingDir, isolation))
	if err != nil {
		return SpawnSubagentResult{}, err
	}

	now := c.now().UTC()
	task := SubagentTask{
		ID:               taskID,
		AgentName:        agentName,
		DisplayName:      displayName,
		Mode:             SubagentTaskModeAsync,
		Isolation:        isolation,
		ParentSessionID:  parent.ID,
		ParentRunID:      parentRunID,
		ParentToolCallID: parentToolCallID,
		ChildSessionID:   child.ID,
		ChildRunID:       run.ID,
		Runtime:          subagentTaskRuntimeLabel(runtime, child),
		Goal:             goal,
		Status:           SubagentTaskStatusRunning,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := c.store.CreateSubagentTask(ctx, task); err != nil {
		return SpawnSubagentResult{}, err
	}
	c.createSubagentWorkJob(ctx, task)
	c.publishSubagentTaskUpdated(task)

	if err := c.startRun(ctx, run.ID); err != nil {
		task.Status = SubagentTaskStatusFailed
		task.Error = "Subagent failed to start: " + err.Error()
		task.Summary = task.Error
		task.UpdatedAt = c.now().UTC()
		finishedAt := task.UpdatedAt
		task.FinishedAt = &finishedAt
		_ = c.store.UpdateSubagentTask(ctx, task)
		c.updateSubagentWorkJob(ctx, task)
		c.publishSubagentTaskUpdated(task)
		return SpawnSubagentResult{}, err
	}

	return SpawnSubagentResult{Task: task}, nil
}

func (c *Core) ListSubagents(ctx context.Context, parentSessionID string, includeRecent bool, limit int) ([]SubagentTask, error) {
	parentSessionID = normalizeText(parentSessionID)
	if parentSessionID == "" {
		return nil, fmt.Errorf("%w: parent session id is required", ErrInvalidInput)
	}
	if limit <= 0 {
		limit = 8
	}
	filter := SubagentTaskFilter{
		ParentSessionID: parentSessionID,
		Mode:            SubagentTaskModeAsync,
		Limit:           limit,
	}
	if !includeRecent {
		filter.Statuses = activeSubagentTaskStatuses()
	}
	return c.store.ListSubagentTasks(ctx, filter)
}

func (c *Core) ReadSubagentResult(ctx context.Context, parentSessionID string, taskID string, name string) (SubagentTask, string, error) {
	parentSessionID = normalizeText(parentSessionID)
	taskID = normalizeText(taskID)
	name = normalizeText(name)
	if parentSessionID == "" {
		return SubagentTask{}, "", fmt.Errorf("%w: parent session id is required", ErrInvalidInput)
	}
	var task SubagentTask
	var err error
	if taskID != "" {
		task, err = c.store.GetSubagentTask(ctx, taskID)
	} else if name != "" {
		var tasks []SubagentTask
		tasks, err = c.store.ListSubagentTasks(ctx, SubagentTaskFilter{
			ParentSessionID: parentSessionID,
			Mode:            SubagentTaskModeAsync,
			Limit:           50,
		})
		if err == nil {
			err = ErrNotFound
			for _, candidate := range tasks {
				if strings.EqualFold(strings.TrimSpace(candidate.DisplayName), name) {
					task = candidate
					err = nil
					break
				}
			}
		}
	} else {
		return SubagentTask{}, "", fmt.Errorf("%w: task_id or name is required", ErrInvalidInput)
	}
	if err != nil {
		return SubagentTask{}, "", err
	}
	if task.ParentSessionID != parentSessionID {
		return SubagentTask{}, "", fmt.Errorf("%w: subagent task belongs to another parent session", ErrInvalidInput)
	}
	return task, c.subagentTaskDetail(task), nil
}

func (c *Core) recordSubagentResultMessage(ctx context.Context, metadata any, resultMessageID string) error {
	resultMessageID = normalizeText(resultMessageID)
	if resultMessageID == "" {
		return nil
	}
	task, ok := metadata.(SubagentTask)
	if !ok {
		if taskPtr, ptrOK := metadata.(*SubagentTask); ptrOK && taskPtr != nil {
			task = *taskPtr
			ok = true
		}
	}
	if !ok || normalizeText(task.ID) == "" || task.Mode != SubagentTaskModeAsync {
		return nil
	}
	current, err := c.store.GetSubagentTask(ctx, task.ID)
	if err != nil {
		return err
	}
	if current.ResultMessageID == resultMessageID {
		return nil
	}
	current.ResultMessageID = resultMessageID
	current.UpdatedAt = c.now().UTC()
	if err := c.store.UpdateSubagentTask(ctx, current); err != nil {
		return err
	}
	c.updateSubagentWorkJob(ctx, current)
	c.publishSubagentTaskUpdated(current)
	return nil
}

func (c *Core) touchAsyncSubagentTaskActivity(ctx context.Context, childRunID string, at time.Time) error {
	childRunID = normalizeText(childRunID)
	if childRunID == "" || c == nil || c.store == nil {
		return nil
	}
	task, err := c.store.GetSubagentTaskByChildRun(ctx, childRunID)
	if err != nil {
		return nil
	}
	if task.Mode != SubagentTaskModeAsync || subagentTaskTerminalStatus(task.Status) {
		return nil
	}
	if at.IsZero() {
		at = c.now().UTC()
	}
	if !task.UpdatedAt.IsZero() && at.Sub(task.UpdatedAt) < time.Second {
		return nil
	}
	task.UpdatedAt = at
	if err := c.store.UpdateSubagentTask(ctx, task); err != nil {
		return err
	}
	c.updateSubagentWorkJob(ctx, task)
	c.publishSubagentTaskUpdated(task)
	return nil
}

func (c *Core) afterRunExecution(ctx context.Context, runID string) error {
	runID = normalizeText(runID)
	if runID == "" || c == nil || c.store == nil {
		return nil
	}
	run, err := c.store.GetRun(ctx, runID)
	if err != nil {
		return nil
	}
	if task, err := c.store.GetSubagentTaskByChildRun(ctx, runID); err == nil {
		switch task.Mode {
		case SubagentTaskModeAsync:
			if syncErr := c.syncAsyncSubagentTaskAfterRun(ctx, task, run); syncErr != nil {
				return syncErr
			}
		case SubagentTaskModeBlocking:
			if syncErr := c.syncBlockingSubagentTaskAfterRun(ctx, task, run); syncErr != nil {
				return syncErr
			}
		}
	}
	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		return nil
	}
	if subagentRunStatusTerminal(run.Status) {
		if err := c.queuePendingSteersForRun(ctx, session.ID, run.ID); err != nil {
			return err
		}
		startedInput, err := c.startNextPendingSessionInput(ctx, session.ID)
		if err != nil || startedInput {
			return err
		}
	}
	if !isSubagentSession(session) && subagentRunStatusTerminal(run.Status) {
		return c.deliverPendingSubagentCompletionsForParent(ctx, session.ID)
	}
	return nil
}

func (c *Core) syncAsyncSubagentTaskAfterRun(ctx context.Context, task SubagentTask, run Run) error {
	switch run.Status {
	case RunStatusWaitingApproval:
		return c.mirrorPendingSubagentApproval(ctx, task)
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
	default:
		return nil
	}
	summary, failed := c.subagentRunSummary(ctx, task.ChildSessionID, task.ChildRunID, nil)
	task.Summary = summary
	task.Error = ""
	task.UpdatedAt = c.now().UTC()
	finishedAt := task.UpdatedAt
	task.FinishedAt = &finishedAt
	if run.Status == RunStatusCanceled {
		task.Status = SubagentTaskStatusCanceled
		task.Error = summary
	} else if failed {
		task.Status = SubagentTaskStatusFailed
		task.Error = summary
	} else {
		task.Status = SubagentTaskStatusCompleted
	}
	if task.CompletionQueuedAt == nil {
		queuedAt := task.UpdatedAt
		task.CompletionQueuedAt = &queuedAt
	}
	if err := c.store.UpdateSubagentTask(ctx, task); err != nil {
		return err
	}
	c.updateSubagentWorkJob(ctx, task)
	c.publishSubagentTaskUpdated(task)
	if err := c.updateSubagentResultMessage(ctx, task); err != nil {
		return err
	}
	return c.deliverPendingSubagentCompletionsForParent(ctx, task.ParentSessionID)
}

func (c *Core) syncBlockingSubagentTaskAfterRun(ctx context.Context, task SubagentTask, run Run) error {
	switch run.Status {
	case RunStatusWaitingApproval:
		parentRunID := normalizeText(task.ParentRunID)
		if parentRunID == "" {
			return nil
		}
		parentRun, err := c.store.GetRun(ctx, parentRunID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if parentRun.Status == RunStatusRunning {
			return nil
		}
		return c.mirrorPendingSubagentApproval(ctx, task)
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
	default:
		return nil
	}
	parentRunID := normalizeText(task.ParentRunID)
	if parentRunID == "" {
		return nil
	}
	parentRun, err := c.store.GetRun(ctx, parentRunID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if subagentRunStatusTerminal(parentRun.Status) {
		return nil
	}
	return c.startRun(ctx, parentRunID)
}

func (c *Core) deliverPendingSubagentCompletionsForParent(ctx context.Context, parentSessionID string) error {
	parentSessionID = normalizeText(parentSessionID)
	if parentSessionID == "" {
		return nil
	}
	if ready, err := c.parentReadyForSubagentAutoResume(ctx, parentSessionID); err != nil || !ready {
		return err
	}
	tasks, err := c.store.ListSubagentTasks(ctx, SubagentTaskFilter{
		ParentSessionID: parentSessionID,
		Mode:            SubagentTaskModeAsync,
		Statuses: []SubagentTaskStatus{
			SubagentTaskStatusCompleted,
			SubagentTaskStatusFailed,
			SubagentTaskStatusCanceled,
		},
		Limit: 50,
	})
	if err != nil {
		return err
	}
	pending := make([]SubagentTask, 0, len(tasks))
	for _, task := range tasks {
		if task.CompletionQueuedAt != nil && task.CompletionDeliveredAt == nil {
			pending = append(pending, task)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})
	triggerID := subagentCompletionTriggerID(parentSessionID, pending)
	result, err := c.AcceptTriggeredRun(ctx, HandleTriggeredRunInput{
		TriggerID: triggerID,
		SessionID: parentSessionID,
		Text:      subagentCompletionPrompt(pending),
	})
	if err != nil {
		return err
	}
	now := c.now().UTC()
	for _, task := range pending {
		task.CompletionDeliveredAt = &now
		task.CompletionAutoResumeRunID = result.Run.ID
		task.UpdatedAt = now
		if err := c.store.UpdateSubagentTask(ctx, task); err != nil {
			return err
		}
		c.updateSubagentWorkJob(ctx, task)
		c.publishSubagentTaskUpdated(task)
	}
	return nil
}

func (c *Core) parentReadyForSubagentAutoResume(ctx context.Context, parentSessionID string) (bool, error) {
	pendingInputs, err := c.store.ListPendingSessionInputs(ctx, parentSessionID)
	if err != nil {
		return false, err
	}
	if len(pendingInputs) > 0 {
		return false, nil
	}
	messages, err := c.store.ListMessages(ctx, parentSessionID, 0)
	if err != nil {
		return false, err
	}
	if len(messages) == 0 {
		return true, nil
	}
	for i := len(messages) - 1; i >= 0; i-- {
		runID := normalizeText(messages[i].RunID)
		if runID == "" {
			continue
		}
		run, err := c.store.GetRun(ctx, runID)
		if errors.Is(err, ErrNotFound) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return subagentRunStatusTerminal(run.Status), nil
	}
	return true, nil
}

func (c *Core) updateSubagentResultMessage(ctx context.Context, task SubagentTask) error {
	resultMessageID := normalizeText(task.ResultMessageID)
	if resultMessageID == "" {
		return nil
	}
	messages, err := c.store.ListMessages(ctx, task.ParentSessionID, 0)
	if err != nil {
		return err
	}
	for _, message := range messages {
		if message.ID != resultMessageID {
			continue
		}
		content := subagentFinishedResultContent(task)
		metadataRaw, err := marshalJSONRaw(task)
		if err != nil {
			return err
		}
		message.Content = normalizeToolContent(content)
		message.UpdatedAt = c.now().UTC()
		for i := range message.Parts {
			if message.Parts[i].ToolResult == nil {
				continue
			}
			message.Parts[i].ToolResult.Content = content
			message.Parts[i].ToolResult.Metadata = metadataRaw
			message.Parts[i].ToolResult.Status = string(subagentTaskToolResultStatus(task))
			message.Parts[i].ToolResult.IsError = subagentTaskFailed(task)
		}
		if err := c.store.UpdateMessage(ctx, message); err != nil {
			return err
		}
		c.publishEvent(Event{
			Type:      EventMessageUpdated,
			SessionID: message.SessionID,
			RunID:     message.RunID,
			Payload:   message,
		})
		c.publishToolUpdate(task.ParentSessionID, task.ParentRunID, ToolUpdate{
			ToolCallID:      task.ParentToolCallID,
			ToolName:        spawnSubagentToolName,
			State:           subagentTaskToolLifecycleState(task),
			ResultStatus:    string(subagentTaskToolResultStatus(task)),
			RunID:           task.ParentRunID,
			SessionID:       task.ParentSessionID,
			ResultMessageID: resultMessageID,
			Error:           task.Error,
		})
		return nil
	}
	return nil
}

func (c *Core) RecoverSubagentTasks(ctx context.Context) error {
	active, err := c.store.ListSubagentTasks(ctx, SubagentTaskFilter{
		Statuses: activeSubagentTaskStatuses(),
		Limit:    200,
	})
	if err != nil {
		return err
	}
	for _, task := range active {
		run, err := c.store.GetRun(ctx, task.ChildRunID)
		if err != nil {
			continue
		}
		switch task.Mode {
		case SubagentTaskModeAsync:
			if subagentRunStatusTerminal(run.Status) || run.Status == RunStatusWaitingApproval {
				if err := c.syncAsyncSubagentTaskAfterRun(ctx, task, run); err != nil {
					return err
				}
				continue
			}
			if err := c.startRun(ctx, task.ChildRunID); err != nil {
				return err
			}
		case SubagentTaskModeBlocking:
			if subagentRunStatusTerminal(run.Status) || run.Status == RunStatusWaitingApproval {
				if err := c.syncBlockingSubagentTaskAfterRun(ctx, task, run); err != nil {
					return err
				}
				continue
			}
			if run.Status == RunStatusAccepted {
				if err := c.startRun(ctx, task.ChildRunID); err != nil {
					return err
				}
			}
		}
	}
	pending, err := c.store.ListPendingSubagentCompletionTasks(ctx, 200)
	if err != nil {
		return err
	}
	seenParents := map[string]struct{}{}
	for _, task := range pending {
		if _, ok := seenParents[task.ParentSessionID]; ok {
			continue
		}
		seenParents[task.ParentSessionID] = struct{}{}
		if err := c.deliverPendingSubagentCompletionsForParent(ctx, task.ParentSessionID); err != nil {
			return err
		}
	}
	return nil
}

func activeSubagentTaskStatuses() []SubagentTaskStatus {
	return []SubagentTaskStatus{
		SubagentTaskStatusPending,
		SubagentTaskStatusRunning,
		SubagentTaskStatusWaitingApproval,
	}
}

func normalizeSubagentIsolation(value string) SubagentIsolation {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(SubagentIsolationShared):
		return SubagentIsolationShared
	case string(SubagentIsolationWorktree):
		return SubagentIsolationWorktree
	default:
		return SubagentIsolationShared
	}
}

func normalizeSubagentDisplayName(name string, goal string) string {
	if name = strings.Join(strings.Fields(name), " "); name != "" {
		return truncateForTitle(name, 48)
	}
	return generatedSubagentDisplayName(goal)
}

func prepareSubagentWorktree(ctx context.Context, workingDir string, taskID string) (string, error) {
	workingDir = normalizeWorkingDir(workingDir)
	taskID = stableIDPart(taskID)
	if workingDir == "" {
		return "", fmt.Errorf("%w: working_dir is required for worktree isolation", ErrInvalidInput)
	}
	if taskID == "" {
		return "", fmt.Errorf("%w: task id is required for worktree isolation", ErrInvalidInput)
	}
	repoRoot, err := gitCommandOutput(ctx, workingDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%w: worktree isolation requires a git working tree: %v", ErrInvalidInput, err)
	}
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return "", fmt.Errorf("%w: git root is empty for worktree isolation", ErrInvalidInput)
	}
	target := filepath.Join(os.TempDir(), "matrixclaw-subagents", stableIDPart(filepath.Base(repoRoot)), taskID)
	if target == "" || target == string(filepath.Separator) {
		return "", fmt.Errorf("%w: invalid worktree target", ErrInvalidInput)
	}
	if _, err := os.Stat(target); err == nil {
		if _, gitErr := gitCommandOutput(ctx, target, "rev-parse", "--show-toplevel"); gitErr == nil {
			return target, nil
		}
		if removeErr := os.RemoveAll(target); removeErr != nil {
			return "", removeErr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if _, err := gitCommandOutput(ctx, repoRoot, "worktree", "add", "--detach", target, "HEAD"); err != nil {
		return "", fmt.Errorf("%w: create subagent worktree: %v", ErrExecutionUnavailable, err)
	}
	return target, nil
}

func gitCommandOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message != "" {
			return "", fmt.Errorf("%w: %s", err, message)
		}
		return "", err
	}
	return string(out), nil
}

func generatedSubagentDisplayName(goal string) string {
	goal = strings.Join(strings.Fields(goal), " ")
	if goal == "" {
		return "Subagent"
	}
	return truncateForTitle(goal, 48)
}

func subagentParentToolName(task SubagentTask) string {
	if task.Mode == SubagentTaskModeAsync {
		return spawnSubagentToolName
	}
	return delegateTaskToolName
}

func asyncSubagentUserPrompt(goal string, contextText string, workingDir string, isolation SubagentIsolation) string {
	prompt := subagentUserPrompt(goal, contextText, workingDir)
	switch isolation {
	case SubagentIsolationWorktree:
		prompt += "\nIsolation: worktree requested. Keep edits scoped to the isolated worktree assigned by the parent runtime. Do not edit files outside the working directory."
	default:
		prompt += "\nIsolation: shared working copy. Prefer read-only investigation unless the parent explicitly requested edits."
	}
	return prompt
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

func subagentFinishedResultContent(task SubagentTask) string {
	verb := "finished"
	if subagentTaskFailed(task) {
		verb = "failed"
	}
	lines := []string{
		fmt.Sprintf("Subagent %s %s", subagentTaskAgentName(task), verb),
		"Task ID: " + task.ID,
		"Status: " + string(task.Status),
	}
	if taskLabel := strings.TrimSpace(task.DisplayName); taskLabel != "" {
		lines = append(lines, "Task: "+taskLabel)
	}
	if summary := strings.TrimSpace(task.Summary); summary != "" {
		lines = append(lines, "", summary)
	}
	if task.Error != "" && !strings.Contains(strings.TrimSpace(task.Summary), strings.TrimSpace(task.Error)) {
		lines = append(lines, "", "Error: "+task.Error)
	}
	return strings.Join(lines, "\n")
}

func subagentCompletionPrompt(tasks []SubagentTask) string {
	if len(tasks) == 1 {
		task := tasks[0]
		return strings.Join([]string{
			fmt.Sprintf("Subagent %s completed.", subagentTaskAgentName(task)),
			"Task: " + strings.TrimSpace(task.DisplayName),
			"Goal: " + task.Goal,
			"Status: " + string(task.Status),
			"Summary: " + firstNonEmpty(task.Summary, task.Error),
			"",
			"Briefly synthesize this result for the user and decide whether any follow-up action is needed.",
		}, "\n")
	}
	lines := []string{"Multiple subagents completed:"}
	for _, task := range tasks {
		lines = append(lines, fmt.Sprintf("- %s [%s]: %s", subagentTaskAgentName(task), task.Status, firstNonEmpty(task.Summary, task.Error)))
	}
	lines = append(lines, "", "Briefly synthesize these results for the user and decide whether any follow-up action is needed.")
	return strings.Join(lines, "\n")
}

func subagentCompletionTriggerID(parentSessionID string, tasks []SubagentTask) string {
	ids := make([]string, 0, len(tasks)+1)
	ids = append(ids, parentSessionID)
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return "subagent_completion_" + stableIDPart(strings.Join(ids, "_"))
}

func subagentTaskTerminalStatus(status SubagentTaskStatus) bool {
	return status == SubagentTaskStatusCompleted || status == SubagentTaskStatusFailed || status == SubagentTaskStatusCanceled
}

func subagentTaskFailed(task SubagentTask) bool {
	return task.Status == SubagentTaskStatusFailed || task.Status == SubagentTaskStatusCanceled || strings.TrimSpace(task.Error) != ""
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

func (c *Core) publishSubagentTaskUpdated(task SubagentTask) {
	c.publishEvent(Event{
		Type:      EventSubagentUpdated,
		SessionID: task.ParentSessionID,
		RunID:     task.ParentRunID,
		Payload:   task,
	})
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
