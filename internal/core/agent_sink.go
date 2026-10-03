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
	case agent.EventContextMeasured:
		s.c.publishContextUsage(ContextUsage{SessionID: event.SessionID, TokenEstimate: event.ContextTokens, WindowTokens: event.WindowTokens}, event.RunID)
	}
}

// publishContextUsage announces a session's context size unless it is the one
// announced last.
func (c *Core) publishContextUsage(usage ContextUsage, runID string) {
	if last, ok := c.contextUsage.Swap(usage.SessionID, usage); ok && last == usage {
		return
	}
	c.publishEvent(Event{Type: EventContextUpdated, SessionID: usage.SessionID, RunID: runID, Payload: usage})
}
