package tools

import (
	"context"
	"time"
)

// ShellTasks runs the commands of bash calls; a command that outlives its call
// becomes a background task of the call's session.
type ShellTasks interface {
	RunCommand(ctx context.Context, call Call, command Command) (CommandResult, error)
	ReadTaskOutput(ctx context.Context, call Call, read TaskRead) (TaskOutput, error)
	StopTask(ctx context.Context, call Call, taskID string) (TaskInfo, error)
}

// Command is a shell command to run for a call. Background starts it as a
// background task at once; otherwise Timeout kills it and, when shorter than
// Timeout, AutoBackground moves it to the background while it still runs.
// OutputLimit caps the output a finished command returns.
type Command struct {
	Command        string
	Description    string
	WorkingDir     string
	Background     bool
	Timeout        time.Duration
	AutoBackground time.Duration
	OutputLimit    int
}

// CommandResult is how a command ended, or the task it went on as (TaskID).
// OutputPath names the file holding the whole output when Output was cut.
type CommandResult struct {
	TaskID     string
	Output     string
	OutputPath string
	ExitCode   int
	TimedOut   bool
	StartedAt  time.Time
	EndedAt    time.Time
}

// TaskRead asks for a task's output since the last read, waiting up to Wait
// for the task to finish; Filter keeps the lines matching a regular expression.
type TaskRead struct {
	TaskID string
	Wait   time.Duration
	Filter string
	Limit  int
}

// TaskInfo describes a background task to the model and clients.
type TaskInfo struct {
	TaskID      string `json:"task_id"`
	Kind        string `json:"kind"`
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	OutputPath  string `json:"output_path,omitempty"`
}

// TaskOutput is a task's output since the last read. Skipped counts bytes
// dropped from the file before Text; More is set when more output is waiting;
// Running is set while the task has not finished.
type TaskOutput struct {
	TaskInfo
	Text    string
	Skipped int64
	More    bool
	Running bool
}
