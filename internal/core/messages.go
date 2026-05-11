package core

import (
	"context"
	"fmt"
)

func (c *Core) ListMessages(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	if normalizeText(sessionID) == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	return c.store.ListMessages(ctx, normalizeText(sessionID), limit)
}
