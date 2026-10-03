package openaicompat

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (r *Runtime) chatMessage(message providers.Message) chatCompletionMessage {
	chatMessage := chatCompletionMessage{
		Role:    normalizeOpenAIRole(message.Role),
		Content: "",
	}
	if len(message.Images) > 0 && r.Capabilities.ImageInput {
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

// markContentCacheBreakpoints marks the system prompt and the two newest user or
// tool messages as cache breakpoints, turning their content into parts.
func markContentCacheBreakpoints(messages []chatCompletionMessage) {
	marked := 0
	for i := len(messages) - 1; i >= 0; i-- {
		switch role := messages[i].Role; {
		case role == "system":
			withCacheBreakpoint(&messages[i])
		case marked < 2 && (role == "user" || role == "tool"):
			if withCacheBreakpoint(&messages[i]) {
				marked++
			}
		}
	}
}

// withCacheBreakpoint puts a cache breakpoint on the last text part of message.
func withCacheBreakpoint(message *chatCompletionMessage) bool {
	switch content := message.Content.(type) {
	case string:
		if strings.TrimSpace(content) == "" {
			return false
		}
		message.Content = []chatCompletionContentPart{{Type: "text", Text: content, CacheControl: &chatCacheControl{Type: "ephemeral"}}}
		return true
	case []chatCompletionContentPart:
		for j := len(content) - 1; j >= 0; j-- {
			if content[j].Type == "text" {
				content[j].CacheControl = &chatCacheControl{Type: "ephemeral"}
				return true
			}
		}
	}
	return false
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

// namespaceToolCalls gives the response's calls IDs of their own: Kimi numbers
// calls per conversation (functions.read:0), so its IDs repeat in other sessions
// and after compaction. A call the response repeats keeps one ID.
func namespaceToolCalls(calls []providers.ToolCall) []providers.ToolCall {
	prefix := "call_" + rand.Text()
	ids := make(map[string]string, len(calls))
	for i := range calls {
		if calls[i].ID == "" {
			continue
		}
		id, ok := ids[calls[i].ID]
		if !ok {
			id = fmt.Sprintf("%s_%d", prefix, len(ids))
			ids[calls[i].ID] = id
		}
		calls[i].ID = id
	}
	return calls
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
