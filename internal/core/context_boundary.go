package core

import (
	"context"
	"fmt"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ClearContext starts the session's model context afresh with a boundary that
// covers every message so far; the transcript keeps them.
func (c *Core) ClearContext(ctx context.Context, sessionID string) (transcript.Message, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return transcript.Message{}, ErrSessionRequired
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return transcript.Message{}, err
	}
	if active, err := c.sessionRunActive(ctx, sessionID); err != nil {
		return transcript.Message{}, err
	} else if active {
		return transcript.Message{}, fmt.Errorf("%w: finish or cancel the current run before clearing context", ErrRunActive)
	}
	latest, err := c.store.ListMessages(ctx, sessionID, 1)
	if err != nil {
		return transcript.Message{}, err
	}
	compaction := transcript.Compaction{Cleared: true}
	if len(latest) > 0 {
		compaction.CoversThroughSeq = latest[0].Seq
	}
	return c.appendBoundary(ctx, sessionID, agentcontext.BoundaryLabel(compaction), compaction)
}

// appendBoundary journals a context boundary of the session and announces it.
func (c *Core) appendBoundary(ctx context.Context, sessionID string, content string, compaction transcript.Compaction) (transcript.Message, error) {
	now := c.now().UTC()
	message := transcript.Message{
		ID:         c.newID("msg"),
		SessionID:  sessionID,
		Role:       transcript.MessageRoleSystem,
		Content:    content,
		Parts:      transcript.NormalizeMessageParts(content, nil),
		Compaction: &compaction,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	seq, err := c.store.AppendMessage(ctx, message)
	if err != nil {
		return transcript.Message{}, err
	}
	message.Seq = seq
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: sessionID, Payload: message})
	return message, nil
}
