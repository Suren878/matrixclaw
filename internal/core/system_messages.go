package core

import (
	"context"
	"fmt"
	"strings"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
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
	if agentcontext.IsMarker(content) {
		if executing, err := c.sessionRunExecuting(ctx, sessionID); err != nil {
			return transcript.Message{}, err
		} else if executing {
			return transcript.Message{}, fmt.Errorf("%w: wait for the current run to finish before clearing context", ErrRunActive)
		}
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
	if err := c.store.SaveMessage(ctx, message); err != nil {
		return transcript.Message{}, err
	}
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: sessionID, Payload: message})
	return message, nil
}
