package core

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
)

// DefaultBackgroundAgents is how many background subagents one session may run
// at once unless the daemon configures another limit.
const DefaultBackgroundAgents = 4

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
	// AgentType names the child's profile; "" is general.
	AgentType string
	// OutputSchema is the JSON schema the child's final answer must match.
	OutputSchema json.RawMessage
}

// AgentResult is how a blocking child ended, or the task a background child
// runs as.
type AgentResult struct {
	Task     Task
	Summary  string
	IsError  bool
	Replayed bool
}

// RunAgent runs a child agent for a parent's agent call. The call waits for a
// blocking child to end and returns its result; a background child is a task
// whose result reaches the parent as an event. A repeated call (after a
// restart) waits for the child it started again, or reports it.
func (c *Core) RunAgent(ctx context.Context, input AgentInput) (AgentResult, error) {
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return AgentResult{}, fmt.Errorf("%w: prompt is required", ErrInvalidInput)
	}
	input.Model = strings.TrimSpace(input.Model)
	parent, err := c.store.GetSession(ctx, input.ParentSessionID)
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
	profile, err := c.subagentProfile(ctx, parent, input)
	if err != nil {
		return AgentResult{}, err
	}
	parentRunID := input.ParentRunID
	parentToolCallID := input.ParentToolCallID
	if parentRunID != "" && parentToolCallID != "" {
		existing, err := c.subagentTaskOfCall(ctx, parent.ID, parentRunID, parentToolCallID)
		switch {
		case err == nil && existing.Background:
			return AgentResult{Task: existing, Replayed: true}, nil
		case err == nil:
			return c.awaitSubagent(ctx, existing)
		case !errors.Is(err, ErrNotFound):
			return AgentResult{}, err
		}
	}
	task, run, err := c.startSubagent(ctx, parent, input, profile, prompt, parentRunID, parentToolCallID)
	if err != nil {
		return AgentResult{}, err
	}
	if err := c.startRun(ctx, run.ID); err != nil {
		summary := "Subagent failed to start: " + err.Error()
		task, _, endErr := c.endSubagentTask(ctx, task, TaskEnd{Status: TaskStatusFailed, Summary: summary, Error: summary, Delivered: true})
		if input.Background || endErr != nil {
			return AgentResult{}, errors.Join(err, endErr)
		}
		return AgentResult{Task: task, Summary: summary, IsError: true}, nil
	}
	if input.Background {
		return AgentResult{Task: task}, nil
	}
	return c.awaitSubagent(ctx, task)
}

// awaitSubagent waits until a blocking child's run ended and returns how its
// task ended; it stops when ctx does, as when its parent is canceled.
func (c *Core) awaitSubagent(ctx context.Context, task Task) (AgentResult, error) {
	if err := c.waitRunEnd(ctx, task.ChildRunID); err != nil {
		return AgentResult{}, err
	}
	task, err := c.store.GetTask(ctx, task.ID)
	if err != nil {
		return AgentResult{}, err
	}
	if !task.Status.Terminal() {
		return AgentResult{}, fmt.Errorf("subagent task %s did not end with its run", task.ID)
	}
	return AgentResult{Task: task, Summary: task.Summary, IsError: task.Status != TaskStatusCompleted}, nil
}

// startSubagent creates the child session, its run and the task that links
// them to the parent's call; a worktree child gets its own git worktree. The
// parent's session gate is held only to reserve the child and record its task.
func (c *Core) startSubagent(ctx context.Context, parent Session, input AgentInput, profile AgentProfile, prompt string, parentRunID string, parentToolCallID string) (Task, Run, error) {
	agentName, err := c.reserveSubagent(ctx, parent.ID, input.Background)
	if err != nil {
		return Task{}, Run{}, err
	}
	task, run, err := c.createSubagent(ctx, parent, input, profile, prompt, parentRunID, parentToolCallID, agentName)
	if err != nil {
		_ = c.endSubagentStart(ctx, parent.ID, agentName, nil)
		return Task{}, Run{}, err
	}
	if err := c.endSubagentStart(ctx, parent.ID, agentName, &task); err != nil {
		c.discardSubagent(ctx, task)
		return Task{}, Run{}, err
	}
	return task, run, nil
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
		if limit := c.Tuning().BackgroundAgents; count >= limit {
			return "", fmt.Errorf("%w: at most %d background subagents run at once in a session; await one of them first", ErrInvalidInput, limit)
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

// subagentProfile is the profile the call's child runs with: its agent type
// among those the parent may pick, read-only when the call asks for it. A
// coding parent may pick the user's agent types; other runs (such as a
// guest's reply-once answer) only the builtins.
func (c *Core) subagentProfile(ctx context.Context, parent Session, input AgentInput) (AgentProfile, error) {
	userAgents := c.codingRun(ctx, nativeTurn{RunID: input.ParentRunID, WorkingDir: parent.WorkingDir, ToolUse: true})
	profile, ok := c.agentProfile(parent.WorkingDir, userAgents, input.AgentType)
	if !ok {
		return AgentProfile{}, fmt.Errorf("%w: unknown agent_type %q; available: %s", ErrInvalidInput, input.AgentType, strings.Join(agentTypeNames(c.agentProfiles(parent.WorkingDir, userAgents)), ", "))
	}
	if normalizeSubagentRuntime(input.Runtime) != SubagentRuntimeMatrixClaw && (profile.Name != defaultAgentType || len(input.OutputSchema) > 0) {
		return AgentProfile{}, fmt.Errorf("%w: agent_type and output_schema work with the matrixclaw runtime only", ErrInvalidInput)
	}
	if _, err := compileOutputSchema(input.OutputSchema); err != nil {
		return AgentProfile{}, err
	}
	profile.Readonly = profile.Readonly || input.Readonly
	return profile, nil
}

func (c *Core) createSubagent(ctx context.Context, parent Session, input AgentInput, profile AgentProfile, prompt string, parentRunID string, parentToolCallID string, agentName string) (Task, Run, error) {
	runtime := normalizeSubagentRuntime(input.Runtime)
	isolation := normalizeSubagentIsolation(input.Isolation)
	if profile.Readonly {
		isolation = SubagentIsolationShared
	}
	model := cmp.Or(input.Model, profile.Model)
	taskID := c.newID("task")
	var tree subagentWorktree
	workingDir := parent.WorkingDir
	if isolation == SubagentIsolationWorktree {
		var err error
		if tree, err = c.prepareSubagentWorktree(ctx, parent.WorkingDir, taskID); err != nil {
			return Task{}, Run{}, err
		}
		workingDir = tree.dir
	}
	now := c.now().UTC()
	task := Task{
		ID:               taskID,
		SessionID:        parent.ID,
		RunID:            parentRunID,
		ParentToolCallID: parentToolCallID,
		Kind:             TaskKindSubagent,
		Status:           TaskStatusRunning,
		Command:          prompt,
		Description:      subagentDisplayName(input.Description, prompt),
		WorkingDir:       tree.root,
		Background:       input.Background,
		AgentName:        agentName,
		Model:            model,
		Isolation:        isolation,
		Readonly:         profile.Readonly,
		Agent:            profile,
		OutputSchema:     input.OutputSchema,
		WorktreeBase:     tree.base,
		StartedAt:        now,
		UpdatedAt:        now,
	}
	child, err := c.createSubagentSession(ctx, parent, runtime, model, workingDir, agentName, profile.Readonly)
	if err != nil {
		c.removeSubagentWorktree(task)
		return Task{}, Run{}, err
	}
	task.ChildSessionID, task.Runtime = child.ID, subagentTaskRuntimeLabel(runtime, child)
	if err := c.limitSubagentSteps(ctx, child, profile); err != nil {
		c.discardSubagent(ctx, task)
		return Task{}, Run{}, err
	}
	run, err := c.createSubagentRun(ctx, child, subagentUserPrompt(prompt, workingDir, task))
	if err != nil {
		c.discardSubagent(ctx, task)
		return Task{}, Run{}, err
	}
	task.ChildRunID = run.ID
	return task, run, nil
}

// limitSubagentSteps gives a child whose profile names a step budget that
// budget, never more than the daemon's budget for children.
func (c *Core) limitSubagentSteps(ctx context.Context, child Session, profile AgentProfile) error {
	steps := profile.BudgetSteps
	if steps <= 0 {
		return nil
	}
	if limit := c.Tuning().Budgets.Subagent.Steps; limit > 0 {
		steps = min(steps, limit)
	}
	return c.store.SaveSessionBudget(ctx, child.ID, SessionBudget{Steps: &steps}, c.now().UTC())
}
