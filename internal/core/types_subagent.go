package core

import (
	"time"
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
	ID               string            `json:"id"`
	AgentName        string            `json:"agent_name,omitempty"`
	DisplayName      string            `json:"display_name,omitempty"`
	Mode             SubagentTaskMode  `json:"mode"`
	Isolation        SubagentIsolation `json:"isolation"`
	ParentSessionID  string            `json:"parent_session_id"`
	ParentRunID      string            `json:"parent_run_id,omitempty"`
	ParentToolCallID string            `json:"parent_tool_call_id,omitempty"`
	ChildSessionID   string            `json:"child_session_id,omitempty"`
	ChildRunID       string            `json:"child_run_id,omitempty"`
	Runtime          string            `json:"runtime"`
	Goal             string            `json:"goal"`
	Status           TaskStatus        `json:"status"`
	Summary          string            `json:"summary,omitempty"`
	Error            string            `json:"error,omitempty"`
	ResultMessageID  string            `json:"result_message_id,omitempty"`
	// DeliveredAt is when the parent was told the task finished, and
	// DeliveredRunID the run that told it; nil while that is due.
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	DeliveredRunID string     `json:"delivered_run_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

type SubagentTaskFilter struct {
	ParentSessionID string
	Mode            SubagentTaskMode
	Statuses        []TaskStatus
	Limit           int
}
