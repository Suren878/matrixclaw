package agent

import (
	"context"

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
