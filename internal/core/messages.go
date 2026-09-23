package core

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func (c *Core) ListMessages(ctx context.Context, sessionID string, limit int) ([]transcript.Message, error) {
	if normalizeText(sessionID) == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	return c.store.ListMessages(ctx, normalizeText(sessionID), limit)
}

func (c *Core) ListMessagesAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]transcript.Message, error) {
	if normalizeText(sessionID) == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	return c.store.ListMessagesAfter(ctx, normalizeText(sessionID), afterSeq, limit)
}
