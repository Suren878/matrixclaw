package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func (c *Core) executeRequestedTools(ctx context.Context, turn turnExecution, response providers.Response) (bool, error) {
	history, err := c.store.ListMessages(ctx, turn.SessionID, 0)
	if err != nil {
		return false, err
	}
	completed := toolResultCallIDs(history)
	known := make(map[string]transcript.Message)
	for _, message := range history {
		for _, part := range message.Parts {
			if part.ToolCall != nil {
				known[part.ToolCall.ID] = message
			}
		}
	}
	seen := make(map[string]providers.ToolCall)
	waitingApproval := false
	for index, toolCall := range response.ToolCalls {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		id := strings.TrimSpace(toolCall.ID)
		if id != "" {
			if prior, ok := seen[id]; ok {
				if !sameRequestedTool(prior.Name, prior.Arguments, toolCall) {
					return false, fmt.Errorf("tool call ID %q reused with different arguments", id)
				}
				continue
			}
			if prior, ok := known[id]; ok {
				if prior.RunID != turn.RunID {
					return false, fmt.Errorf("tool call ID %q belongs to another run", id)
				}
				for _, part := range prior.Parts {
					if part.ToolCall != nil && part.ToolCall.ID == id && !sameRequestedTool(part.ToolCall.Name, []byte(part.ToolCall.Input), toolCall) {
						return false, fmt.Errorf("tool call ID %q reused with different arguments", id)
					}
				}
				if _, done := completed[id]; done {
					continue
				}
			}
			seen[id] = toolCall
		}
		input, err := turn.toolInput(toolCall)
		if err != nil {
			return false, err
		}
		result, err := c.ExecuteTool(ctx, input)
		if errors.Is(err, ErrInvalidInput) && result.ToolResultMessage == nil && result.ToolCallMessage.ID == "" {
			result, err = c.recordRejectedToolRequest(ctx, input, err)
		}
		if index == 0 {
			if attachErr := c.attachReasoningToToolCallMessage(ctx, result.ToolCallMessage, response.ReasoningContent); attachErr != nil {
				return false, attachErr
			}
		}
		if err != nil && result.ToolResultMessage == nil {
			return false, err
		}
		if result.ToolResultMessage != nil {
			if _, steerErr := c.injectPendingSteersIntoToolResultMessage(ctx, *result.ToolResultMessage); steerErr != nil {
				return false, steerErr
			}
		}
		if result.Approval != nil {
			waitingApproval = true
		}
	}
	return waitingApproval, nil
}

func sameRequestedTool(name string, arguments []byte, call providers.ToolCall) bool {
	if strings.TrimSpace(name) != strings.TrimSpace(call.Name) {
		return false
	}
	var left, right any
	if len(arguments) == 0 {
		arguments = []byte(`{}`)
	}
	if len(call.Arguments) == 0 {
		call.Arguments = []byte(`{}`)
	}
	if !json.Valid(arguments) || !json.Valid(call.Arguments) {
		return strings.TrimSpace(string(arguments)) == strings.TrimSpace(string(call.Arguments))
	}
	leftDecoder := json.NewDecoder(bytes.NewReader(arguments))
	rightDecoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	leftDecoder.UseNumber()
	rightDecoder.UseNumber()
	if leftDecoder.Decode(&left) != nil || rightDecoder.Decode(&right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}

// Invalid/unknown tools are feedback for the next model turn. Persist a paired
// failure without executing anything so the model can correct its request.
func (c *Core) recordRejectedToolRequest(ctx context.Context, input ExecuteToolInput, cause error) (ExecuteToolResult, error) {
	id := strings.TrimSpace(input.ToolCallID)
	if id == "" {
		id = c.newID("tool")
	}
	message := newToolCallMessage(id, input.SessionID, input.RunID, input.ToolName, input.Args, true, c.now().UTC())
	if err := c.store.SaveMessage(ctx, message); err != nil {
		return ExecuteToolResult{}, err
	}
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: input.SessionID, RunID: input.RunID, Payload: message})
	result, err := c.saveToolResultMessage(ctx, preparedToolCall{ToolCallID: id, SessionID: input.SessionID, RunID: input.RunID, ToolName: input.ToolName}, tools.Result{Content: cause.Error(), IsError: true})
	if err != nil {
		return ExecuteToolResult{}, err
	}
	return ExecuteToolResult{ToolCallMessage: message, ToolResultMessage: result}, nil
}

func (c *Core) attachReasoningToToolCallMessage(ctx context.Context, message transcript.Message, reasoningContent *string) error {
	if reasoningContent == nil || strings.TrimSpace(message.ID) == "" {
		return nil
	}
	for _, part := range message.Parts {
		if part.Reasoning != nil {
			return nil
		}
	}
	message.Parts = append([]transcript.MessagePart{{
		Kind: transcript.MessagePartKindReasoning,
		Reasoning: &transcript.ReasoningPart{
			Text: *reasoningContent,
		},
	}}, message.Parts...)
	message.UpdatedAt = c.now().UTC()
	if err := c.store.UpdateMessage(ctx, message); err != nil {
		return err
	}
	c.publishEvent(Event{
		Type:      EventMessageUpdated,
		SessionID: message.SessionID,
		RunID:     message.RunID,
		Payload:   message,
	})
	return nil
}
