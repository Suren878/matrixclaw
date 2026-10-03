package core

import "time"

// TaskStatus is where a background task stands.
type TaskStatus string

const (
	TaskStatusPending         TaskStatus = "pending"
	TaskStatusRunning         TaskStatus = "running"
	TaskStatusWaitingApproval TaskStatus = "waiting_approval"
	TaskStatusCompleted       TaskStatus = "completed"
	TaskStatusFailed          TaskStatus = "failed"
	TaskStatusCanceled        TaskStatus = "canceled"
	// TaskStatusLost is a shell task the daemon stopped when it restarted.
	TaskStatusLost TaskStatus = "lost"
)

// Terminal reports whether the task has ended.
func (s TaskStatus) Terminal() bool {
	return s == TaskStatusCompleted || s == TaskStatusFailed || s == TaskStatusCanceled || s == TaskStatusLost
}

// TaskKind says what a background task runs.
type TaskKind string

const (
	TaskKindShell    TaskKind = "shell"
	TaskKindSubagent TaskKind = "subagent"
)

// SubagentIsolation is where a subagent works: in its parent's directory or
// in a git worktree of its own.
type SubagentIsolation string

const (
	SubagentIsolationShared   SubagentIsolation = "shared"
	SubagentIsolationWorktree SubagentIsolation = "worktree"
)

// SubagentRuntime runs a subagent: matrixclaw's own engine or an external agent.
type SubagentRuntime string

const (
	SubagentRuntimeMatrixClaw SubagentRuntime = "matrixclaw"
	SubagentRuntimeCodex      SubagentRuntime = "codex"
	SubagentRuntimeClaude     SubagentRuntime = "claude"
	SubagentRuntimeAuto       SubagentRuntime = "auto"
)

// Task is work a session runs besides its runs: a shell command or a subagent
// that SessionID's run RunID started with its call ParentToolCallID. Command is
// the shell command or the subagent's prompt, Description its label; a blocking
// subagent is not Background. A finished background task whose DeliveredAt is
// nil is an event its session has not seen yet.
type Task struct {
	ID               string            `json:"id"`
	SessionID        string            `json:"session_id"`
	RunID            string            `json:"run_id,omitempty"`
	ParentToolCallID string            `json:"parent_tool_call_id,omitempty"`
	Kind             TaskKind          `json:"kind"`
	Status           TaskStatus        `json:"status"`
	Command          string            `json:"command"`
	Description      string            `json:"description,omitempty"`
	WorkingDir       string            `json:"working_dir,omitempty"`
	Background       bool              `json:"background"`
	AgentName        string            `json:"agent_name,omitempty"`
	Runtime          string            `json:"runtime,omitempty"`
	Model            string            `json:"model,omitempty"`
	Isolation        SubagentIsolation `json:"isolation,omitempty"`
	// Readonly subagents get read-only tools only.
	Readonly       bool       `json:"readonly,omitempty"`
	PID            int        `json:"pid,omitempty"`
	PGID           int        `json:"pgid,omitempty"`
	LeaderStart    string     `json:"-"`
	BootID         string     `json:"-"`
	OutputPath     string     `json:"output_path,omitempty"`
	ExitCode       *int       `json:"exit_code,omitempty"`
	OutputCursor   int64      `json:"output_cursor,omitempty"`
	ChildSessionID string     `json:"child_session_id,omitempty"`
	ChildRunID     string     `json:"child_run_id,omitempty"`
	Summary        string     `json:"summary,omitempty"`
	Error          string     `json:"error,omitempty"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	DeliveredRunID string     `json:"delivered_run_id,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

// TaskEnd is how a task ended; a Delivered end is no event for its session.
type TaskEnd struct {
	Status    TaskStatus
	ExitCode  *int
	Summary   string
	Error     string
	Delivered bool
	At        time.Time
}

// TaskFilter selects tasks; Background keeps background tasks only and
// Undelivered finished background tasks their session has not seen.
type TaskFilter struct {
	SessionID        string
	RunID            string
	ParentToolCallID string
	ChildRunID       string
	ChildSessionID   string
	Kind             TaskKind
	Statuses         []TaskStatus
	Background       bool
	Undelivered      bool
	Limit            int
}

// SessionTasksResponse lists a session's background tasks.
type SessionTasksResponse struct {
	Tasks []Task `json:"tasks"`
}

// TaskResponse carries one task.
type TaskResponse struct {
	Task Task `json:"task"`
}

// TaskDetailResponse is a task and the end of its output.
type TaskDetailResponse struct {
	Task       Task   `json:"task"`
	OutputTail string `json:"output_tail,omitempty"`
}
