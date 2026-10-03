package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/shelltask"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// taskOutputDir holds a session's background task output files.
const taskOutputDir = "tasks"

// taskStopGrace is how long a stopped command may take to end on SIGTERM
// before its process group is killed.
const taskStopGrace = 3 * time.Second

// foregroundWaitDelay is how long a finished foreground command waits for
// processes it left behind to close their output.
const foregroundWaitDelay = 2 * time.Second

// DefaultBackgroundTasks is how many background commands a session runs at
// once unless the daemon configures another limit.
const DefaultBackgroundTasks = 8

// WithBackgroundTaskLimit bounds the background commands one session runs at
// once; 0 or less keeps the default.
func (c *Core) WithBackgroundTaskLimit(n int) *Core {
	if n <= 0 {
		n = DefaultBackgroundTasks
	}
	c.backgroundTasks = n
	return c
}

// RunCommand runs a bash call's command. A background command, or a foreground
// one still running after its AutoBackground delay, goes on as a task of the
// call's session while it runs fewer than its limit; a foreground one is
// stopped at its timeout or when ctx stops.
func (c *Core) RunCommand(ctx context.Context, call tools.Call, command tools.Command) (tools.CommandResult, error) {
	if command.Background && c.backgroundTasksFull(call.SessionID) {
		return tools.CommandResult{}, c.backgroundTasksFullError()
	}
	id := c.newID("task")
	path, err := c.taskOutputPath(call.SessionID, id)
	if err != nil {
		return tools.CommandResult{}, err
	}
	out, err := shelltask.CreateOutput(path)
	if err != nil {
		return tools.CommandResult{}, err
	}
	process, err := shelltask.Start(command.Command, command.WorkingDir, out)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(path)
		return tools.CommandResult{}, err
	}
	if command.Background {
		result, err := c.adoptCommand(ctx, call, command, id, process, time.Time{})
		if errors.Is(err, errBackgroundTasksFull) {
			_ = process.Stop(taskStopGrace)
			process.WaitOutput(foregroundWaitDelay)
			_ = os.Remove(path)
		}
		return result, err
	}
	timeout := time.NewTimer(command.Timeout)
	defer timeout.Stop()
	var background <-chan time.Time
	if command.AutoBackground > 0 && command.AutoBackground < command.Timeout {
		timer := time.NewTimer(command.AutoBackground)
		defer timer.Stop()
		background = timer.C
	}
	for {
		select {
		case <-process.Exited():
			process.WaitOutput(foregroundWaitDelay)
			return c.finishedCommand(process, command, false)
		case <-timeout.C:
			_ = process.Stop(taskStopGrace)
			process.WaitOutput(foregroundWaitDelay)
			return c.finishedCommand(process, command, true)
		case <-background:
			result, err := c.adoptCommand(ctx, call, command, id, process, process.StartedAt().Add(command.Timeout))
			if !errors.Is(err, errBackgroundTasksFull) {
				return result, err
			}
			// The session runs all the background commands it may; this one
			// stays in the foreground.
			background = nil
		case <-ctx.Done():
			_ = process.Stop(taskStopGrace)
			process.WaitOutput(foregroundWaitDelay)
			_ = os.Remove(path)
			return tools.CommandResult{}, ctx.Err()
		}
	}
}

// errBackgroundTasksFull is returned while a session runs as many background
// commands as it may.
var errBackgroundTasksFull = fmt.Errorf("%w: too many background commands", ErrInvalidInput)

func (c *Core) backgroundTasksFullError() error {
	return fmt.Errorf("%w: this session already runs %d background commands; wait for one (await, task_output with wait_seconds) or stop one with task_kill first", errBackgroundTasksFull, c.backgroundTasks)
}

func (c *Core) backgroundTasksFull(sessionID string) bool {
	c.tasksMu.Lock()
	defer c.tasksMu.Unlock()
	return c.sessionLiveTasksLocked(sessionID) >= c.backgroundTasks
}

func (c *Core) sessionLiveTasksLocked(sessionID string) int {
	n := 0
	for _, live := range c.liveTasks {
		if live.sessionID == sessionID {
			n++
		}
	}
	return n
}

// finishedCommand reads a foreground command's output; the file is kept only
// when the output is longer than the call may return.
func (c *Core) finishedCommand(process *shelltask.Process, command tools.Command, timedOut bool) (tools.CommandResult, error) {
	path := process.Output().Path()
	result := tools.CommandResult{ExitCode: process.ExitCode(), TimedOut: timedOut, StartedAt: process.StartedAt(), EndedAt: c.now()}
	chunk, err := shelltask.Read(path, 0, command.OutputLimit)
	if err != nil {
		return tools.CommandResult{}, err
	}
	result.Output = chunk.Text
	if chunk.More {
		result.OutputPath = path
		return result, nil
	}
	_ = os.Remove(path)
	return result, nil
}

// adoptCommand records a running command as a background task and watches it
// until it ends or, with a deadline, is stopped then. While the session runs
// as many as it may, it fails with errBackgroundTasksFull and leaves the
// command alone.
func (c *Core) adoptCommand(ctx context.Context, call tools.Call, command tools.Command, id string, process *shelltask.Process, deadline time.Time) (tools.CommandResult, error) {
	now := c.now().UTC()
	leader := process.Leader()
	task := Task{
		ID:               id,
		SessionID:        call.SessionID,
		RunID:            call.RunID,
		ParentToolCallID: call.ToolCallID,
		Kind:             TaskKindShell,
		Status:           TaskStatusRunning,
		Command:          command.Command,
		Description:      command.Description,
		WorkingDir:       command.WorkingDir,
		Background:       true,
		PID:              leader.PID,
		PGID:             leader.PID,
		LeaderStart:      leader.Start,
		BootID:           leader.BootID,
		OutputPath:       process.Output().Path(),
		StartedAt:        process.StartedAt().UTC(),
		UpdatedAt:        now,
	}
	live := &liveTask{sessionID: call.SessionID, process: process, recorded: make(chan struct{})}
	c.tasksMu.Lock()
	if c.sessionLiveTasksLocked(call.SessionID) >= c.backgroundTasks {
		c.tasksMu.Unlock()
		return tools.CommandResult{}, c.backgroundTasksFullError()
	}
	c.liveTasks[id] = live
	c.tasksMu.Unlock()
	if err := c.store.CreateTask(context.WithoutCancel(ctx), task); err != nil {
		c.forgetLiveTask(id)
		_ = process.Stop(taskStopGrace)
		return tools.CommandResult{}, err
	}
	c.publishTaskUpdated(task)
	safego.Go("core.watchShellTask", func() { c.watchShellTask(task, live, deadline) })
	return tools.CommandResult{TaskID: id, StartedAt: process.StartedAt(), EndedAt: now}, nil
}

// liveTask is a shell task of this daemon; recorded is closed once its end is stored.
type liveTask struct {
	sessionID string
	process   *shelltask.Process
	recorded  chan struct{}
}

func (c *Core) watchShellTask(task Task, live *liveTask, deadline time.Time) {
	defer c.forgetLiveTask(task.ID)
	defer close(live.recorded)
	process := live.process
	errText := ""
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		select {
		case <-process.Done():
		case <-timer.C:
			_ = process.Stop(taskStopGrace)
			errText = fmt.Sprintf("killed when its timeout of %s ran out", deadline.Sub(task.StartedAt).Round(time.Second))
		}
	}
	code := process.ExitCode()
	status := TaskStatusCompleted
	if code != 0 {
		status = TaskStatusFailed
	}
	if err := c.finishTask(context.Background(), task.ID, status, &code, errText); err != nil {
		log.Printf("core: finish task %q: %v", task.ID, err)
	}
}

// finishTask ends a task unless it already ended and tells its session's
// clients; the finished task becomes an event for the session.
func (c *Core) finishTask(ctx context.Context, taskID string, status TaskStatus, exitCode *int, errText string) error {
	task, finished, err := c.recordTaskEnd(ctx, taskID, status, exitCode, errText)
	if err != nil || !finished {
		return err
	}
	c.taskFinished(ctx, task)
	return nil
}

// recordTaskEnd stores how a task ended, unless it already had, and tells its
// session's clients.
func (c *Core) recordTaskEnd(ctx context.Context, taskID string, status TaskStatus, exitCode *int, errText string) (Task, bool, error) {
	finished, err := c.store.FinishTask(ctx, taskID, TaskEnd{Status: status, ExitCode: exitCode, Error: errText, At: c.now().UTC()})
	if err != nil || !finished {
		return Task{}, false, err
	}
	task, err := c.store.GetTask(ctx, taskID)
	if err != nil {
		return Task{}, false, err
	}
	c.publishTaskUpdated(task)
	return task, true, nil
}

func (c *Core) forgetLiveTask(id string) {
	c.tasksMu.Lock()
	delete(c.liveTasks, id)
	c.tasksMu.Unlock()
}

func (c *Core) liveTask(id string) (*liveTask, bool) {
	c.tasksMu.Lock()
	defer c.tasksMu.Unlock()
	live, ok := c.liveTasks[id]
	return live, ok
}

// ReadTaskOutput returns a task's output since the call's session last read it;
// it waits up to read.Wait for a running task to finish.
func (c *Core) ReadTaskOutput(ctx context.Context, call tools.Call, read tools.TaskRead) (tools.TaskOutput, error) {
	task, err := c.sessionTask(ctx, call.SessionID, read.TaskID)
	if err != nil {
		return tools.TaskOutput{}, err
	}
	var filter *regexp.Regexp
	if strings.TrimSpace(read.Filter) != "" {
		if filter, err = regexp.Compile(read.Filter); err != nil {
			return tools.TaskOutput{}, fmt.Errorf("%w: filter: %v", ErrInvalidInput, err)
		}
	}
	if task.FinishedAt == nil {
		if task, err = c.awaitTask(ctx, task, read.Wait); err != nil {
			return tools.TaskOutput{}, err
		}
	}
	if task.FinishedAt != nil {
		// The model reads how the task ended here and needs no event about it.
		if err := c.store.MarkTasksDelivered(ctx, []string{task.ID}, call.RunID, c.now().UTC()); err != nil {
			return tools.TaskOutput{}, err
		}
	}
	out := tools.TaskOutput{TaskInfo: taskInfo(task), Running: task.FinishedAt == nil}
	if task.Kind != TaskKindShell {
		out.Text = firstNonEmpty(task.Summary, task.Error)
		return out, nil
	}
	chunk, err := shelltask.Read(task.OutputPath, task.OutputCursor, read.Limit)
	if err != nil {
		return tools.TaskOutput{}, err
	}
	if err := c.store.SetTaskCursor(ctx, task.ID, chunk.Next); err != nil {
		return tools.TaskOutput{}, err
	}
	out.Text, out.Skipped, out.More = chunk.Text, chunk.Skipped, chunk.More
	if filter != nil {
		out.Text = matchingLines(out.Text, filter)
	}
	return out, nil
}

// taskPollInterval is how often awaitTask looks at a task that is not a
// shell task of this daemon.
const taskPollInterval = 250 * time.Millisecond

// awaitTask returns a running task once it finished or wait ran out. A shell
// task of this daemon is done once its end is stored; any other shell task
// ended before, maybe after it was read.
func (c *Core) awaitTask(ctx context.Context, task Task, wait time.Duration) (Task, error) {
	var recorded <-chan struct{}
	if live, ok := c.liveTask(task.ID); ok {
		recorded = live.recorded
	} else if task.Kind == TaskKindShell {
		return c.store.GetTask(ctx, task.ID)
	}
	if wait <= 0 {
		return task, nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	poll := time.NewTicker(taskPollInterval)
	defer poll.Stop()
	for {
		select {
		case <-recorded:
			return c.store.GetTask(ctx, task.ID)
		case <-timer.C:
			return c.store.GetTask(ctx, task.ID)
		case <-ctx.Done():
			return Task{}, ctx.Err()
		case <-poll.C:
			if current, err := c.store.GetTask(ctx, task.ID); err != nil || current.FinishedAt != nil {
				return current, err
			}
		}
	}
}

func matchingLines(text string, filter *regexp.Regexp) string {
	var kept []string
	for line := range strings.SplitSeq(text, "\n") {
		if filter.MatchString(line) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// StopTask stops a task of the call's session for the model, which then needs
// no event about it.
func (c *Core) StopTask(ctx context.Context, call tools.Call, taskID string) (tools.TaskInfo, error) {
	task, err := c.sessionTask(ctx, call.SessionID, taskID)
	if err != nil {
		return tools.TaskInfo{}, err
	}
	if err := c.store.MarkTasksDelivered(ctx, []string{task.ID}, call.RunID, c.now().UTC()); err != nil {
		return tools.TaskInfo{}, err
	}
	task, err = c.cancelTask(ctx, task, "stopped with task_kill")
	if err != nil {
		return tools.TaskInfo{}, err
	}
	return taskInfo(task), nil
}

// cancelTask ends a running task: a shell task's process group is stopped, a
// subagent's run is canceled. A finished task is returned as it is.
func (c *Core) cancelTask(ctx context.Context, task Task, reason string) (Task, error) {
	if task.Status.Terminal() {
		return task, nil
	}
	if task.Kind == TaskKindSubagent {
		if _, err := c.CancelRun(ctx, task.ChildRunID); err != nil {
			return Task{}, err
		}
		return c.store.GetTask(ctx, task.ID)
	}
	if err := c.finishTask(ctx, task.ID, TaskStatusCanceled, nil, reason); err != nil {
		return Task{}, err
	}
	if live, ok := c.liveTask(task.ID); ok {
		if err := live.process.Stop(taskStopGrace); err != nil {
			return Task{}, err
		}
	}
	return c.store.GetTask(ctx, task.ID)
}

// sessionTask is the task with the ID if it belongs to the session.
func (c *Core) sessionTask(ctx context.Context, sessionID string, taskID string) (Task, error) {
	task, err := c.store.GetTask(ctx, normalizeText(taskID))
	if errors.Is(err, ErrNotFound) || err == nil && task.SessionID != sessionID {
		return Task{}, fmt.Errorf("%w: no task %q in this session", ErrInvalidInput, taskID)
	}
	return task, err
}

// recoverShellTasks marks the shell tasks the previous daemon left running
// lost, and kills their process group when its leader is surely still theirs;
// runs waiting for them wake, and the session's next run reads them.
func (c *Core) recoverShellTasks(ctx context.Context) error {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{Kind: TaskKindShell, Statuses: []TaskStatus{TaskStatusRunning}})
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if _, live := c.liveTask(task.ID); live {
			continue
		}
		if err := shelltask.KillLeftover(shelltask.Leader{PID: task.PID, BootID: task.BootID, Start: task.LeaderStart}); err != nil {
			log.Printf("core: kill leftover task %q: %v", task.ID, err)
		}
		if _, _, err := c.recordTaskEnd(ctx, task.ID, TaskStatusLost, nil, "the daemon restarted while it ran and stopped it"); err != nil {
			return err
		}
	}
	return nil
}

// stopSessionTasks stops the shell tasks a session runs when nobody will read
// them; they are delivered first, so that their end wakes nothing in it.
func (c *Core) stopSessionTasks(ctx context.Context, sessionID string, reason string) error {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: sessionID, Kind: TaskKindShell, Statuses: []TaskStatus{TaskStatusRunning}})
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	if err := c.store.MarkTasksDelivered(ctx, ids, "", c.now().UTC()); err != nil {
		return err
	}
	for _, task := range tasks {
		if _, err := c.cancelTask(ctx, task, reason); err != nil {
			return err
		}
	}
	return nil
}

// stopRunCommands stops the background commands a canceled run started.
func (c *Core) stopRunCommands(ctx context.Context, run Run) error {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: run.SessionID, Kind: TaskKindShell, Statuses: []TaskStatus{TaskStatusRunning}})
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.RunID != run.ID {
			continue
		}
		if _, err := c.cancelTask(ctx, task, "its run was canceled"); err != nil {
			return err
		}
	}
	return nil
}

func (c *Core) taskOutputPath(sessionID string, taskID string) (string, error) {
	dir, err := c.sessionDir(sessionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, taskOutputDir, taskID+".log"), nil
}

func (c *Core) publishTaskUpdated(task Task) {
	c.publishEvent(Event{Type: EventTaskUpdated, SessionID: task.SessionID, RunID: task.RunID, Payload: task})
}

func taskInfo(task Task) tools.TaskInfo {
	return tools.TaskInfo{
		TaskID:      task.ID,
		Kind:        string(task.Kind),
		Command:     task.Command,
		Description: task.Description,
		Status:      string(task.Status),
		ExitCode:    task.ExitCode,
		OutputPath:  task.OutputPath,
	}
}

// taskFinished lets the task's session react: a parked run wakes, a running
// run reads the event at its next step, an idle session starts a run.
func (c *Core) taskFinished(ctx context.Context, task Task) {
	if !task.Background || task.DeliveredAt != nil {
		return
	}
	active, err := c.store.GetActiveRunBySession(ctx, task.SessionID)
	switch {
	case errors.Is(err, ErrNotFound):
		err = c.wakeSession(ctx, task.SessionID, &task)
	case err == nil && (active.Status == RunStatusWaitingEvents || active.Status == RunStatusWaitingApproval):
		err = c.wakeWaitingRun(ctx, task.SessionID, active.ID)
	}
	if err != nil {
		log.Printf("core: tell session %q that task %q finished: %v", task.SessionID, task.ID, err)
	}
}
