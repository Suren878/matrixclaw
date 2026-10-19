package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/events"
)

const (
	EventRunUpdated      events.Type = "run.updated"
	EventMessageCreated  events.Type = "message.created"
	EventMessageUpdated  events.Type = "message.updated"
	EventTodoUpdated     events.Type = "todo.updated"
	EventToolUpdated     events.Type = "tool.updated"
	EventApprovalRequest events.Type = "approval.requested"
	EventApprovalResult  events.Type = "approval.resolved"
	EventInputUpdated    events.Type = "input.updated"
	EventTaskUpdated     events.Type = "task.updated"
	EventContextUpdated  events.Type = "context.updated"
)

// ContextUsage is how much of the model's window a session's conversation
// fills, as measured after a run's latest step.
type ContextUsage struct {
	SessionID     string `json:"session_id"`
	TokenEstimate int    `json:"token_estimate"`
	WindowTokens  int    `json:"window_tokens,omitempty"`
}

// SubscribeEvents streams the session's events (every session's for an empty
// sessionID), first those after afterID the bus still holds.
func (c *Core) SubscribeEvents(ctx context.Context, sessionID string, afterID uint64) <-chan events.Event {
	return c.events.Subscribe(ctx, sessionID, afterID)
}

func (c *Core) publishEvent(event events.Event) {
	if c.events == nil {
		return
	}
	if event.At.IsZero() {
		event.At = c.now().UTC()
	}
	c.events.Publish(event)
}
