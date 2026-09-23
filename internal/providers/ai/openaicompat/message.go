package openaicompat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func combinedSystemPrompt(systemPrompt string, customInstructions string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	customInstructions = strings.TrimSpace(customInstructions)
	if customInstructions == "" {
		return systemPrompt
	}
	block := "User custom instructions:\n" + customInstructions
	if systemPrompt == "" {
		return block
	}
	return systemPrompt + "\n\n" + block
}

func (r *Runtime) chatMessage(message providers.Message) chatCompletionMessage {
	chatMessage := chatCompletionMessage{
		Role:    normalizeOpenAIRole(message.Role),
		Content: "",
	}
	if len(message.Images) > 0 && r.capabilities.ImageInput {
		chatMessage.Content = openAIContentParts(message)
	} else if content := strings.TrimSpace(message.Content); content != "" {
		chatMessage.Content = content
	} else if len(message.Images) > 0 {
		chatMessage.Content = "[Image attachment was not sent because the selected model does not support image input.]"
	}
	if strings.TrimSpace(message.ToolCallID) != "" {
		chatMessage.ToolCallID = strings.TrimSpace(message.ToolCallID)
	}
	if chatMessage.Role == "assistant" && message.ReasoningContent != nil {
		reasoningContent := *message.ReasoningContent
		chatMessage.ReasoningContent = &reasoningContent
	}
	return chatMessage
}

func openAIContentParts(message providers.Message) []chatCompletionContentPart {
	parts := make([]chatCompletionContentPart, 0, 1+len(message.Images))
	if content := strings.TrimSpace(message.Content); content != "" {
		parts = append(parts, chatCompletionContentPart{Type: "text", Text: content})
	}
	for _, image := range message.Images {
		data := strings.TrimSpace(image.DataBase64)
		if data == "" {
			continue
		}
		mimeType := strings.TrimSpace(image.MIMEType)
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		parts = append(parts, chatCompletionContentPart{
			Type: "image_url",
			ImageURL: &chatCompletionContentImageURL{
				URL: "data:" + mimeType + ";base64," + data,
			},
		})
	}
	return parts
}

func decodeToolCalls(value []chatCompletionToolCall) []providers.ToolCall {
	if len(value) == 0 {
		return nil
	}
	result := make([]providers.ToolCall, 0, len(value))
	for _, item := range value {
		name := strings.TrimSpace(item.Function.Name)
		result = append(result, providers.ToolCall{
			ID:        strings.TrimSpace(item.ID),
			Name:      name,
			Arguments: compactJSONRaw(item.Function.Arguments),
		})
	}
	return result
}

func validateToolCalls(calls []providers.ToolCall) error {
	for _, call := range calls {
		if strings.TrimSpace(call.Name) == "" {
			return fmt.Errorf("openaicompat: function call is missing a name")
		}
		if !json.Valid(call.Arguments) {
			return fmt.Errorf("openaicompat: invalid arguments for tool %q", call.Name)
		}
	}
	return nil
}

func openAIStopReason(reason string) providers.StopReason {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length", "max_tokens":
		return providers.StopMaxTokens
	case "content_filter":
		return providers.StopContentFilter
	case "tool_calls", "function_call":
		return providers.StopToolUse
	default:
		return providers.StopEndTurn
	}
}

// openAIIncompleteFinishReason reports a mid-generation failure — OpenRouter's
// "error" or DeepSeek's "insufficient_system_resource" — that should be
// retried rather than treated as a normal stop.
func openAIIncompleteFinishReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "error", "insufficient_system_resource":
		return true
	default:
		return false
	}
}

// completeToolCalls keeps the calls a truncated reply finished before the limit.
func completeToolCalls(calls []providers.ToolCall) []providers.ToolCall {
	var out []providers.ToolCall
	for _, call := range calls {
		if strings.TrimSpace(call.Name) != "" && json.Valid(call.Arguments) {
			out = append(out, call)
		}
	}
	return out
}

func normalizeOpenAIRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "developer":
		return "developer"
	case "system":
		return "system"
	case "assistant":
		return "assistant"
	case "tool":
		return "tool"
	default:
		return "user"
	}
}
