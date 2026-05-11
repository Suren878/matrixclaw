package core

import (
	"context"
	"strings"
)

func (c *Core) CreateSystemMessage(ctx context.Context, sessionID string, content string) (Message, error) {
	sessionID = normalizeText(sessionID)
	content = strings.TrimSpace(content)
	if sessionID == "" {
		return Message{}, ErrSessionRequired
	}
	if content == "" {
		return Message{}, ErrInvalidInput
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return Message{}, err
	}

	now := c.now().UTC()
	message := Message{
		ID:        c.newID("msg"),
		SessionID: sessionID,
		Role:      MessageRoleSystem,
		Content:   content,
		Parts: []MessagePart{{
			Kind: MessagePartKindText,
			Text: &TextPart{Text: content},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := c.store.SaveMessage(ctx, message); err != nil {
		return Message{}, err
	}
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: sessionID, Payload: message})
	return message, nil
}
