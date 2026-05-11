package openaicompat

import (
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
	if len(message.Images) > 0 {
		chatMessage.Content = openAIContentParts(message)
	} else if content := strings.TrimSpace(message.Content); content != "" {
		chatMessage.Content = content
	}
	if strings.TrimSpace(message.ToolCallID) != "" {
		chatMessage.ToolCallID = strings.TrimSpace(message.ToolCallID)
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
		if name == "" {
			continue
		}
		result = append(result, providers.ToolCall{
			ID:        strings.TrimSpace(item.ID),
			Name:      name,
			Arguments: compactJSONRaw(item.Function.Arguments),
		})
	}
	return result
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
