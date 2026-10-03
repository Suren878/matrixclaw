package core

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func (c *Core) CreateSystemMessage(ctx context.Context, sessionID string, content string) (transcript.Message, error) {
	sessionID = normalizeText(sessionID)
	content = strings.TrimSpace(content)
	if sessionID == "" {
		return transcript.Message{}, ErrSessionRequired
	}
	if content == "" {
		return transcript.Message{}, ErrInvalidInput
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return transcript.Message{}, err
	}
	now := c.now().UTC()
	message := transcript.Message{
		ID:        c.newID("msg"),
		SessionID: sessionID,
		Role:      transcript.MessageRoleSystem,
		Content:   content,
		Parts: []transcript.MessagePart{{
			Kind: transcript.MessagePartKindText,
			Text: &transcript.TextPart{Text: content},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := c.store.AppendMessage(ctx, message); err != nil {
		return transcript.Message{}, err
	}
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: sessionID, Payload: message})
	return message, nil
}
