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

// transition is the only writer of a run's status. It commits the change
// (edge check, FinishedAt, the wakeup of a parked run, the reply sealed in the
// same write) and then does what follows it. A run that ended meanwhile stays
// ended (ErrRunEnded). Only a failed commit is returned: once the status is
// written, every follow-up step runs and their errors are logged.
func (c *Core) transition(ctx context.Context, run *Run, change runChange) error {
	from := run.Status
	if change.To.Terminal() {
		// Callers waiting for the end wait for its follow-up too.
		c.beginRunEnd(run.ID)
		defer c.endRunEnd(run.ID)
	}
	reply, created, err := c.commitRun(ctx, run, change)
	if err != nil {
		return err
	}
	if err := c.afterCommit(ctx, *run, from, change, reply, created); err != nil {
		log.Printf("core: after run %s became %s: %v", run.ID, run.Status, err)
	}
	return nil
}

// commitRun writes the change and, on success, applies it to run; it returns
// the sealed reply and whether it is new.
func (c *Core) commitRun(ctx context.Context, run *Run, change runChange) (*transcript.Message, bool, error) {
	if !run.Status.canBecome(change.To) {
		return nil, false, fmt.Errorf("core: run %s cannot go from %s to %s", run.ID, run.Status, change.To)
	}
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
			return nil, false, errors.New("core: a run waiting for events has nothing to wait for")
		}
		wakeup := RunWakeup{RunID: run.ID, SessionID: run.SessionID, WakeAt: change.Await.Until, TaskIDs: change.Await.TaskIDs}
		if err := c.store.SaveRunWakeup(ctx, wakeup); err != nil {
			return nil, false, err
		}
	}
	reply, created := c.stampReply(change.Reply, change.ReplySaved, now)
	if err := c.store.SealRun(ctx, next, reply, change.ReplySaved); err != nil {
		if change.To == RunStatusWaitingEvents && run.Status != RunStatusWaitingEvents {
			if delErr := c.store.DeleteRunWakeup(ctx, run.ID); delErr != nil {
				err = errors.Join(err, fmt.Errorf("drop the wakeup: %w", delErr))
			}
		}
		return nil, false, err
	}
	*run = next
	return reply, created, nil
}

// afterCommit does what follows a committed change: it drops a wakeup the run
// no longer has, publishes the change, keeps the subagent task in step and,
// for an ended run, clears its checkpoint, rejects the approvals it leaves,
// stops the blocking children of a failed run and tells the user of a failed
// wake run.
//
// Session gates never nest. Ending a subagent's run takes its parent session's
// gate (taskFinished wakes the parent), so no subagent run ends under a gate.
func (c *Core) afterCommit(ctx context.Context, run Run, from RunStatus, change runChange, reply *transcript.Message, created bool) error {
	ended := run.Status.Terminal()
	var errs []error
	// An ended run may have parked after the caller read it.
	if run.Status != RunStatusWaitingEvents && (from == RunStatusWaitingEvents || ended) {
		if err := c.store.DeleteRunWakeup(ctx, run.ID); err != nil {
			errs = append(errs, fmt.Errorf("drop the wakeup: %w", err))
		}
	}
	if reply != nil {
		c.publishEvent(Event{Type: replyEvent(created), SessionID: reply.SessionID, RunID: reply.RunID, Payload: *reply})
	}
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: run.SessionID, RunID: run.ID, Payload: run})
	if ended {
		if err := c.store.DeleteRunCheckpoint(ctx, run.ID); err != nil {
			errs = append(errs, fmt.Errorf("clear the checkpoint: %w", err))
		}
		if err := c.rejectRunApprovals(ctx, run, change.Err); err != nil {
			errs = append(errs, fmt.Errorf("reject open approvals: %w", err))
		}
	}
	if _, err := c.syncSubagentTask(ctx, run); err != nil {
		errs = append(errs, fmt.Errorf("sync the subagent task: %w", err))
	}
	if run.Status == RunStatusFailed {
		// A blocking child works for a call the failed run no longer waits for.
		stopped, err := c.cancelSubagentChildren(ctx, run, true)
		for _, id := range stopped {
			c.cancelActiveRun(id)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("cancel blocking subagents: %w", err))
		}
		if run.Trigger == RunTriggerWake {
			if err := c.noticeFailedWakeRun(ctx, run); err != nil {
				errs = append(errs, fmt.Errorf("notice the failed wake run: %w", err))
			}
		}
	}
	return errors.Join(errs...)
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
// the run was kept for recovery. An interrupted run commits what it reached,
// in a context of its own as the run's has stopped. A run canceled while its
// engine finished stays canceled, with its reply marked so.
func (c *Core) applyOutcome(ctx context.Context, run Run, outcome agent.Outcome) (bool, error) {
	if outcome.Interrupted {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), runInterruptionPersistenceTimeout)
		defer cancel()
	}
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
		return false, c.transition(ctx, &run, runChange{To: RunStatusCompleted, Stop: outcome.StopReason, Reply: outcome.Assistant, ReplySaved: outcome.AssistantSaved})
	case agent.StatusWaitingApproval:
		if outcome.Interrupted {
			// A decision may have come in after the engine asked.
			pending, err := c.runHasPendingApprovals(ctx, run.SessionID, run.ID)
			if err != nil {
				return false, err
			}
			if !pending {
				return c.keepForRecovery(ctx, run, outcome)
			}
		}
		return false, c.transition(ctx, &run, runChange{To: RunStatusWaitingApproval})
	case agent.StatusWaitingEvents:
		return false, c.transition(ctx, &run, runChange{To: RunStatusWaitingEvents, Await: outcome.RunState.Await})
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
	case "":
		if outcome.Interrupted {
			return c.keepForRecovery(ctx, run, outcome)
		}
	}
	return false, fmt.Errorf("core: unknown run outcome %q", outcome.Status)
}

// keepForRecovery leaves an interrupted run running, its reply sealed, for
// recovery (reported as true), unless it ended meanwhile.
func (c *Core) keepForRecovery(ctx context.Context, run Run, outcome agent.Outcome) (bool, error) {
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
// steers that missed the run are queued, the run counts against the session
// goal, and the next queued message starts, or else the session wakes for
// background work that finished, or else the goal's next run starts.
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
	if err := c.settleGoalRun(ctx, run); err != nil {
		return err
	}
	started, err := c.startNextPendingSessionInput(ctx, session.ID)
	if err != nil || started {
		return err
	}
	if err := c.wakeSession(ctx, session.ID, nil); err != nil {
		return err
	}
	return c.continueGoal(ctx, session.ID)
}
