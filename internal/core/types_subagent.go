package core

import (
	"time"
)

type SubagentTaskStatus string

const (
	SubagentTaskStatusPending         SubagentTaskStatus = "pending"
	SubagentTaskStatusRunning         SubagentTaskStatus = "running"
	SubagentTaskStatusWaitingApproval SubagentTaskStatus = "waiting_approval"
	SubagentTaskStatusCompleted       SubagentTaskStatus = "completed"
	SubagentTaskStatusFailed          SubagentTaskStatus = "failed"
	SubagentTaskStatusCanceled        SubagentTaskStatus = "canceled"
)

type SubagentTaskMode string

const (
	SubagentTaskModeBlocking SubagentTaskMode = "blocking"
	SubagentTaskModeAsync    SubagentTaskMode = "async"
)

type SubagentIsolation string

const (
	SubagentIsolationShared   SubagentIsolation = "shared"
	SubagentIsolationWorktree SubagentIsolation = "worktree"
)

type SubagentRuntime string

const (
	SubagentRuntimeMatrixClaw SubagentRuntime = "matrixclaw"
	SubagentRuntimeCodex      SubagentRuntime = "codex"
	SubagentRuntimeClaude     SubagentRuntime = "claude"
	SubagentRuntimeAuto       SubagentRuntime = "auto"
)

type SubagentTask struct {
	ID                        string             `json:"id"`
	AgentName                 string             `json:"agent_name,omitempty"`
	DisplayName               string             `json:"display_name,omitempty"`
	Mode                      SubagentTaskMode   `json:"mode"`
	Isolation                 SubagentIsolation  `json:"isolation"`
	ParentSessionID           string             `json:"parent_session_id"`
	ParentRunID               string             `json:"parent_run_id,omitempty"`
	ParentToolCallID          string             `json:"parent_tool_call_id,omitempty"`
	ChildSessionID            string             `json:"child_session_id,omitempty"`
	ChildRunID                string             `json:"child_run_id,omitempty"`
	Runtime                   string             `json:"runtime"`
	Goal                      string             `json:"goal"`
	Status                    SubagentTaskStatus `json:"status"`
	Summary                   string             `json:"summary,omitempty"`
	Error                     string             `json:"error,omitempty"`
	ResultMessageID           string             `json:"result_message_id,omitempty"`
	CompletionQueuedAt        *time.Time         `json:"completion_queued_at,omitempty"`
	CompletionDeliveredAt     *time.Time         `json:"completion_delivered_at,omitempty"`
	CompletionAutoResumeRunID string             `json:"completion_auto_resume_run_id,omitempty"`
	CreatedAt                 time.Time          `json:"created_at"`
	UpdatedAt                 time.Time          `json:"updated_at"`
	FinishedAt                *time.Time         `json:"finished_at,omitempty"`
}

type SubagentTaskFilter struct {
	ParentSessionID string
	Mode            SubagentTaskMode
	Statuses        []SubagentTaskStatus
	Limit           int
}
