package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func (c *Core) finishToolCall(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput, result tools.Result) (transcript.Message, *transcript.Message, error) {
	toolCallMessage := agent.ToolCallMessage(prepared.ToolCallID, prepared.SessionID, prepared.RunID, prepared.ToolName, input.Args, true, prepared.Message.CreatedAt)
	toolCallMessage.UpdatedAt = c.now().UTC()
	if err := c.store.UpdateMessage(ctx, toolCallMessage); err != nil {
		return transcript.Message{}, nil, err
	}
	c.publishEvent(Event{
		Type:      EventMessageUpdated,
		SessionID: prepared.SessionID,
		RunID:     toolCallMessage.RunID,
		Payload:   toolCallMessage,
	})

	resultMessage, err := c.saveToolResultMessage(ctx, prepared, result)
	if err != nil {
		return transcript.Message{}, nil, err
	}
	c.publishFinishedToolUpdate(prepared, resultMessage.ID, result)
	return toolCallMessage, resultMessage, nil
}

func (c *Core) saveToolResultMessage(ctx context.Context, prepared preparedToolCall, result tools.Result) (*transcript.Message, error) {
	message, err := agent.ToolResultMessage(c.newID("tool_result"), prepared.SessionID, prepared.RunID, prepared.ToolCallID, prepared.ToolName, result, c.now().UTC())
	if err != nil {
		return nil, err
	}
	if _, err := c.store.AppendMessage(ctx, message); err != nil {
		return nil, err
	}
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: prepared.SessionID, RunID: message.RunID, Payload: message})
	return &message, nil
}

func (c *Core) publishFinishedToolUpdate(prepared preparedToolCall, resultMessageID string, result tools.Result) {
	toolState := ToolLifecycleCompleted
	if result.IsError() {
		toolState = ToolLifecycleFailed
	}
	c.publishToolUpdate(prepared.SessionID, prepared.RunID, ToolUpdate{
		ToolCallID:      prepared.ToolCallID,
		ToolName:        prepared.ToolName,
		State:           toolState,
		ResultStatus:    string(agent.ToolResultStatus(result)),
		RunID:           prepared.RunID,
		SessionID:       prepared.SessionID,
		ResultMessageID: resultMessageID,
		Error:           errorText(result),
	})
}

func (c *Core) publishToolUpdate(sessionID string, runID string, update ToolUpdate) {
	c.publishEvent(Event{
		Type:      EventToolUpdated,
		SessionID: sessionID,
		RunID:     runID,
		Payload:   update,
	})
}
