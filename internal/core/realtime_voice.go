package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type CommitRealtimeVoiceTurnInput struct {
	SessionID           string `json:"session_id"`
	ProviderID          string `json:"provider_id,omitempty"`
	ModelID             string `json:"model_id,omitempty"`
	UserTranscript      string `json:"user_transcript,omitempty"`
	AssistantTranscript string `json:"assistant_transcript,omitempty"`
}

type CommitRealtimeVoiceTurnResult struct {
	UserMessage      *Message `json:"user_message,omitempty"`
	AssistantMessage *Message `json:"assistant_message,omitempty"`
}

func (c *Core) CommitRealtimeVoiceTurn(ctx context.Context, input CommitRealtimeVoiceTurnInput) (CommitRealtimeVoiceTurnResult, error) {
	sessionID := normalizeText(input.SessionID)
	if sessionID == "" {
		return CommitRealtimeVoiceTurnResult{}, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	userTranscript := strings.TrimSpace(input.UserTranscript)
	assistantTranscript := strings.TrimSpace(input.AssistantTranscript)
	if userTranscript == "" && assistantTranscript == "" {
		return CommitRealtimeVoiceTurnResult{}, fmt.Errorf("%w: transcript text is required", ErrInvalidInput)
	}

	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return CommitRealtimeVoiceTurnResult{}, err
	}

	now := c.now().UTC()
	result := CommitRealtimeVoiceTurnResult{}
	if userTranscript != "" {
		message := realtimeVoiceTextMessage(c.newID("msg"), sessionID, MessageRoleUser, userTranscript, "", "", now)
		if err := c.store.SaveMessage(ctx, message); err != nil {
			return CommitRealtimeVoiceTurnResult{}, err
		}
		c.publishEvent(Event{Type: EventMessageCreated, SessionID: sessionID, Payload: message})
		result.UserMessage = &message
	}
	if assistantTranscript != "" {
		createdAt := now
		if result.UserMessage != nil {
			createdAt = now.Add(time.Nanosecond)
		}
		message := realtimeVoiceTextMessage(c.newID("msg"), sessionID, MessageRoleAssistant, assistantTranscript, normalizeText(input.ProviderID), normalizeText(input.ModelID), createdAt)
		if err := c.store.SaveMessage(ctx, message); err != nil {
			return CommitRealtimeVoiceTurnResult{}, err
		}
		c.publishEvent(Event{Type: EventMessageCreated, SessionID: sessionID, Payload: message})
		result.AssistantMessage = &message
	}

	if result.AssistantMessage != nil {
		session.UpdatedAt = result.AssistantMessage.UpdatedAt
	} else if result.UserMessage != nil {
		session.UpdatedAt = result.UserMessage.UpdatedAt
	}
	if !session.UpdatedAt.IsZero() {
		if err := c.store.UpdateSession(ctx, session); err != nil {
			return CommitRealtimeVoiceTurnResult{}, err
		}
	}
	return result, nil
}

func realtimeVoiceTextMessage(id string, sessionID string, role MessageRole, content string, providerID string, modelID string, createdAt time.Time) Message {
	return Message{
		ID:        id,
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Parts:     NormalizeMessageParts(content, nil),
		Model:     modelID,
		Provider:  providerID,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
}
