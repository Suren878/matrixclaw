package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const maxActiveAsyncSubagents = 4

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
	if err := c.createSubagentTaskRecord(ctx, task); err != nil {
		return SpawnSubagentResult{}, err
	}

	if err := c.startRun(ctx, run.ID); err != nil {
		summary := "Subagent failed to start: " + err.Error()
		_, _ = c.finishSubagentTaskRecord(ctx, task, SubagentTaskStatusFailed, summary, summary, false)
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
