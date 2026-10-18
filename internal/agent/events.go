package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// drainEvents journals what happened outside the run since its last step, such
// as background tasks that finished, as engine notes, consuming each event as
// soon as its note is journaled.
func (r *run) drainEvents(ctx context.Context) error {
	events, err := r.Inbox.Events(ctx)
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := r.appendEngineMessage(ctx, transcript.OriginEngine, event.Text); err != nil {
			return err
		}
		if err := r.Inbox.ConsumeEvents(context.WithoutCancel(ctx), []string{event.TaskID}); err != nil {
			return err
		}
	}
	return nil
}

// AwaitWake is why a run parked by await goes on; AwaitParked keeps it parked.
type AwaitWake int

const (
	AwaitParked AwaitWake = iota
	AwaitSteered
	AwaitEnded
	AwaitTimedOut
)

// AwaitSignals is what a run parked by await may wake for: the user wrote to
// it, a task the await names ended, a finished task's event is pending.
type AwaitSignals struct {
	Steered bool
	Ended   bool
	Events  bool
}

// AwaitOver says whether and why a run parked by await goes on at now: the
// user wrote, a task it waits for ended (any task's event, for an await that
// names none), or its time ran out.
func AwaitOver(await tools.Await, now time.Time, signals AwaitSignals) AwaitWake {
	switch {
	case signals.Steered:
		return AwaitSteered
	case len(await.TaskIDs) > 0 && signals.Ended, len(await.TaskIDs) == 0 && signals.Events:
		return AwaitEnded
	case now.Before(await.Until):
		return AwaitParked
	default:
		return AwaitTimedOut
	}
}

// resumeAwait keeps a run that called await parked until AwaitOver lets it go
// on: what the user wrote is journaled as a user message, a timeout is told to
// the model. It reports whether the run still waits.
func (r *run) resumeAwait(ctx context.Context) (bool, error) {
	await := r.state.Await
	if await == nil {
		return false, nil
	}
	steers, err := r.Inbox.Steers(ctx)
	if err != nil {
		return false, err
	}
	signals := AwaitSignals{Steered: len(steers) > 0}
	switch {
	case signals.Steered:
	case len(await.TaskIDs) > 0:
		if signals.Ended, err = r.Inbox.Finished(ctx, await.TaskIDs); err != nil {
			return false, err
		}
	default:
		events, err := r.Inbox.Events(ctx)
		if err != nil {
			return false, err
		}
		signals.Events = len(events) > 0
	}
	wake := AwaitOver(*await, r.Now(), signals)
	if wake == AwaitParked {
		return true, nil
	}
	r.state.Await = nil
	switch wake {
	case AwaitSteered:
		return false, r.appendSteers(ctx, steers)
	case AwaitTimedOut:
		return false, r.appendEngineMessage(ctx, transcript.OriginEngine, awaitTimeoutText(await.TaskIDs))
	}
	return false, nil
}

// appendImageSteers journals the pending steers that came with images, which
// no tool result can carry, before the next model call.
func (r *run) appendImageSteers(ctx context.Context) error {
	steers, err := r.Inbox.Steers(ctx)
	if err != nil {
		return err
	}
	steers = slices.DeleteFunc(steers, func(steer Steer) bool { return len(steer.Images) == 0 })
	if len(steers) == 0 {
		return nil
	}
	return r.appendSteers(ctx, steers)
}

// appendSteers journals steer input that no tool result can carry as one user
// message and consumes it.
func (r *run) appendSteers(ctx context.Context, steers []Steer) error {
	texts := make([]string, 0, len(steers))
	ids := make([]string, 0, len(steers))
	var images []transcript.MessagePart
	for _, steer := range steers {
		if steer.Text != "" {
			texts = append(texts, steer.Text)
		}
		images = append(images, steer.Images...)
		ids = append(ids, steer.ID)
	}
	text := strings.Join(texts, "\n\n")
	now := r.Now()
	message := transcript.Message{ID: r.NewID("msg"), SessionID: r.task.SessionID, RunID: r.task.RunID, Role: transcript.MessageRoleUser, Content: text, Parts: append(transcript.NormalizeMessageParts(text, nil), images...), CreatedAt: now, UpdatedAt: now}
	if err := r.history.append(ctx, message); err != nil {
		return err
	}
	return r.Inbox.ConsumeSteers(context.WithoutCancel(ctx), ids)
}

func awaitTimeoutText(taskIDs []string) string {
	if len(taskIDs) == 0 {
		return "Stopped waiting: the await timed out before any background task finished."
	}
	return fmt.Sprintf("Stopped waiting: the await timed out before %s finished.", strings.Join(taskIDs, ", "))
}
