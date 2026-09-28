package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const awaitToolName = "await"

// An await waits 10 minutes unless the call names up to an hour.
const (
	defaultAwaitTimeout = 10 * time.Minute
	maxAwaitTimeout     = time.Hour
)

type awaitInput struct {
	IDs            []string `json:"ids,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

type awaitTool struct {
	app *Core
}

// AwaitToolExecutors returns the await tool.
func AwaitToolExecutors(app *Core) []tools.Executor {
	return []tools.Executor{&awaitTool{app: app}}
}

func (t *awaitTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              awaitToolName,
		Name:            "Await",
		Description:     "Wait for background tasks to finish instead of polling them. Ends your turn; you are woken when one of the tasks finishes, the user writes, or the timeout passes.",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "core.await",
		Category:        tools.CategoryAutomation,
		Profiles:        []tools.Profile{tools.ProfileCoding},
		OutputKind:      tools.OutputText,
		InputJSONSchema: awaitToolSchema,
	}
}

// Execute checks what the call waits for and asks the engine to wait; tasks
// that already finished are reported at once instead.
func (t *awaitTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input awaitInput
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &input); err != nil {
			return tools.Result{}, tools.InvalidArgs(awaitToolName, err)
		}
	}
	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	switch {
	case strings.TrimSpace(call.RunID) == "":
		return awaitError("await waits inside a run only."), nil
	case input.TimeoutSeconds < 0 || timeout > maxAwaitTimeout:
		return awaitError(fmt.Sprintf("timeout_seconds is at most %d.", int(maxAwaitTimeout/time.Second))), nil
	case timeout == 0:
		timeout = defaultAwaitTimeout
	}
	waiting, finished, err := t.app.awaitedTasks(ctx, call.SessionID, input.IDs)
	if err != nil {
		return tools.Result{}, err
	}
	if len(waiting) == 0 {
		if len(finished) == 0 {
			return tools.Result{Content: "No background task is running; there is nothing to wait for.", Status: tools.ResultStatusNeutral}, nil
		}
		return tools.Result{Content: "Already finished: " + strings.Join(finished, "; ") + ".", Status: tools.ResultStatusNeutral}, nil
	}
	await := &tools.Await{Until: t.app.now().UTC().Add(timeout)}
	if len(input.IDs) > 0 {
		await.TaskIDs = waiting
	}
	content := fmt.Sprintf("Waiting up to %s for %s. You are woken when one of them finishes, the user writes, or the time is up.", timeout, strings.Join(waiting, ", "))
	return tools.Result{Content: content, Status: tools.ResultStatusNeutral, Await: await}, nil
}

// awaitedTasks sorts the tasks an await names into those still running and
// those that finished (with their status); no IDs means every running
// background task of the session.
func (c *Core) awaitedTasks(ctx context.Context, sessionID string, ids []string) (waiting []string, finished []string, err error) {
	if len(ids) == 0 {
		tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: sessionID, Statuses: []TaskStatus{TaskStatusPending, TaskStatusRunning, TaskStatusWaitingApproval}, Background: true})
		if err != nil {
			return nil, nil, err
		}
		for _, task := range tasks {
			waiting = append(waiting, task.ID)
		}
		return waiting, nil, nil
	}
	for _, id := range ids {
		task, err := c.sessionTask(ctx, sessionID, id)
		if err != nil {
			return nil, nil, err
		}
		if task.FinishedAt == nil {
			waiting = append(waiting, task.ID)
			continue
		}
		status := string(task.Status)
		if task.ExitCode != nil {
			status += fmt.Sprintf(", exit code %d", *task.ExitCode)
		}
		finished = append(finished, task.ID+" "+status)
	}
	return waiting, finished, nil
}

func awaitError(text string) tools.Result {
	return tools.Result{Content: text, Status: tools.ResultStatusError, IsError: true}
}

var awaitToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "ids": {"type": "array", "items": {"type": "string"}, "description": "Background task ids to wait for; none waits for any running background task."},
    "timeout_seconds": {"type": "integer", "minimum": 0, "maximum": 3600, "description": "How long to wait at most; default 600."}
  },
  "additionalProperties": false
}`)
