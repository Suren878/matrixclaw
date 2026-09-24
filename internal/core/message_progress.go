package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// MessageProgressStore is an optional fast path for transient streaming
// snapshots. Final message writes still use MessageStore so search indexes and
// other durable projections are updated exactly once at a turn boundary.
type MessageProgressStore interface {
	SaveMessageProgress(ctx context.Context, message transcript.Message) (int64, error)
	UpdateMessageProgress(ctx context.Context, message transcript.Message) error
}

func (c *Core) saveMessageProgress(ctx context.Context, message transcript.Message) (int64, error) {
	if progressStore, ok := c.store.(MessageProgressStore); ok {
		return progressStore.SaveMessageProgress(ctx, message)
	}
	return c.store.AppendMessage(ctx, message)
}

func (c *Core) updateMessageProgress(ctx context.Context, message transcript.Message) error {
	if progressStore, ok := c.store.(MessageProgressStore); ok {
		return progressStore.UpdateMessageProgress(ctx, message)
	}
	return c.store.UpdateMessage(ctx, message)
}
