package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func (c *Core) ListMessages(ctx context.Context, sessionID string, limit int) ([]transcript.Message, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	return c.store.ListMessages(ctx, sessionID, limit)
}

func (c *Core) ListMessagesAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]transcript.Message, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	return c.store.ListMessagesAfter(ctx, sessionID, afterSeq, limit)
}
