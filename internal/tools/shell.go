package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/sandbox"
)

const (
	bashToolName       = "bash"
	taskOutputToolName = "task_output"
	taskKillToolName   = "task_kill"
	maxToolOutput      = 30000
)

// A foreground command is killed after its timeout, 10 minutes unless the call
// names up to 60, and moves to the background after 2 minutes. A call's
// seconds over maxSeconds are read as milliseconds, as many models send them.
const (
	DefaultCommandTimeout = 10 * time.Minute
	MaxCommandTimeout     = time.Hour
	DefaultAutoBackground = 2 * time.Minute
	maxTaskWait           = 10 * time.Minute
	maxSeconds            = int(MaxCommandTimeout / time.Second)
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

// ConcurrencyKey keys a command on the directory it runs in.
func (e *bashExecutor) ConcurrencyKey(call Call) string {
	var params BashParams
	_ = json.Unmarshal(call.Args, &params)
	return WorkingDirKey(resolvedPath(call.WorkingDir, params.WorkingDir))
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

// bashCommand reads a call into the command it runs; an error is the model's
// to fix, as is a command that would install the managed browser.
func bashCommand(call Call) (BashParams, Command, error) {
	var params BashParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return params, Command{}, InvalidArgs(bashToolName, err)
	}
	switch {
	case strings.TrimSpace(params.Command) == "":
		return params, Command{}, errors.New("command is required")
	case installsManagedBrowser(params.Command):
		return params, Command{}, errors.New(managedBrowserSetupMessage)
	}
	timeout, autoBackground, err := commandLimits(params)
	if err != nil {
		return params, Command{}, err
	}
	dir, err := ResolveFilesystemPath(call.WorkingDir, params.WorkingDir)
	if err != nil {
		return params, Command{}, fmt.Errorf("invalid working_dir: %w", err)
	}
	params.WorkingDir = dir.Path
	return params, Command{
		Command:        params.Command,
		Description:    params.Description,
		WorkingDir:     params.WorkingDir,
		Background:     params.RunInBackground,
		Timeout:        timeout,
		AutoBackground: autoBackground,
		OutputLimit:    maxToolOutput,
	}, nil
}

// Preview shows the command and the directory it runs in.
func (e *bashExecutor) Preview(_ context.Context, call Call) (ApprovalRequest, error) {
	params, _, err := bashCommand(call)
	if err != nil {
		return ApprovalRequest{}, err
	}
	return ApprovalRequest{Description: "Execute command: " + params.Command, Path: params.WorkingDir, Params: params}, nil
}

func (e *bashExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	params, command, err := bashCommand(call)
	if err != nil {
		return failure(err)
	}
	if e.tasks == nil {
		return Result{}, errShellUnavailable
	}
	result, err := e.tasks.RunCommand(ctx, call, command)
	if errors.Is(err, sandbox.ErrUnavailable) {
		return Result{Content: err.Error() + ". Tell the user; no command can run until they fix it.", Status: ResultStatusError}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("bash: %w", err)
	}
	meta := BashResponseMetadata{
		StartTime:        result.StartedAt.UnixMilli(),
		EndTime:          result.EndedAt.UnixMilli(),
		Description:      params.Description,
		WorkingDirectory: command.WorkingDir,
	}
	if result.TaskID != "" {
		meta.Background, meta.TaskID = true, result.TaskID
		return Result{Content: backgroundStartText(result.TaskID, params.RunInBackground, command.AutoBackground), Metadata: meta}, nil
	}
	meta.Output = strings.TrimSpace(result.Output)
	meta.ExitCode, meta.TimedOut, meta.OutputPath = result.ExitCode, result.TimedOut, result.OutputPath
	var status ResultStatus
	switch {
	case result.TimedOut || result.ExitCode != 0 && !noMatch(params.Command, meta.Output, result.ExitCode):
		status = ResultStatusError
	case result.ExitCode != 0:
		status = ResultStatusNeutral
	}
	return Result{Content: commandText(meta.Output, result, command.Timeout), Metadata: meta, Status: status, OutputPath: result.OutputPath}, nil
}

// commandText is a finished command's output followed by how it ended, the
// exit code always included, so that every model sees a failure.
func commandText(output string, result CommandResult, timeout time.Duration) string {
	parts := []string{output}
	if output == "" {
		parts[0] = "(no output)"
	}
	if result.OutputPath != "" {
		parts = append(parts, "(output cut to its head and tail; the full output is in "+result.OutputPath+": read or grep it)")
	}
	switch {
	case result.TimedOut:
		parts = append(parts, fmt.Sprintf("Timed out after %ds and killed. Run long commands with run_in_background: true.", int(timeout/time.Second)))
	case result.ExitCode < 0:
		parts = append(parts, "Exit code: -1 (ended by a signal)")
	default:
		parts = append(parts, fmt.Sprintf("Exit code: %d", result.ExitCode))
	}
	if result.KilledLeftovers {
		parts = append(parts, "Processes the command left running were killed; start servers and watchers with run_in_background: true.")
	}
	if result.SandboxHint != "" {
		parts = append(parts, result.SandboxHint)
	}
	return strings.Join(parts, "\n\n")
}

// commandLimits reads a call's timeout and auto-background delay; zero takes
// the defaults.
func commandLimits(params BashParams) (time.Duration, time.Duration, error) {
	timeout, err := callSeconds("timeout", params.Timeout)
	if err != nil {
		return 0, 0, err
	}
	if timeout > MaxCommandTimeout {
		return 0, 0, fmt.Errorf("timeout is in seconds, at most %d", maxSeconds)
	}
	autoBackground, err := callSeconds("auto_background_after", params.AutoBackgroundAfter)
	if err != nil {
		return 0, 0, err
	}
	if timeout == 0 {
		timeout = DefaultCommandTimeout
	}
	if autoBackground == 0 {
		autoBackground = DefaultAutoBackground
	}
	return timeout, autoBackground, nil
}

// callSeconds reads a duration a call gives in seconds; a value over
// maxSeconds is taken as milliseconds.
func callSeconds(name string, value int) (time.Duration, error) {
	switch {
	case value < 0:
		return 0, fmt.Errorf("%s is in seconds and must not be negative", name)
	case value > maxSeconds:
		return time.Duration(value) * time.Millisecond, nil
	}
	return time.Duration(value) * time.Second, nil
}

func backgroundStartText(taskID string, requested bool, after time.Duration) string {
	how := "Read its output with task_output (wait_seconds waits for it to finish); stop it with task_kill."
	if requested {
		return "Background task " + taskID + " started. " + how
	}
	return fmt.Sprintf("The command is still running after %ds, so it moved to the background as task %s. %s", int(after/time.Second), taskID, how)
}

func (e *taskOutputExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params TaskOutputParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(taskOutputToolName, err)
	}
	if strings.TrimSpace(params.ID) == "" {
		return Result{Content: "id is required", Status: ResultStatusError}, nil
	}
	wait := time.Duration(params.WaitSeconds) * time.Second
	if params.WaitSeconds < 0 || wait > maxTaskWait {
		return Result{Content: fmt.Sprintf("wait_seconds is at most %d", int(maxTaskWait/time.Second)), Status: ResultStatusError}, nil
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

// taskID reads the task a task_kill call stops.
func taskID(call Call) (string, error) {
	var params TaskKillParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return "", InvalidArgs(taskKillToolName, err)
	}
	id := strings.TrimSpace(params.ID)
	if id == "" {
		return "", errors.New("id is required")
	}
	return id, nil
}

func (e *taskKillExecutor) Preview(_ context.Context, call Call) (ApprovalRequest, error) {
	id, err := taskID(call)
	return ApprovalRequest{Description: "Stop background task " + id, Params: TaskKillParams{ID: id}}, err
}

func (e *taskKillExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	id, err := taskID(call)
	if err != nil {
		return failure(err)
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

// noMatch reports whether exit code 1 with no output only says that a
// search, test or process lookup on the command line found nothing.
func noMatch(command string, output string, exitCode int) bool {
	if exitCode != 1 || output != "" {
		return false
	}
	for _, words := range permission.ParseLine(command).Commands {
		name := filepath.Base(words[0])
		if probeCommands[name] || name == "git" && len(words) > 1 && words[1] == "grep" {
			return true
		}
	}
	return false
}

// probeCommands exit 1 when they find nothing.
var probeCommands = map[string]bool{
	"[": true, "egrep": true, "fgrep": true, "grep": true, "pgrep": true, "pidof": true,
	"pkill": true, "rg": true, "test": true, "which": true, "zgrep": true,
}

// ReadOnlySpec is the spec a read-only subagent gets for a mutating tool it
// may still call: bash that never asks and runs only read-only command lines
// (CheckReadOnlyCall). ok is false for every other tool.
func ReadOnlySpec(spec Spec) (Spec, bool) {
	if spec.ID != bashToolName {
		return Spec{}, false
	}
	spec.Effect, spec.Asks, spec.Description = EffectReadOnly, false, readOnlyBashDescription
	return spec, true
}

// CheckReadOnlyCall refuses a call made under a ReadOnlySpec that may change
// something or read outside the call's working directory, or where keys and
// credentials are kept.
func CheckReadOnlyCall(call Call) error {
	var params BashParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return InvalidArgs(bashToolName, err)
	}
	if err := permission.CheckReadOnly(params.Command); err != nil {
		return fmt.Errorf("this subagent is read-only, and bash runs only read-only commands: %w", err)
	}
	if err := checkReadOnlyPaths(params, resolvedPath(call.WorkingDir, params.WorkingDir)); err != nil {
		return fmt.Errorf("this subagent is read-only, and bash reads only inside the working directory, never where keys are kept: %w; use read, grep or glob instead", err)
	}
	return nil
}

// checkReadOnlyPaths refuses a command line whose path arguments resolve
// outside dir or to secrets, or that runs in or above the home directory,
// where a search would reach them.
func checkReadOnlyPaths(params BashParams, dir string) error {
	root := comparableFilesystemPath(dir)
	home, _ := os.UserHomeDir()
	if home = comparableFilesystemPath(home); home != "" && pathWithin(root, home) {
		return fmt.Errorf("%s holds the home directory", dir)
	}
	for _, words := range permission.ParseLine(params.Command).Commands {
		for _, word := range words[1:] {
			if _, value, ok := strings.Cut(word, "="); ok && strings.HasPrefix(word, "-") {
				word = value
			} else if strings.HasPrefix(word, "-") {
				continue
			}
			if !strings.ContainsRune(word, '/') && !strings.HasPrefix(word, ".") && !strings.HasPrefix(word, "~") {
				continue
			}
			path := word
			switch {
			case path == "~" || strings.HasPrefix(path, "~/"):
				path = filepath.Join(home, strings.TrimPrefix(path, "~"))
			case strings.HasPrefix(path, "~"):
				return fmt.Errorf("%s is another user's home", word)
			}
			resolved := comparableFilesystemPath(resolvedPath(dir, path))
			if !pathWithin(root, resolved) {
				return fmt.Errorf("%s is outside %s", word, dir)
			}
			if HoldsSecrets(resolved) || isProtectedCredentialPath(resolved) {
				return fmt.Errorf("%s holds secrets", word)
			}
		}
	}
	return nil
}
