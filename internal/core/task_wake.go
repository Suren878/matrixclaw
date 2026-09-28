package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// maxWakeChain is how many runs in a row a session starts for finished
// background work without a user message; after that it only tells the user.
const maxWakeChain = 20

// wakeRunText is the user message of a run started for finished background work;
// the tasks' results follow it as engine notes.
const wakeRunText = "Background work finished while you were idle; its results follow. Carry on with what it was for, or report what happened."

// wakeSession starts a run in an idle session for background tasks that
// finished unseen, delivered where the user last wrote from. Past the wake
// chain limit it only tells the user about finished, when given.
func (c *Core) wakeSession(ctx context.Context, sessionID string, finished *Task) error {
	gate := c.sessionGate(sessionID)
	gate.Lock()
	result, notice, err := c.prepareWake(ctx, sessionID, finished)
	gate.Unlock()
	switch {
	case err != nil:
		return err
	case notice != nil:
		return c.sendWakeNotice(ctx, *notice)
	case result == nil:
		return nil
	}
	if err := c.startRun(ctx, result.Run.ID); err != nil {
		_, err = c.failAcceptedRun(ctx, *result, err)
		return err
	}
	return nil
}

// wakeNotice tells the user that background work finished while the session
// may not wake itself.
type wakeNotice struct {
	session Session
	last    Run
	text    string
}

// prepareWake creates the wake run under the session gate: only for an idle,
// top-level session with a task whose end should wake it.
func (c *Core) prepareWake(ctx context.Context, sessionID string, finished *Task) (*AcceptRunResult, *wakeNotice, error) {
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil || isSubagentSession(session) {
		return nil, nil, err
	}
	if _, err := c.store.GetActiveRunBySession(ctx, session.ID); !errors.Is(err, ErrNotFound) {
		return nil, nil, err
	}
	inputs, err := c.store.ListPendingSessionInputs(ctx, session.ID)
	if err != nil || len(inputs) > 0 {
		return nil, nil, err
	}
	events, err := c.store.ListTasks(ctx, TaskFilter{SessionID: session.ID, Undelivered: true})
	if err != nil {
		return nil, nil, err
	}
	wakes := false
	for _, task := range events {
		wakes = wakes || taskWakesSession(task)
	}
	if !wakes {
		return nil, nil, nil
	}
	runs, err := c.store.ListSessionRuns(ctx, session.ID, maxWakeChain+1)
	if err != nil {
		return nil, nil, err
	}
	chain := 0
	for chain < len(runs) && runs[chain].Trigger == RunTriggerWake {
		chain++
	}
	var last Run
	if chain < len(runs) {
		last = runs[chain]
	}
	if chain >= maxWakeChain {
		if finished == nil || !taskWakesSession(*finished) {
			return nil, nil, nil
		}
		text := fmt.Sprintf("%s finished. This session has continued on its own %d times in a row, so it waits for your message before it goes on.", taskLabel(*finished), maxWakeChain)
		return nil, &wakeNotice{session: session, last: last, text: text}, nil
	}
	address, err := c.runDeliveryAddress(ctx, last)
	if err != nil {
		return nil, nil, err
	}
	parts := transcript.NormalizeMessageParts(wakeRunText, nil)
	result, err := c.createAcceptedRun(ctx, session, wakeRunText, parts, last.Client, last.ExternalKey, last.ClientCapabilities, address, "", RunTriggerWake)
	if err != nil {
		return nil, nil, err
	}
	return &result, nil, nil
}

// taskWakesSession reports whether a finished task starts a run: a command
// that ended by itself or a subagent; a stopped or lost command waits for the
// session's next run.
func taskWakesSession(task Task) bool {
	if task.Kind == TaskKindSubagent {
		return true
	}
	return task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed
}

func taskLabel(task Task) string {
	if task.Kind == TaskKindSubagent {
		return "Subagent " + firstNonEmpty(task.AgentName, task.ID)
	}
	return "Background task " + task.ID + " (" + truncateForTitle(task.Command, 80) + ")"
}

// runDeliveryAddress is where the run's reply was delivered; none for a run
// without a client delivery.
func (c *Core) runDeliveryAddress(ctx context.Context, run Run) (json.RawMessage, error) {
	if run.ID == "" || run.Client == "" || run.ExternalKey == "" {
		return nil, nil
	}
	deliveries, err := c.store.ListClientDeliveries(ctx, ClientDeliveryFilter{RunID: run.ID, Type: ClientDeliveryTypeRun, Limit: 1})
	if err != nil || len(deliveries) == 0 {
		return nil, err
	}
	return deliveries[0].Address, nil
}

// sendWakeNotice shows the notice in the session and sends it where the user
// last wrote from.
func (c *Core) sendWakeNotice(ctx context.Context, notice wakeNotice) error {
	if _, err := c.CreateSystemMessage(ctx, notice.session.ID, notice.text); err != nil {
		return err
	}
	if notice.last.Client == "" || notice.last.ExternalKey == "" {
		return nil
	}
	address, err := c.runDeliveryAddress(ctx, notice.last)
	if err != nil {
		return err
	}
	_, err = c.CreateClientDelivery(ctx, ClientDelivery{
		Type:        ClientDeliveryTypeNotice,
		Client:      notice.last.Client,
		ExternalKey: notice.last.ExternalKey,
		SessionID:   notice.session.ID,
		Summary:     notice.text,
		Address:     address,
	})
	return err
}

// RecoverTaskEvents starts the runs that idle sessions owe to background work
// finished before the daemon restarted.
func (c *Core) RecoverTaskEvents(ctx context.Context) error {
	events, err := c.store.ListTasks(ctx, TaskFilter{Undelivered: true})
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var errs []error
	for _, task := range events {
		if seen[task.SessionID] {
			continue
		}
		seen[task.SessionID] = true
		errs = append(errs, c.wakeSession(ctx, task.SessionID, nil))
	}
	return errors.Join(errs...)
}
