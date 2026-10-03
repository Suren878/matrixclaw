package core

import (
	"cmp"
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

// noticeFailedWakeRun tells the user when a run started for finished
// background work failed; the session then starts no other on its own until
// the user writes.
func (c *Core) noticeFailedWakeRun(ctx context.Context, run Run) error {
	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		return ignoreNotFound(err)
	}
	target, _, err := c.wakeTargetOf(ctx, run)
	if err != nil {
		return err
	}
	text := fmt.Sprintf("Background work finished, but the run started for it failed (%s). This session waits for your message before it goes on.", cmp.Or(run.Error, "no reason given"))
	return c.sendWakeNotice(ctx, wakeNotice{session: session, target: target, text: text})
}

// wakeNotice tells the user that background work finished while the session
// may not wake itself.
type wakeNotice struct {
	session Session
	target  wakeTarget
	text    string
}

// wakeTarget is where a run or notice the session starts on its own goes; an
// empty one stays in the session.
type wakeTarget struct {
	client       string
	externalKey  string
	capabilities ClientCapabilities
	address      json.RawMessage
}

// wakeChainWindow is how many of a session's newest runs prepareWake reads.
const wakeChainWindow = 5 * maxWakeChain

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
	runs, err := c.store.ListSessionRuns(ctx, session.ID, wakeChainWindow)
	if err != nil {
		return nil, nil, err
	}
	chain, stopped, err := c.wakeChain(ctx, session.ID, runs)
	if err != nil || stopped {
		return nil, nil, err
	}
	target, err := c.latestWakeTarget(ctx, runs)
	if err != nil {
		return nil, nil, err
	}
	if chain >= maxWakeChain {
		if finished == nil {
			return nil, nil, nil
		}
		if wakes, err := c.taskWakesSession(ctx, *finished); err != nil || !wakes {
			return nil, nil, err
		}
		text := fmt.Sprintf("%s finished. This session has continued on its own %d times in a row, so it waits for your message before it goes on.", taskLabel(*finished), maxWakeChain)
		return nil, &wakeNotice{session: session, target: target, text: text}, nil
	}
	parts := transcript.NormalizeMessageParts(wakeRunText, nil)
	run := clientRun(wakeRunText, parts, target.client, target.externalKey, target.capabilities, deliveryTo{Address: target.address})
	run.Trigger = RunTriggerWake
	result, err := c.createAcceptedRun(ctx, session, run)
	if err != nil {
		return nil, nil, err
	}
	if err := c.journalWakeEvents(ctx, result.Run, events); err != nil {
		_, err = c.failAcceptedRun(ctx, result, err)
		return nil, nil, err
	}
	return &result, nil, nil
}

// wakeChain counts the newest wake runs in a row that took no message from
// the user; automation runs neither count nor end the chain. stopped reports
// that the newest of them failed or was canceled.
func (c *Core) wakeChain(ctx context.Context, sessionID string, runs []Run) (chain int, stopped bool, err error) {
	for _, run := range runs {
		if run.Trigger == RunTriggerAutomation {
			continue
		}
		if run.Trigger != RunTriggerWake {
			break
		}
		steered, err := c.store.HasConsumedSessionInput(ctx, sessionID, run.ID)
		if err != nil {
			return 0, false, err
		}
		if steered {
			break
		}
		if chain == 0 && (run.Status == RunStatusFailed || run.Status == RunStatusCanceled) {
			stopped = true
		}
		chain++
	}
	return chain, stopped, nil
}

// latestWakeTarget is the newest of the runs' targets that a later message
// still reaches.
func (c *Core) latestWakeTarget(ctx context.Context, runs []Run) (wakeTarget, error) {
	for _, run := range runs {
		target, ok, err := c.wakeTargetOf(ctx, run)
		if err != nil || ok {
			return target, err
		}
	}
	return wakeTarget{}, nil
}

// wakeTargetOf is where the run's reply was delivered, unless a later message
// cannot go there (a reply-once address).
func (c *Core) wakeTargetOf(ctx context.Context, run Run) (wakeTarget, bool, error) {
	if run.Client == "" || run.ExternalKey == "" {
		return wakeTarget{}, false, nil
	}
	deliveries, err := c.store.ListClientDeliveries(ctx, ClientDeliveryFilter{RunID: run.ID, Type: ClientDeliveryTypeRun, Limit: 1})
	if err != nil {
		return wakeTarget{}, false, err
	}
	var address json.RawMessage
	if len(deliveries) > 0 {
		if deliveries[0].ReplyOnce {
			return wakeTarget{}, false, nil
		}
		address = deliveries[0].Address
	}
	return wakeTarget{client: run.Client, externalKey: run.ExternalKey, capabilities: run.ClientCapabilities, address: address}, true, nil
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
	if task.RunID == "" {
		return true, nil
	}
	run, err := c.store.GetRun(ctx, task.RunID)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	return run.Status != RunStatusCanceled, err
}

func taskLabel(task Task) string {
	if task.Kind == TaskKindSubagent {
		return "Subagent " + cmp.Or(task.AgentName, task.ID)
	}
	return "Background task " + task.ID + " (" + truncateForTitle(task.Command, 80) + ")"
}

// sendWakeNotice shows the notice in the session and sends it to its target.
func (c *Core) sendWakeNotice(ctx context.Context, notice wakeNotice) error {
	if _, err := c.CreateSystemMessage(ctx, notice.session.ID, notice.text); err != nil {
		return err
	}
	if notice.target.client == "" {
		return nil
	}
	_, err := c.CreateClientDelivery(ctx, ClientDelivery{
		Type:        ClientDeliveryTypeNotice,
		Client:      notice.target.client,
		ExternalKey: notice.target.externalKey,
		SessionID:   notice.session.ID,
		Summary:     notice.text,
		Address:     notice.target.address,
	})
	return err
}

func ignoreNotFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
