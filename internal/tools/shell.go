package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	bashToolName      = "bash"
	jobOutputToolName = "job_output"
	jobKillToolName   = "job_kill"
	maxToolOutput     = 30000
)

type BashParams struct {
	Description         string `json:"description,omitempty"`
	Command             string `json:"command"`
	WorkingDir          string `json:"working_dir,omitempty"`
	RunInBackground     bool   `json:"run_in_background,omitempty"`
	AutoBackgroundAfter int    `json:"auto_background_after,omitempty"`
}

type BashPermissionsParams struct {
	Description         string `json:"description"`
	Command             string `json:"command"`
	WorkingDir          string `json:"working_dir"`
	RunInBackground     bool   `json:"run_in_background"`
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
	ShellID          string `json:"shell_id,omitempty"`
}

type JobOutputParams struct {
	ShellID string `json:"shell_id"`
	Wait    bool   `json:"wait,omitempty"`
}

type JobKillParams struct {
	ShellID string `json:"shell_id"`
}

type JobOutputResponseMetadata struct {
	ShellID          string `json:"shell_id"`
	Command          string `json:"command"`
	Description      string `json:"description"`
	Done             bool   `json:"done"`
	WorkingDirectory string `json:"working_directory"`
}

type JobKillResponseMetadata struct {
	ShellID     string `json:"shell_id"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

type jobState struct {
	info       BackgroundJob
	cmd        *exec.Cmd
	output     synchronizedBuffer
	mu         sync.Mutex
	done       bool
	err        error
	finishedAt time.Time
}

type synchronizedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type jobManager struct {
	mu   sync.RWMutex
	jobs map[string]*jobState
}

type bashExecutor struct{}
type jobOutputExecutor struct{}
type jobKillExecutor struct{}

var defaultJobManager = &jobManager{jobs: map[string]*jobState{}}

func NewBashExecutor() Executor      { return &bashExecutor{} }
func NewJobOutputExecutor() Executor { return &jobOutputExecutor{} }
func NewJobKillExecutor() Executor   { return &jobKillExecutor{} }

func (e *bashExecutor) Spec() Spec {
	return Spec{
		ID:              bashToolName,
		Name:            "Bash",
		Description:     "Run a shell command",
		Risk:            RiskApproval,
		Namespace:       namespaceCoreShell,
		Category:        CategoryShell,
		Profiles:        []Profile{ProfileCoding},
		OutputKind:      OutputText,
		InputJSONSchema: bashInputSchema,
	}
}

func (e *jobOutputExecutor) Spec() Spec {
	return Spec{
		ID:              jobOutputToolName,
		Name:            "JobOutput",
		Description:     "Read background job output",
		Risk:            RiskSafe,
		Namespace:       namespaceCoreShell,
		Category:        CategoryShell,
		Profiles:        []Profile{ProfileCoding},
		OutputKind:      OutputJob,
		InputJSONSchema: jobOutputInputSchema,
	}
}

func (e *jobKillExecutor) Spec() Spec {
	return Spec{
		ID:              jobKillToolName,
		Name:            "JobKill",
		Description:     "Kill a background job",
		Risk:            RiskApproval,
		Namespace:       namespaceCoreShell,
		Category:        CategoryShell,
		Profiles:        []Profile{ProfileCoding},
		OutputKind:      OutputJob,
		InputJSONSchema: jobKillInputSchema,
	}
}

func (e *bashExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params BashParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(bashToolName, err)
	}
	if strings.TrimSpace(params.Command) == "" {
		return Result{Content: "command is required", IsError: true}, nil
	}
	workingDir := resolvePath(call.WorkingDir, params.WorkingDir)
	if !call.Approved {
		return approvalResult(bashToolName, "execute", workingDir, "Execute command: "+params.Command, BashPermissionsParams{
			Description:         params.Description,
			Command:             params.Command,
			WorkingDir:          workingDir,
			RunInBackground:     params.RunInBackground,
			AutoBackgroundAfter: params.AutoBackgroundAfter,
		}), nil
	}

	if params.RunInBackground {
		job, err := defaultJobManager.start(params, workingDir)
		if err != nil {
			return Result{}, fmt.Errorf("bash: start background job: %w", err)
		}
		return Result{
			Content: fmt.Sprintf("Background job started: %s", job.info.ID),
			Metadata: BashResponseMetadata{
				StartTime:        job.info.StartedAt.UnixMilli(),
				EndTime:          time.Now().UnixMilli(),
				Output:           "",
				Description:      job.info.Description,
				WorkingDirectory: job.info.WorkingDir,
				Background:       true,
				ShellID:          job.info.ID,
			},
			Background: &job.info,
		}, nil
	}

	startedAt := time.Now()
	cmd := exec.CommandContext(ctx, "bash", "-lc", params.Command)
	cmd.Dir = workingDir
	configureCommandProcessGroup(cmd, true)
	output, err := cmd.CombinedOutput()
	trimmed := trimOutput(string(output))
	if err != nil {
		exitCode := commandExitCode(err)
		isError := !isExpectedEmptyProcessProbe(params.Command, trimmed, exitCode)
		status := ResultStatusError
		if !isError {
			status = ResultStatusNeutral
		}
		return Result{
			Content: trimmed,
			Metadata: BashResponseMetadata{
				StartTime:        startedAt.UnixMilli(),
				EndTime:          time.Now().UnixMilli(),
				Output:           trimmed,
				ExitCode:         exitCode,
				Description:      params.Description,
				WorkingDirectory: workingDir,
			},
			Status:  status,
			IsError: isError,
		}, nil
	}

	return Result{
		Content: trimmed,
		Metadata: BashResponseMetadata{
			StartTime:        startedAt.UnixMilli(),
			EndTime:          time.Now().UnixMilli(),
			Output:           trimmed,
			Description:      params.Description,
			WorkingDirectory: workingDir,
		},
	}, nil
}

func (e *jobOutputExecutor) Execute(_ context.Context, call Call) (Result, error) {
	var params JobOutputParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(jobOutputToolName, err)
	}
	job, ok := defaultJobManager.get(strings.TrimSpace(params.ShellID))
	if !ok {
		return Result{Content: "job not found", IsError: true}, nil
	}
	if params.Wait {
		for {
			_, done, _, _ := job.snapshot()
			if done {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	output, done, _, jobErr := job.snapshot()
	content := trimOutput(output)
	if content == "" {
		content = "no output"
	}
	if done {
		if jobErr != nil {
			content += "\n\n(job failed: " + jobErr.Error() + ")"
		} else {
			content += "\n\n(job completed)"
		}
	}
	return Result{
		Content: content,
		Metadata: JobOutputResponseMetadata{
			ShellID:          job.info.ID,
			Command:          job.info.Command,
			Description:      job.info.Description,
			Done:             done,
			WorkingDirectory: job.info.WorkingDir,
		},
		IsError: done && jobErr != nil,
	}, nil
}

func (e *jobKillExecutor) Execute(_ context.Context, call Call) (Result, error) {
	var params JobKillParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(jobKillToolName, err)
	}
	jobID := strings.TrimSpace(params.ShellID)
	if jobID == "" {
		return Result{Content: "shell_id is required", IsError: true}, nil
	}
	if !call.Approved {
		return approvalResult(jobKillToolName, "kill", jobID, "Kill background job "+jobID, params), nil
	}
	job, ok := defaultJobManager.get(jobID)
	if !ok {
		return Result{Content: "job not found", IsError: true}, nil
	}
	if err := defaultJobManager.kill(jobID); err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	return Result{
		Content: "job killed: " + jobID,
		Metadata: JobKillResponseMetadata{
			ShellID:     jobID,
			Command:     job.info.Command,
			Description: job.info.Description,
		},
	}, nil
}

func (m *jobManager) start(params BashParams, workingDir string) (*jobState, error) {
	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())
	cmd := exec.Command("bash", "-lc", params.Command)
	cmd.Dir = workingDir
	configureCommandProcessGroup(cmd, false)

	job := &jobState{
		info: BackgroundJob{
			ID:          jobID,
			Command:     params.Command,
			WorkingDir:  workingDir,
			Description: params.Description,
			StartedAt:   time.Now(),
		},
		cmd: cmd,
	}
	cmd.Stdout = &job.output
	cmd.Stderr = &job.output
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.jobs[jobID] = job
	m.mu.Unlock()

	go func() {
		err := cmd.Wait()
		job.mu.Lock()
		job.done = true
		job.err = err
		job.finishedAt = time.Now()
		job.mu.Unlock()
	}()

	return job, nil
}

func (m *jobManager) get(jobID string) (*jobState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[jobID]
	return job, ok
}

func (m *jobManager) kill(jobID string) error {
	job, ok := m.get(jobID)
	if !ok {
		return fmt.Errorf("job not found")
	}
	if job.cmd.Process == nil {
		return fmt.Errorf("job has no running process")
	}
	if err := killCommandProcessGroup(job.cmd); err != nil {
		return err
	}
	return nil
}

func configureCommandProcessGroup(cmd *exec.Cmd, canCancel bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if !canCancel {
		return
	}
	cmd.Cancel = func() error {
		return killCommandProcessGroup(cmd)
	}
	cmd.WaitDelay = 2 * time.Second
}

func killCommandProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}

func commandExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
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

func (j *jobState) snapshot() (string, bool, time.Time, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.output.String(), j.done, j.finishedAt, j.err
}

func trimOutput(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxToolOutput {
		return value
	}
	return value[:maxToolOutput] + "\n\n(output truncated)"
}
