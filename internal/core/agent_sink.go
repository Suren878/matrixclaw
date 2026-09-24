package core

import "github.com/Suren878/matrixclaw/internal/agent"

// coreSink publishes engine events on the core event bus.
type coreSink struct {
	c *Core
}

func (s coreSink) Emit(event agent.Event) {
	switch event.Kind {
	case agent.EventMessageCreated:
		s.c.publishEvent(Event{Type: EventMessageCreated, SessionID: event.SessionID, RunID: event.RunID, Payload: event.Message})
	case agent.EventMessageUpdated:
		s.c.publishEvent(Event{Type: EventMessageUpdated, SessionID: event.SessionID, RunID: event.RunID, Payload: event.Message})
	case agent.EventToolRequested:
		s.c.publishToolUpdate(event.SessionID, event.RunID, ToolUpdate{
			ToolCallID: event.ToolCallID,
			ToolName:   event.ToolName,
			State:      ToolLifecycleRequested,
			RunID:      event.RunID,
			SessionID:  event.SessionID,
		})
	case agent.EventToolFinished:
		prepared := preparedToolCall{SessionID: event.SessionID, RunID: event.RunID, ToolName: event.ToolName, ToolCallID: event.ToolCallID}
		s.c.publishFinishedToolUpdate(prepared, event.ResultMessageID, event.Result)
	}
}
