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
		failed, err := c.failAcceptedRun(ctx, *result, err)
		return errors.Join(err, c.noticeFailedWakeRun(ctx, failed.Run.ID))
	}
	return nil
}

// noticeFailedWakeRun tells the user when a run started for finished
// background work failed; the session then starts no other on its own until
// the user writes.
func (c *Core) noticeFailedWakeRun(ctx context.Context, runID string) error {
	run, err := c.store.GetRun(ctx, runID)
	if err != nil || run.Trigger != RunTriggerWake || run.Status != RunStatusFailed {
		return ignoreNotFound(err)
	}
	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		return ignoreNotFound(err)
	}
	text := fmt.Sprintf("Background work finished, but the run started for it failed (%s). This session waits for your message before it goes on.", firstNonEmpty(run.Error, "no reason given"))
	return c.sendWakeNotice(ctx, wakeNotice{session: session, last: run, text: text})
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
	events, err := c.taskEvents(ctx, session.ID)
	if err != nil {
		return nil, nil, err
	}
	wakes := false
	for _, task := range events {
		if wakes, err = c.taskWakesSession(ctx, task); err != nil || wakes {
			break
		}
	}
	if err != nil || !wakes {
		return nil, nil, err
	}
	runs, err := c.store.ListSessionRuns(ctx, session.ID, maxWakeChain+1)
	if err != nil {
		return nil, nil, err
	}
	if len(runs) > 0 && runs[0].Trigger == RunTriggerWake && (runs[0].Status == RunStatusFailed || runs[0].Status == RunStatusCanceled) {
		// The last run this session started on its own did not get through.
		return nil, nil, nil
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
		if finished == nil {
			return nil, nil, nil
		}
		if wakes, err := c.taskWakesSession(ctx, *finished); err != nil || !wakes {
			return nil, nil, err
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
	if err := c.journalWakeEvents(ctx, result.Run, events); err != nil {
		_, err = c.failAcceptedRun(ctx, result, err)
		return nil, nil, err
	}
	return &result, nil, nil
}

// journalWakeEvents writes the events a wake run is for after its message as
// engine notes and delivers them to it, before it starts, so that no run reads
// them again.
func (c *Core) journalWakeEvents(ctx context.Context, run Run, events []Task) error {
	for _, task := range events {
		text := taskEventText(task)
		now := c.now().UTC()
		note := transcript.Message{ID: c.newID("msg"), SessionID: run.SessionID, RunID: run.ID, Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: text, Parts: transcript.NormalizeMessageParts(text, nil), CreatedAt: now, UpdatedAt: now}
		if _, err := c.store.AppendMessage(ctx, note); err != nil {
			return err
		}
		if err := c.store.MarkTasksDelivered(ctx, []string{task.ID}, run.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// taskWakesSession reports whether a finished task starts a run: a command
// that ended by itself or a subagent, unless the run that started it was
// canceled; a stopped or lost command waits for the session's next run.
func (c *Core) taskWakesSession(ctx context.Context, task Task) (bool, error) {
	if task.Kind != TaskKindSubagent && task.Status != TaskStatusCompleted && task.Status != TaskStatusFailed {
		return false, nil
	}
	canceled, err := c.isRunCanceled(ctx, task.RunID)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	return !canceled, err
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

func ignoreNotFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
