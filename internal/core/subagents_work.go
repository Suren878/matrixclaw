package core

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Suren878/matrixclaw/internal/work"
)

type subagentWorkResult struct {
	TaskID                    string `json:"task_id"`
	AgentName                 string `json:"agent_name,omitempty"`
	DisplayName               string `json:"display_name,omitempty"`
	Mode                      string `json:"mode,omitempty"`
	Isolation                 string `json:"isolation,omitempty"`
	ParentSessionID           string `json:"parent_session_id,omitempty"`
	ParentRunID               string `json:"parent_run_id,omitempty"`
	ParentToolCallID          string `json:"parent_tool_call_id,omitempty"`
	ChildSessionID            string `json:"child_session_id,omitempty"`
	ChildRunID                string `json:"child_run_id,omitempty"`
	Runtime                   string `json:"runtime,omitempty"`
	Status                    string `json:"status,omitempty"`
	Error                     string `json:"error,omitempty"`
	ResultMessageID           string `json:"result_message_id,omitempty"`
	CompletionQueuedAt        string `json:"completion_queued_at,omitempty"`
	CompletionDeliveredAt     string `json:"completion_delivered_at,omitempty"`
	CompletionAutoResumeRunID string `json:"completion_auto_resume_run_id,omitempty"`
}

func (c *Core) saveSubagentWorkJob(ctx context.Context, task SubagentTask) {
	if c == nil || c.workStore == nil {
		return
	}
	job := subagentWorkJob(task)
	existing, err := c.workStore.GetJob(ctx, job.ID)
	if errors.Is(err, work.ErrNotFound) {
		_ = c.workStore.CreateJob(ctx, job)
		return
	}
	if err != nil {
		return
	}
	job.CreatedAt = existing.CreatedAt
	_ = c.workStore.UpdateJob(ctx, job)
}

func subagentWorkJob(task SubagentTask) work.Job {
	resultRaw, _ := json.Marshal(subagentWorkResult{
		TaskID:                    task.ID,
		AgentName:                 task.AgentName,
		DisplayName:               task.DisplayName,
		Mode:                      string(task.Mode),
		Isolation:                 string(task.Isolation),
		ParentSessionID:           task.ParentSessionID,
		ParentRunID:               task.ParentRunID,
		ParentToolCallID:          task.ParentToolCallID,
		ChildSessionID:            task.ChildSessionID,
		ChildRunID:                task.ChildRunID,
		Runtime:                   task.Runtime,
		Status:                    string(task.Status),
		Error:                     task.Error,
		ResultMessageID:           task.ResultMessageID,
		CompletionQueuedAt:        optionalTime(task.CompletionQueuedAt),
		CompletionDeliveredAt:     optionalTime(task.CompletionDeliveredAt),
		CompletionAutoResumeRunID: task.CompletionAutoResumeRunID,
	})
	createdAt := task.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := task.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	return work.Job{
		ID:         task.ID,
		Kind:       work.KindSubagent,
		Status:     workStatusForSubagent(task.Status),
		Task:       task.Goal,
		Summary:    task.Summary,
		ResultJSON: string(resultRaw),
		WorkerRef:  task.ChildRunID,
		Error:      task.Error,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		FinishedAt: task.FinishedAt,
	}
}

func workStatusForSubagent(status SubagentTaskStatus) string {
	switch status {
	case SubagentTaskStatusRunning, SubagentTaskStatusWaitingApproval:
		return work.StatusRunning
	case SubagentTaskStatusCompleted:
		return work.StatusCompleted
	case SubagentTaskStatusFailed:
		return work.StatusFailed
	case SubagentTaskStatusCanceled:
		return work.StatusCanceled
	default:
		return work.StatusPending
	}
}

func optionalTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
