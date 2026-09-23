package core

import (
	"context"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

// Retry only the model call, before any output was published or tools executed.
// A partial answer stays visible as failed instead of being replayed into the
// conversation or silently concatenated with a second attempt.
func (c *Core) generateAssistantTurnWithRetry(ctx context.Context, turn turnExecution, request providers.Request) (Message, bool, providers.Response, error) {
	backoffs := [...]time.Duration{200 * time.Millisecond, 750 * time.Millisecond}
	for attempt := 0; ; attempt++ {
		assistant, saved, response, err := c.generateAssistantTurn(ctx, turn, request)
		if err == nil && sanitizeAssistantOutput(response.Text) == "" && len(response.ToolCalls) == 0 {
			err = providers.ErrEmptyResponse
		}
		if err == nil || saved || assistant.Content != "" || ctx.Err() != nil || attempt >= len(backoffs) || !providers.IsRetryableGenerationError(err) {
			return assistant, saved, response, err
		}
		timer := time.NewTimer(backoffs[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return assistant, saved, response, ctx.Err()
		case <-timer.C:
		}
	}
}

// Finalize the model's commentary and usage before dispatching its tools. Some
// runtimes do not stream, and a streamed preview need not equal the final text.
func (c *Core) saveAssistantToolTurn(ctx context.Context, turn turnExecution, assistant *Message, saved bool, response providers.Response) error {
	if assistant == nil {
		return nil
	}
	assistant.Content = response.Text
	assistant.Model = response.Model
	assistant.Provider = response.Provider
	assistant.Parts = NormalizeMessageParts(assistant.Content, nil)
	finish := providerUsageFinishPart(response.Usage)
	if finish == nil {
		finish = &MessagePart{Kind: MessagePartKindFinish, Finish: &FinishPart{}}
	}
	finish.Finish.Reason = "tool_calls"
	assistant.Parts = append(assistant.Parts, *finish)
	assistant.UpdatedAt = c.now().UTC()
	eventType := EventMessageUpdated
	if saved {
		if err := c.store.UpdateMessage(ctx, *assistant); err != nil {
			return err
		}
	} else {
		assistant.CreatedAt = assistant.UpdatedAt
		if err := c.store.SaveMessage(ctx, *assistant); err != nil {
			return err
		}
		eventType = EventMessageCreated
	}
	c.saveRunUsage(ctx, Run{ID: turn.RunID, SessionID: turn.SessionID}, *assistant, response.Usage)
	c.publishEvent(Event{Type: eventType, SessionID: turn.SessionID, RunID: turn.RunID, Payload: *assistant})
	return nil
}
