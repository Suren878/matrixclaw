package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	bashToolName       = "bash"
	taskOutputToolName = "task_output"
	taskKillToolName   = "task_kill"
	maxToolOutput      = 30000
)

// A foreground command is killed after its timeout, 10 minutes unless the call
// names up to 60, and moves to the background after 2 minutes.
const (
	DefaultCommandTimeout = 10 * time.Minute
	MaxCommandTimeout     = time.Hour
	DefaultAutoBackground = 2 * time.Minute
	maxTaskWait           = 10 * time.Minute
)

var errShellUnavailable = errors.New("shell commands are not available in this daemon")

type BashParams struct {
	Description         string `json:"description,omitempty"`
	Command             string `json:"command"`
	WorkingDir          string `json:"working_dir,omitempty"`
	RunInBackground     bool   `json:"run_in_background,omitempty"`
	Timeout             int    `json:"timeout,omitempty"`
	AutoBackgroundAfter int    `json:"auto_background_after,omitempty"`
}

type BashPermissionsParams struct {
	Description         string `json:"description"`
	Command             string `json:"command"`
	WorkingDir          string `json:"working_dir"`
	RunInBackground     bool   `json:"run_in_background"`
	Timeout             int    `json:"timeout"`
	AutoBackgroundAfter int    `json:"auto_background_after"`
}

type BashResponseMetadata struct {
	StartTime        int64  `json:"start_time"`
	EndTime          int64  `json:"end_time"`
	Output           string `json:"output"`
	ExitCode         int    `json:"exit_code,omitempty"`
	Description      string `json:"description,omitempty"`
	WorkingDirectory string `json:"working_directory"`
	Background       bool   `json:"background,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	TimedOut         bool   `json:"timed_out,omitempty"`
	OutputPath       string `json:"output_path,omitempty"`
}

type TaskOutputParams struct {
	ID          string `json:"id"`
	WaitSeconds int    `json:"wait_seconds,omitempty"`
	Filter      string `json:"filter,omitempty"`
}

type TaskKillParams struct {
	ID string `json:"id"`
}

type bashExecutor struct{ tasks ShellTasks }
type taskOutputExecutor struct{ tasks ShellTasks }
type taskKillExecutor struct{ tasks ShellTasks }

// NewShellExecutors returns bash, task_output and task_kill over tasks.
func NewShellExecutors(tasks ShellTasks) []Executor {
	return []Executor{&bashExecutor{tasks: tasks}, &taskOutputExecutor{tasks: tasks}, &taskKillExecutor{tasks: tasks}}
}

func (e *bashExecutor) Spec() Spec {
	return coreDefinitionSpec(bashToolName)
}

func (e *taskOutputExecutor) Spec() Spec {
	return coreDefinitionSpec(taskOutputToolName)
}

// ConcurrencyKey lets the reads of one task take turns, as each moves its cursor.
func (e *taskOutputExecutor) ConcurrencyKey(call Call) string {
	var params TaskOutputParams
	_ = json.Unmarshal(call.Args, &params)
	return "task:" + strings.TrimSpace(params.ID)
}

func (e *taskKillExecutor) Spec() Spec {
	return coreDefinitionSpec(taskKillToolName)
}

func (e *bashExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params BashParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(bashToolName, err)
	}
	if strings.TrimSpace(params.Command) == "" {
		return Result{Content: "command is required", IsError: true}, nil
	}
	if blockedManagedBrowserInstallCommand(params.Command) {
		return Result{Content: managedBrowserSetupMessage, Status: ResultStatusError, IsError: true}, nil
	}
	timeout, autoBackground, err := commandLimits(params)
	if err != nil {
		return Result{Content: err.Error(), Status: ResultStatusError, IsError: true}, nil
	}
	workingDir := resolvePath(call.WorkingDir, params.WorkingDir)
	if !call.Approved {
		return approvalResult(bashToolName, "execute", workingDir, "Execute command: "+params.Command, BashPermissionsParams{
			Description:         params.Description,
			Command:             params.Command,
			WorkingDir:          workingDir,
			RunInBackground:     params.RunInBackground,
			Timeout:             params.Timeout,
			AutoBackgroundAfter: params.AutoBackgroundAfter,
		}), nil
	}
	if e.tasks == nil {
		return Result{}, errShellUnavailable
	}
	result, err := e.tasks.RunCommand(ctx, call, Command{
		Command:        params.Command,
		Description:    params.Description,
		WorkingDir:     workingDir,
		Background:     params.RunInBackground,
		Timeout:        timeout,
		AutoBackground: autoBackground,
		OutputLimit:    maxToolOutput,
	})
	if err != nil {
		return Result{}, fmt.Errorf("bash: %w", err)
	}
	meta := BashResponseMetadata{
		StartTime:        result.StartedAt.UnixMilli(),
		EndTime:          result.EndedAt.UnixMilli(),
		Description:      params.Description,
		WorkingDirectory: workingDir,
	}
	if result.TaskID != "" {
		meta.Background, meta.TaskID = true, result.TaskID
		return Result{Content: backgroundStartText(result.TaskID, params.RunInBackground, autoBackground), Metadata: meta}, nil
	}
	meta.Output = strings.TrimSpace(result.Output)
	meta.ExitCode, meta.TimedOut, meta.OutputPath = result.ExitCode, result.TimedOut, result.OutputPath
	content := meta.Output
	if result.OutputPath != "" {
		content += "\n\n(output truncated; the full output is in " + result.OutputPath + ")"
	}
	if result.TimedOut {
		content += fmt.Sprintf("\n\n(killed after its timeout of %s; run long commands with run_in_background)", timeout)
		return Result{Content: strings.TrimSpace(content), Metadata: meta, Status: ResultStatusError, IsError: true}, nil
	}
	if result.ExitCode == 0 {
		return Result{Content: content, Metadata: meta}, nil
	}
	status := ResultStatusError
	if isExpectedEmptyProcessProbe(params.Command, meta.Output, result.ExitCode) {
		status = ResultStatusNeutral
	}
	return Result{Content: content, Metadata: meta, Status: status, IsError: status == ResultStatusError}, nil
}

// commandLimits reads a call's timeout and auto-background delay in seconds;
// zero takes the defaults.
func commandLimits(params BashParams) (time.Duration, time.Duration, error) {
	timeout := time.Duration(params.Timeout) * time.Second
	switch {
	case params.Timeout < 0 || timeout > MaxCommandTimeout:
		return 0, 0, fmt.Errorf("timeout is in seconds, at most %d", int(MaxCommandTimeout/time.Second))
	case params.AutoBackgroundAfter < 0:
		return 0, 0, errors.New("auto_background_after is in seconds and must not be negative")
	case timeout == 0:
		timeout = DefaultCommandTimeout
	}
	autoBackground := time.Duration(params.AutoBackgroundAfter) * time.Second
	if autoBackground == 0 {
		autoBackground = DefaultAutoBackground
	}
	return timeout, autoBackground, nil
}

func backgroundStartText(taskID string, requested bool, after time.Duration) string {
	how := "Read its output with task_output (wait_seconds waits for it to finish); stop it with task_kill."
	if requested {
		return "Background task " + taskID + " started. " + how
	}
	return fmt.Sprintf("The command is still running after %s, so it went on as background task %s. %s", after, taskID, how)
}

func (e *taskOutputExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params TaskOutputParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(taskOutputToolName, err)
	}
	if strings.TrimSpace(params.ID) == "" {
		return Result{Content: "id is required", IsError: true}, nil
	}
	wait := time.Duration(params.WaitSeconds) * time.Second
	if params.WaitSeconds < 0 || wait > maxTaskWait {
		return Result{Content: fmt.Sprintf("wait_seconds is at most %d", int(maxTaskWait/time.Second)), IsError: true}, nil
	}
	if e.tasks == nil {
		return Result{}, errShellUnavailable
	}
	out, err := e.tasks.ReadTaskOutput(ctx, call, TaskRead{TaskID: strings.TrimSpace(params.ID), Wait: wait, Filter: params.Filter, Limit: maxToolOutput})
	if err != nil {
		return Result{}, err
	}
	waiting := wait > 0 && out.Running && out.Text == "" && out.Skipped == 0
	return Result{Content: taskOutputText(out), Metadata: out.TaskInfo, Status: ResultStatusNeutral, Waiting: waiting}, nil
}

func taskOutputText(out TaskOutput) string {
	var parts []string
	if out.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("[... %d bytes of output dropped ...]", out.Skipped))
	}
	if text := strings.TrimRight(out.Text, "\n"); text != "" {
		parts = append(parts, text)
	} else {
		parts = append(parts, "(no new output)")
	}
	status := "(task " + out.Status
	if out.ExitCode != nil {
		status += fmt.Sprintf(", exit code %d", *out.ExitCode)
	}
	parts = append(parts, status+")")
	if out.More {
		parts = append(parts, "(more output is waiting: call task_output again, or read "+out.OutputPath+")")
	}
	return strings.Join(parts, "\n\n")
}

func (e *taskKillExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params TaskKillParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(taskKillToolName, err)
	}
	id := strings.TrimSpace(params.ID)
	if id == "" {
		return Result{Content: "id is required", IsError: true}, nil
	}
	if !call.Approved {
		return approvalResult(taskKillToolName, "kill", id, "Stop background task "+id, params), nil
	}
	if e.tasks == nil {
		return Result{}, errShellUnavailable
	}
	info, err := e.tasks.StopTask(ctx, call, id)
	if err != nil {
		return Result{}, err
	}
	return Result{Content: "Task " + id + " is " + info.Status + ".", Metadata: info}, nil
}

func isExpectedEmptyProcessProbe(command string, output string, exitCode int) bool {
	if exitCode != 1 || strings.TrimSpace(output) != "" {
		return false
	}
	return IsProcessProbeCommand(command)
}

func IsProcessProbeCommand(command string) bool {
	command = strings.ToLower(strings.TrimSpace(command))
	switch {
	case strings.Contains(command, "grep -v grep"):
		return true
	case strings.Contains(command, "pgrep"):
		return true
	case strings.Contains(command, "pidof"):
		return true
	case strings.Contains(command, "pkill"):
		return true
	case strings.Contains(command, "ps ") && strings.Contains(command, "grep"):
		return true
	default:
		return false
	}
}
