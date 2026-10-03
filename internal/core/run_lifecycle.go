package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// runEdges are the status changes a run may make besides ending as failed or
// canceled, which every run that has not ended may do. An interrupted run stays
// running until it executes again.
var runEdges = map[RunStatus][]RunStatus{
	RunStatusAccepted:        {RunStatusRunning},
	RunStatusRunning:         {RunStatusWaitingApproval, RunStatusWaitingEvents, RunStatusCompleted},
	RunStatusWaitingApproval: {RunStatusRunning},
	RunStatusWaitingEvents:   {RunStatusRunning},
}

// canBecome reports whether a run in s may move to status to.
func (s RunStatus) canBecome(to RunStatus) bool {
	if s.Terminal() {
		return false
	}
	return to == RunStatusFailed || to == RunStatusCanceled || slices.Contains(runEdges[s], to)
}

// runChange is one status change of a run: Err for an ended run, Stop for a
// completed or failed one, Await for one parked in waiting_events, and the reply
// sealed together with the change.
type runChange struct {
	To         RunStatus
	Err        string
	Stop       agent.StopReason
	Await      *tools.Await
	Reply      *transcript.Message
	ReplySaved bool
}

// transition is the only writer of a run's status. It checks the edge, keeps
// FinishedAt, the wakeup of a parked run and the checkpoint, seals the reply in
// the same write, rejects the approvals an ended run leaves and publishes the
// change. A run that ended meanwhile stays ended (ErrRunEnded).
func (c *Core) transition(ctx context.Context, run *Run, change runChange) error {
	if !run.Status.canBecome(change.To) {
		return fmt.Errorf("core: run %s cannot go from %s to %s", run.ID, run.Status, change.To)
	}
	from := run.Status
	now := c.now().UTC()
	next := *run
	next.Status, next.Error, next.UpdatedAt, next.FinishedAt = change.To, change.Err, now, nil
	if change.To == RunStatusCompleted || change.To == RunStatusFailed {
		next.StopReason = change.Stop
	}
	if change.To.Terminal() {
		next.FinishedAt = &now
	}
	if change.To == RunStatusWaitingEvents {
		if change.Await == nil {
			return errors.New("core: a run waiting for events has nothing to wait for")
		}
		wakeup := RunWakeup{RunID: run.ID, SessionID: run.SessionID, WakeAt: change.Await.Until, TaskIDs: change.Await.TaskIDs}
		if err := c.store.SaveRunWakeup(ctx, wakeup); err != nil {
			return err
		}
	}
	reply, created := c.stampReply(change.Reply, change.ReplySaved, now)
	if err := c.store.SealRun(ctx, next, reply, change.ReplySaved); err != nil {
		if change.To == RunStatusWaitingEvents && from != RunStatusWaitingEvents {
			_ = c.store.DeleteRunWakeup(ctx, run.ID)
		}
		return err
	}
	*run = next
	if from == RunStatusWaitingEvents && change.To != RunStatusWaitingEvents {
		if err := c.store.DeleteRunWakeup(ctx, run.ID); err != nil {
			return err
		}
	}
	if reply != nil {
		c.publishEvent(Event{Type: replyEvent(created), SessionID: reply.SessionID, RunID: reply.RunID, Payload: *reply})
	}
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: run.SessionID, RunID: run.ID, Payload: *run})
	if !change.To.Terminal() {
		_, err := c.syncSubagentTask(ctx, *run)
		return err
	}
	c.clearRunCheckpoint(ctx, run.ID)
	if err := c.rejectRunApprovals(ctx, *run, change.Err); err != nil {
		return err
	}
	_, err := c.syncSubagentTask(ctx, *run)
	c.notifyRunEnd(run.ID)
	if err != nil {
		return err
	}
	if change.To == RunStatusFailed {
		// A blocking child works for a call the failed run no longer waits for.
		stopped, err := c.cancelSubagentChildren(ctx, *run, true)
		for _, id := range stopped {
			c.cancelActiveRun(id)
		}
		if err != nil {
			return err
		}
	}
	if change.To == RunStatusFailed && run.Trigger == RunTriggerWake {
		return c.noticeFailedWakeRun(ctx, *run)
	}
	return nil
}

// stampReply sets the reply's times and reports whether it is new.
func (c *Core) stampReply(reply *transcript.Message, saved bool, now time.Time) (*transcript.Message, bool) {
	if reply == nil {
		return nil, false
	}
	if reply.CreatedAt.IsZero() {
		reply.CreatedAt = now
	}
	reply.UpdatedAt = now
	return reply, !saved
}

func replyEvent(created bool) EventType {
	if created {
		return EventMessageCreated
	}
	return EventMessageUpdated
}

// sealReply saves a reply without changing its run's status: created when it
// was not saved yet, updated otherwise.
func (c *Core) sealReply(ctx context.Context, reply *transcript.Message, saved bool) error {
	reply, created := c.stampReply(reply, saved, c.now().UTC())
	var err error
	if created {
		_, err = c.store.AppendMessage(ctx, *reply)
	} else {
		err = c.store.UpdateMessage(ctx, *reply)
	}
	if err != nil {
		return err
	}
	c.publishEvent(Event{Type: replyEvent(created), SessionID: reply.SessionID, RunID: reply.RunID, Payload: *reply})
	return nil
}

// withFinishPart is the reply's text followed by a finish part.
func withFinishPart(content string, reason string, message string) []transcript.MessagePart {
	return append(transcript.NormalizeMessageParts(content, nil), transcript.MessagePart{
		Kind:   transcript.MessagePartKindFinish,
		Finish: &transcript.FinishPart{Reason: reason, Message: message},
	})
}

// failRun ends the run as failed and returns cause.
func (c *Core) failRun(ctx context.Context, run Run, cause error) error {
	if err := c.transition(ctx, &run, runChange{To: RunStatusFailed, Err: cause.Error()}); err != nil {
		return err
	}
	return cause
}

// failWithReply ends the run as failed with its reply sealed with the error.
func (c *Core) failWithReply(ctx context.Context, run Run, reply *transcript.Message, saved bool, stop agent.StopReason, cause error) error {
	reply.Parts = withFinishPart(reply.Content, transcript.FinishReasonError, cause.Error())
	if err := c.transition(ctx, &run, runChange{To: RunStatusFailed, Err: cause.Error(), Stop: stop, Reply: reply, ReplySaved: saved}); err != nil {
		return err
	}
	return cause
}

// failAcceptedRun ends a run that could not be started.
func (c *Core) failAcceptedRun(ctx context.Context, result AcceptRunResult, cause error) (AcceptRunResult, error) {
	run := result.Run
	if err := c.transition(ctx, &run, runChange{To: RunStatusFailed, Err: cause.Error()}); err != nil {
		return result, err
	}
	result.Run = run
	return result, cause
}

// sealCanceledReply marks the reply of a run the user canceled.
func (c *Core) sealCanceledReply(ctx context.Context, reply *transcript.Message, saved bool) error {
	if reply == nil {
		return nil
	}
	reply.Parts = withFinishPart(reply.Content, transcript.FinishReasonCanceled, "Canceled by user.")
	return c.sealReply(ctx, reply, saved)
}

// applyOutcome persists how the engine left a native run and reports whether
// the run was kept for recovery. A run canceled while its engine finished stays
// canceled, with its reply marked so.
func (c *Core) applyOutcome(ctx context.Context, run Run, outcome agent.Outcome) (bool, error) {
	kept, err := c.writeOutcome(ctx, run, outcome)
	if !errors.Is(err, ErrRunEnded) {
		return kept, err
	}
	latest, err := c.store.GetRun(ctx, run.ID)
	if err != nil || latest.Status != RunStatusCanceled {
		return false, err
	}
	return false, c.sealCanceledReply(ctx, outcome.Assistant, outcome.AssistantSaved)
}

func (c *Core) writeOutcome(ctx context.Context, run Run, outcome agent.Outcome) (bool, error) {
	switch outcome.Status {
	case agent.StatusCompleted:
		if outcome.Assistant == nil {
			return false, nil
		}
		return false, c.transition(ctx, &run, runChange{To: RunStatusCompleted, Stop: outcome.StopReason, Reply: outcome.Assistant, ReplySaved: outcome.AssistantSaved})
	case agent.StatusWaitingApproval:
		return false, c.transition(ctx, &run, runChange{To: RunStatusWaitingApproval})
	case agent.StatusWaitingEvents:
		return false, c.transition(ctx, &run, runChange{To: RunStatusWaitingEvents, Await: outcome.Counters.Await})
	case agent.StatusCanceled:
		return false, c.sealCanceledReply(ctx, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusFailed:
		if outcome.MarkErrored && outcome.Assistant != nil {
			return false, c.failWithReply(ctx, run, outcome.Assistant, outcome.AssistantSaved, outcome.StopReason, outcome.Err)
		}
		if err := c.transition(ctx, &run, runChange{To: RunStatusFailed, Err: outcome.Err.Error(), Stop: outcome.StopReason}); err != nil {
			return false, err
		}
		return false, outcome.Err
	case agent.StatusInterrupted:
		return c.applyInterruptedOutcome(run, outcome)
	default:
		return false, fmt.Errorf("core: unknown run outcome %q", outcome.Status)
	}
}

// applyInterruptedOutcome commits what the run reached before its context
// stopped, or leaves it running, its reply sealed, for recovery (reported as true).
func (c *Core) applyInterruptedOutcome(run Run, outcome agent.Outcome) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runInterruptionPersistenceTimeout)
	defer cancel()
	latest, err := c.store.GetRun(ctx, run.ID)
	if err != nil {
		return false, err
	}
	if latest.Status == RunStatusCanceled {
		return false, c.sealCanceledReply(ctx, outcome.Assistant, outcome.AssistantSaved)
	}
	if latest.Status.Terminal() {
		return false, nil
	}
	switch outcome.Reached {
	case agent.StatusCompleted:
		return false, c.transition(ctx, &latest, runChange{To: RunStatusCompleted, Stop: outcome.StopReason, Reply: outcome.Assistant, ReplySaved: outcome.AssistantSaved})
	case agent.StatusWaitingApproval:
		pending, err := c.runHasPendingApprovals(ctx, latest.SessionID, latest.ID)
		if err != nil {
			return false, err
		}
		if pending {
			return false, c.transition(ctx, &latest, runChange{To: RunStatusWaitingApproval})
		}
	case agent.StatusWaitingEvents:
		return false, c.transition(ctx, &latest, runChange{To: RunStatusWaitingEvents, Await: outcome.Counters.Await})
	}
	return true, c.sealInterruptedReply(ctx, outcome.Assistant, outcome.AssistantSaved)
}

// afterRun does what follows a run that no longer executes: an ended run
// starts the session's next queued message or wakes the session for finished
// background work; a parked run starts when what it waits for arrived while it
// parked. The executor's release runs it, and CancelRun for a run without one.
// Every step checks its own state under the session gate, so a repeat is harmless.
func (c *Core) afterRun(ctx context.Context, runID string) {
	if c.lifetime.Err() != nil {
		return
	}
	run, err := c.store.GetRun(ctx, runID)
	if err == nil {
		switch {
		case run.Status.Terminal():
			err = c.afterEnd(ctx, run)
		case run.Status == RunStatusWaitingApproval:
			err = c.resumeDecidedRun(ctx, run.SessionID, run.ID)
		case run.Status == RunStatusWaitingEvents:
			err = c.wakeWaitingRun(ctx, run.SessionID, run.ID)
		}
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		log.Printf("core: after run %q: %v", runID, err)
	}
}

// afterEnd lets the session of an ended run go on: a subagent's commands stop,
// steers that missed the run are queued, and the next queued message starts,
// or else the session wakes for background work that finished.
func (c *Core) afterEnd(ctx context.Context, run Run) error {
	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		return err
	}
	if isSubagentSession(session) {
		// Nobody reads a child's background commands once it has finished.
		if err := c.stopSessionTasks(ctx, session.ID, "its subagent finished"); err != nil {
			return err
		}
	}
	if err := c.queuePendingSteersForRun(ctx, session.ID, run.ID); err != nil {
		return err
	}
	started, err := c.startNextPendingSessionInput(ctx, session.ID)
	if err != nil || started {
		return err
	}
	return c.wakeSession(ctx, session.ID, nil)
}
