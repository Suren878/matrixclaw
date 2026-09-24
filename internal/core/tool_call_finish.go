package core

import (
	"context"
	"time"

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
	_ = c.touchSubagentTaskActivity(ctx, prepared.RunID, resultMessage.UpdatedAt)
	if err := c.saveFileVersionSnapshot(ctx, prepared, result, resultMessage.CreatedAt); err != nil {
		return transcript.Message{}, nil, err
	}
	if err := c.saveRunCheckpoint(ctx, prepared.RunID, RunCheckpointPhaseModel, "", ""); err != nil {
		return transcript.Message{}, nil, err
	}
	return toolCallMessage, resultMessage, nil
}

func (c *Core) saveToolResultMessage(ctx context.Context, prepared preparedToolCall, result tools.Result) (*transcript.Message, error) {
	message, err := agent.ToolResultMessage(c.newID("tool_result"), prepared.SessionID, prepared.RunID, prepared.ToolCallID, prepared.ToolName, result, c.now().UTC())
	if err != nil {
		return nil, err
	}
	if err := c.store.SaveMessage(ctx, message); err != nil {
		return nil, err
	}
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: prepared.SessionID, RunID: message.RunID, Payload: message})
	return &message, nil
}

func (c *Core) publishFinishedToolUpdate(prepared preparedToolCall, resultMessageID string, result tools.Result) {
	toolState := ToolLifecycleCompleted
	if result.IsError {
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

func (c *Core) saveFileVersionSnapshot(ctx context.Context, prepared preparedToolCall, result tools.Result, createdAt time.Time) error {
	if result.FileVersion == nil {
		return nil
	}
	fileSnapshot, err := c.store.CreateFileSnapshot(ctx, FileSnapshot{
		ID:        c.newID("file"),
		SessionID: prepared.SessionID,
		Path:      result.FileVersion.Path,
		Content:   result.FileVersion.NewContent,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	if err != nil {
		return err
	}
	c.publishEvent(Event{
		Type:      EventFileVersioned,
		SessionID: prepared.SessionID,
		RunID:     prepared.RunID,
		Payload:   fileSnapshot,
	})
	return nil
}
