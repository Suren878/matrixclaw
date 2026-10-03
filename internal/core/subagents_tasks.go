package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// findTask is the one task the filter selects; ErrNotFound when none does.
func (c *Core) findTask(ctx context.Context, filter TaskFilter) (Task, error) {
	filter.Limit = 1
	tasks, err := c.store.ListTasks(ctx, filter)
	if err != nil {
		return Task{}, err
	}
	if len(tasks) == 0 {
		return Task{}, ErrNotFound
	}
	return tasks[0], nil
}

// subagentTaskOfCall is the subagent task a parent's call started.
func (c *Core) subagentTaskOfCall(ctx context.Context, sessionID string, runID string, callID string) (Task, error) {
	return c.findTask(ctx, TaskFilter{SessionID: sessionID, RunID: runID, ParentToolCallID: callID, Kind: TaskKindSubagent})
}

// subagentTaskOf is the task a subagent's session works for; ok is false for
// a session that is no subagent's.
func (c *Core) subagentTaskOf(ctx context.Context, sessionID string) (Task, bool, error) {
	task, err := c.findTask(ctx, TaskFilter{ChildSessionID: sessionID, Kind: TaskKindSubagent})
	if errors.Is(err, ErrNotFound) {
		return Task{}, false, nil
	}
	return task, err == nil, err
}

// readonlySubagent reports whether the session is a read-only subagent's.
func (c *Core) readonlySubagent(ctx context.Context, sessionID string) (bool, error) {
	task, ok, err := c.subagentTaskOf(ctx, sessionID)
	return ok && task.Readonly, err
}

func activeTaskStatuses() []TaskStatus {
	return []TaskStatus{TaskStatusPending, TaskStatusRunning, TaskStatusWaitingApproval}
}

func (c *Core) createSubagentTaskRecord(ctx context.Context, task Task) error {
	if err := c.store.CreateTask(ctx, task); err != nil {
		return err
	}
	c.publishTaskUpdated(task)
	return nil
}

// setSubagentTaskStatus moves a subagent task that has not ended to status.
func (c *Core) setSubagentTaskStatus(ctx context.Context, task Task, status TaskStatus) (Task, error) {
	if err := c.store.SetTaskStatus(ctx, task.ID, status, c.now().UTC()); err != nil {
		return Task{}, err
	}
	task, err := c.store.GetTask(ctx, task.ID)
	if err != nil {
		return Task{}, err
	}
	c.publishTaskUpdated(task)
	return task, nil
}

// endSubagentTask ends the task unless it ended already, and returns it as
// stored and whether this call ended it.
func (c *Core) endSubagentTask(ctx context.Context, task Task, end TaskEnd) (Task, bool, error) {
	if end.At.IsZero() {
		end.At = c.now().UTC()
	}
	ended, err := c.store.FinishTask(ctx, task.ID, end)
	if err != nil {
		return Task{}, false, err
	}
	stored, err := c.store.GetTask(ctx, task.ID)
	if err != nil {
		return Task{}, false, err
	}
	if ended {
		c.publishTaskUpdated(stored)
	}
	return stored, ended, nil
}

// syncSubagentTask brings the task a subagent's run works for up to date with
// the run: running or waiting for approval while it is, ended with the run's
// reply once it ends. A background task's end is an event for its parent; a
// blocking one's parent reads it as its call's result.
func (c *Core) syncSubagentTask(ctx context.Context, run Run) (Task, error) {
	task, err := c.findTask(ctx, TaskFilter{ChildRunID: run.ID, Kind: TaskKindSubagent})
	if errors.Is(err, ErrNotFound) {
		return Task{}, nil
	}
	if err != nil || task.Status.Terminal() {
		return task, err
	}
	switch {
	case run.Status == RunStatusRunning && task.Status != TaskStatusRunning:
		return c.setSubagentTaskStatus(ctx, task, TaskStatusRunning)
	case run.Status == RunStatusWaitingApproval && task.Status != TaskStatusWaitingApproval:
		return c.setSubagentTaskStatus(ctx, task, TaskStatusWaitingApproval)
	case !run.Status.Terminal():
		return task, nil
	}
	summary, failed := c.subagentRunSummary(ctx, run)
	end := TaskEnd{Status: TaskStatusCompleted, Summary: summary, Delivered: !task.Background}
	switch {
	case run.Status == RunStatusCanceled:
		end.Status, end.Error = TaskStatusCanceled, summary
	case failed:
		end.Status, end.Error = TaskStatusFailed, summary
	}
	task, ended, err := c.endSubagentTask(ctx, task, end)
	if err == nil && ended {
		c.taskFinished(ctx, task)
	}
	return task, err
}

// RecoverSubagentTasks ends the tasks whose child run ended before the daemon
// stopped while the task was not told.
func (c *Core) RecoverSubagentTasks(ctx context.Context) error {
	active, err := c.store.ListTasks(ctx, TaskFilter{Kind: TaskKindSubagent, Statuses: activeTaskStatuses()})
	if err != nil {
		return err
	}
	var errs []error
	for _, task := range active {
		run, err := c.store.GetRun(ctx, task.ChildRunID)
		if err == nil && run.Status.Terminal() {
			_, err = c.syncSubagentTask(ctx, run)
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			errs = append(errs, fmt.Errorf("subagent task %s: %w", task.ID, err))
		}
	}
	return errors.Join(errs...)
}

// waitRunEnd returns once the run has ended, or with ctx's error.
func (c *Core) waitRunEnd(ctx context.Context, runID string) error {
	ended := make(chan struct{})
	c.mu.Lock()
	c.runEnds[runID] = append(c.runEnds[runID], ended)
	c.mu.Unlock()
	defer c.forgetRunEnd(runID, ended)
	run, err := c.store.GetRun(ctx, runID)
	if err != nil || run.Status.Terminal() {
		return err
	}
	select {
	case <-ended:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// notifyRunEnd wakes the callers waiting for the run to end.
func (c *Core) notifyRunEnd(runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ended := range c.runEnds[runID] {
		close(ended)
	}
	delete(c.runEnds, runID)
}

func (c *Core) forgetRunEnd(runID string, ended chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	waiters := slices.DeleteFunc(c.runEnds[runID], func(ch chan struct{}) bool { return ch == ended })
	if len(waiters) == 0 {
		delete(c.runEnds, runID)
		return
	}
	c.runEnds[runID] = waiters
}

// approvalAudience is the session and run whose clients hear of the approval:
// a subagent's approval goes to its parent's.
func (c *Core) approvalAudience(ctx context.Context, approval Approval) (string, string) {
	if approval.TaskID != "" {
		if task, err := c.store.GetTask(ctx, approval.TaskID); err == nil {
			return task.SessionID, task.RunID
		}
	}
	return approval.SessionID, approval.RunID
}

// deliverSubagentApproval sends a subagent's approval to the chat its parent
// session answers in, as the parent's own run delivery shows only its own.
func (c *Core) deliverSubagentApproval(ctx context.Context, approval Approval, task Task) error {
	runs, err := c.store.ListSessionRuns(ctx, task.SessionID, wakeChainWindow)
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
		SessionID:   task.SessionID,
		RunID:       task.RunID,
		TaskID:      task.ID,
		Summary:     subagentApprovalSummary(approval),
		Address:     target.address,
		Payload:     payload,
	})
	return err
}

func subagentApprovalSummary(approval Approval) string {
	summary := fmt.Sprintf("Subagent %s asks to run %s", firstNonEmpty(approval.AgentName, "subagent"), firstNonEmpty(approval.ToolName, "a tool"))
	if detail := strings.TrimSpace(approval.Description); detail != "" {
		summary += ": " + detail
	}
	return summary
}
