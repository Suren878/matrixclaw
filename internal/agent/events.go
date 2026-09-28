package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// drainEvents journals what happened outside the run since its last step, such
// as background tasks that finished, as engine notes and then consumes it.
func (r *run) drainEvents(ctx context.Context) error {
	events, err := r.Inbox.Peek(ctx, r.task.RunID, InputEvent)
	if err != nil || len(events) == 0 {
		return err
	}
	ids := make([]string, 0, len(events))
	for _, event := range events {
		if err := r.appendEngineMessage(ctx, transcript.OriginEngine, event.Text); err != nil {
			return err
		}
		ids = append(ids, event.ID)
	}
	return r.Inbox.Consume(ctx, r.task.RunID, ids)
}

// resumeAwait keeps a run that called await parked until what it waits for
// happened: an awaited task finished, the user wrote, which is journaled as a
// user message, or the wait timed out, which the model is told. It reports
// whether the run still waits.
func (r *run) resumeAwait(ctx context.Context) (bool, error) {
	await := r.counters.Await
	if await == nil {
		return false, nil
	}
	steers, err := r.Inbox.Peek(ctx, r.task.RunID, InputSteer)
	if err != nil {
		return false, err
	}
	events, err := r.Inbox.Peek(ctx, r.task.RunID, InputEvent)
	if err != nil {
		return false, err
	}
	finished := slices.ContainsFunc(events, func(event Input) bool {
		return len(await.TaskIDs) == 0 || slices.Contains(await.TaskIDs, event.ID)
	})
	switch {
	case len(steers) > 0:
		r.counters.Await = nil
		return false, r.appendSteers(ctx, steers)
	case finished:
		r.counters.Await = nil
		return false, nil
	case r.Now().Before(await.Until):
		return true, nil
	default:
		r.counters.Await = nil
		return false, r.appendEngineMessage(ctx, transcript.OriginEngine, awaitTimeoutText(await.TaskIDs))
	}
}

// appendSteers journals steer input that no tool result can carry as one user
// message and consumes it.
func (r *run) appendSteers(ctx context.Context, steers []Input) error {
	texts := make([]string, 0, len(steers))
	ids := make([]string, 0, len(steers))
	for _, steer := range steers {
		texts = append(texts, steer.Text)
		ids = append(ids, steer.ID)
	}
	text := strings.Join(texts, "\n\n")
	now := r.Now()
	message := transcript.Message{ID: r.NewID("msg"), SessionID: r.task.SessionID, RunID: r.task.RunID, Role: transcript.MessageRoleUser, Content: text, Parts: transcript.NormalizeMessageParts(text, nil), CreatedAt: now, UpdatedAt: now}
	if err := r.history.append(ctx, message); err != nil {
		return err
	}
	return r.Inbox.Consume(context.WithoutCancel(ctx), r.task.RunID, ids)
}

func awaitTimeoutText(taskIDs []string) string {
	if len(taskIDs) == 0 {
		return "Stopped waiting: the await timed out before any background task finished."
	}
	return fmt.Sprintf("Stopped waiting: the await timed out before %s finished.", strings.Join(taskIDs, ", "))
}
