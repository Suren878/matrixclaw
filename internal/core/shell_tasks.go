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

// foregroundWaitDelay is how long a finished foreground command waits for
// processes it left behind to close their output.
const foregroundWaitDelay = 2 * time.Second

// RunCommand runs a bash call's command. A background command, or a foreground
// one still running after its AutoBackground delay, goes on as a task of the
// call's session; a foreground one is killed at its timeout or when ctx stops.
func (c *Core) RunCommand(ctx context.Context, call tools.Call, command tools.Command) (tools.CommandResult, error) {
	id := c.newID("task")
	path, err := c.taskOutputPath(call.SessionID, id)
	if err != nil {
		return tools.CommandResult{}, err
	}
	out, err := shelltask.CreateOutput(path)
	if err != nil {
		return tools.CommandResult{}, err
	}
	waitDelay := foregroundWaitDelay
	if command.Background {
		waitDelay = 0
	}
	process, err := shelltask.Start(command.Command, command.WorkingDir, out, waitDelay)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(path)
		return tools.CommandResult{}, err
	}
	if command.Background {
		return c.adoptCommand(ctx, call, command, id, process, time.Time{})
	}
	timeout := time.NewTimer(command.Timeout)
	defer timeout.Stop()
	var background <-chan time.Time
	if command.AutoBackground > 0 && command.AutoBackground < command.Timeout {
		timer := time.NewTimer(command.AutoBackground)
		defer timer.Stop()
		background = timer.C
	}
	select {
	case <-process.Done():
		return c.finishedCommand(process, command, false)
	case <-timeout.C:
		_ = process.Kill()
		<-process.Done()
		return c.finishedCommand(process, command, true)
	case <-background:
		return c.adoptCommand(ctx, call, command, id, process, process.StartedAt().Add(command.Timeout))
	case <-ctx.Done():
		_ = process.Kill()
		<-process.Done()
		_ = os.Remove(path)
		return tools.CommandResult{}, ctx.Err()
	}
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
// until it ends or, with a deadline, is killed then.
func (c *Core) adoptCommand(ctx context.Context, call tools.Call, command tools.Command, id string, process *shelltask.Process, deadline time.Time) (tools.CommandResult, error) {
	now := c.now().UTC()
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
		PID:              process.PID(),
		PGID:             process.PID(),
		OutputPath:       process.Output().Path(),
		StartedAt:        process.StartedAt().UTC(),
		UpdatedAt:        now,
	}
	live := &liveTask{process: process, recorded: make(chan struct{})}
	c.tasksMu.Lock()
	c.liveTasks[id] = live
	c.tasksMu.Unlock()
	if err := c.store.CreateTask(context.WithoutCancel(ctx), task); err != nil {
		c.forgetLiveTask(id)
		_ = process.Kill()
		return tools.CommandResult{}, err
	}
	c.publishTaskUpdated(task)
	safego.Go("core.watchShellTask", func() { c.watchShellTask(task, live, deadline) })
	return tools.CommandResult{TaskID: id, StartedAt: process.StartedAt(), EndedAt: now}, nil
}

// liveTask is a shell task of this daemon; recorded is closed once its end is stored.
type liveTask struct {
	process  *shelltask.Process
	recorded chan struct{}
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
			_ = process.Kill()
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
	finished, err := c.store.FinishTask(ctx, taskID, status, exitCode, errText, c.now().UTC())
	if err != nil || !finished {
		return err
	}
	task, err := c.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	c.publishTaskUpdated(task)
	return nil
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
	if live, ok := c.liveTask(task.ID); ok && read.Wait > 0 {
		timer := time.NewTimer(read.Wait)
		select {
		case <-live.recorded:
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
		if err := ctx.Err(); err != nil {
			return tools.TaskOutput{}, err
		}
		if task, err = c.store.GetTask(ctx, task.ID); err != nil {
			return tools.TaskOutput{}, err
		}
	}
	if task.FinishedAt != nil {
		// The model reads how the task ended here and needs no event about it.
		if err := c.store.MarkTasksDelivered(ctx, []string{task.ID}, call.RunID, c.now().UTC()); err != nil {
			return tools.TaskOutput{}, err
		}
	}
	out := tools.TaskOutput{TaskInfo: taskInfo(task)}
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

// cancelTask ends a running task: a shell task's process group is killed, a
// subagent's run is canceled. A finished task is returned as it is.
func (c *Core) cancelTask(ctx context.Context, task Task, reason string) (Task, error) {
	if taskStatusTerminal(task.Status) {
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
		if err := live.process.Kill(); err != nil {
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

// RecoverTasks runs at daemon start: shell tasks left running by the previous
// daemon have their process group killed and are marked lost.
func (c *Core) RecoverTasks(ctx context.Context) error {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{Kind: TaskKindShell, Statuses: []TaskStatus{TaskStatusRunning}})
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if _, live := c.liveTask(task.ID); live {
			continue
		}
		if err := shelltask.KillLeftover(task.PID, task.PGID, task.StartedAt); err != nil {
			log.Printf("core: kill leftover task %q: %v", task.ID, err)
		}
		if err := c.finishTask(ctx, task.ID, TaskStatusLost, nil, "the daemon restarted while it ran and stopped it"); err != nil {
			return err
		}
	}
	return nil
}

// stopSessionTasks kills the shell tasks a session runs, before it is deleted.
func (c *Core) stopSessionTasks(ctx context.Context, sessionID string) error {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: sessionID, Kind: TaskKindShell, Statuses: []TaskStatus{TaskStatusRunning}})
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if _, err := c.cancelTask(ctx, task, "its session was deleted"); err != nil {
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
