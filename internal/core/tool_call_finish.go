package core

import (
	"context"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func (c *Core) finishToolCall(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput, result tools.Result) (Message, *Message, error) {
	toolCallMessage := newToolCallMessage(prepared.ToolCallID, prepared.SessionID, prepared.RunID, prepared.ToolName, input.Args, true, prepared.Message.CreatedAt)
	toolCallMessage.UpdatedAt = c.now().UTC()
	if err := c.store.UpdateMessage(ctx, toolCallMessage); err != nil {
		return Message{}, nil, err
	}
	c.publishEvent(Event{
		Type:      EventMessageUpdated,
		SessionID: prepared.SessionID,
		RunID:     toolCallMessage.RunID,
		Payload:   toolCallMessage,
	})

	resultMessage, err := c.saveToolResultMessage(ctx, prepared, result)
	if err != nil {
		return Message{}, nil, err
	}
	c.publishFinishedToolUpdate(prepared, resultMessage.ID, result)
	if err := c.saveFileVersionSnapshot(ctx, prepared, result, resultMessage.CreatedAt); err != nil {
		return Message{}, nil, err
	}
	return toolCallMessage, resultMessage, nil
}

func (c *Core) saveToolResultMessage(ctx context.Context, prepared preparedToolCall, result tools.Result) (*Message, error) {
	metadataRaw, err := marshalJSONRaw(result.Metadata)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(result.Content)
	now := c.now().UTC()
	message := Message{
		ID:        c.newID("tool_result"),
		SessionID: prepared.SessionID,
		RunID:     prepared.RunID,
		Role:      MessageRoleTool,
		Content:   normalizeToolContent(content),
		Parts: []MessagePart{{
			Kind: MessagePartKindToolResult,
			ToolResult: &ToolResultPart{
				ToolCallID: prepared.ToolCallID,
				Name:       prepared.ToolName,
				Content:    content,
				MIMEType:   result.MIMEType,
				Metadata:   metadataRaw,
				Status:     string(toolResultStatus(result)),
				IsError:    result.IsError,
			},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := c.store.SaveMessage(ctx, message); err != nil {
		return nil, err
	}
	c.publishEvent(Event{
		Type:      EventMessageCreated,
		SessionID: prepared.SessionID,
		RunID:     message.RunID,
		Payload:   message,
	})
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
		ResultStatus:    string(toolResultStatus(result)),
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
