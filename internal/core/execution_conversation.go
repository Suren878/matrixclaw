package core

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

const maxProviderImageBytes int64 = 8 * 1024 * 1024

func (c *Core) buildProviderConversation(ctx context.Context, history []Message, currentRunID string) ([]providers.Message, error) {
	return buildProviderConversationWithAttachmentsForRun(ctx, history, c.attachments, currentRunID)
}

func buildProviderConversationWithAttachmentsForRun(ctx context.Context, history []Message, reader AttachmentReader, currentRunID string) ([]providers.Message, error) {
	entries, err := convertProviderConversationHistory(ctx, history, reader, currentRunID)
	if err != nil {
		return nil, err
	}
	toolResults := collectProviderToolResults(entries)

	conversation := make([]providers.Message, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		providerMessages := entries[i].messages
		if len(providerMessages) == 0 {
			continue
		}

		providerMessage := providerMessages[0]
		if isPairedToolResultMessage(providerMessage) {
			continue
		}

		if !isToolCallOnlyProviderMessage(providerMessage) {
			conversation = append(conversation, providerMessages...)
			continue
		}

		var batched providers.Message
		batched, i = batchAdjacentToolCallMessages(entries, i)
		conversation = append(conversation, batched)
		conversation = appendProviderToolResults(conversation, batched.ToolCalls, toolResults)
	}
	return conversation, nil
}

type providerConversationEntry struct {
	messages []providers.Message
}

func convertProviderConversationHistory(ctx context.Context, history []Message, reader AttachmentReader, currentRunID string) ([]providerConversationEntry, error) {
	entries := make([]providerConversationEntry, 0, len(history))
	for _, message := range history {
		if skipInternalPlanPromptForProvider(message, currentRunID) {
			continue
		}
		providerMessages, err := toProviderMessages(ctx, message, reader)
		if err != nil {
			return nil, err
		}
		entries = append(entries, providerConversationEntry{messages: providerMessages})
	}
	return entries, nil
}

func collectProviderToolResults(entries []providerConversationEntry) map[string]providers.Message {
	toolResults := make(map[string]providers.Message)
	for _, entry := range entries {
		for _, providerMessage := range entry.messages {
			if !isPairedToolResultMessage(providerMessage) {
				continue
			}
			toolCallID := strings.TrimSpace(providerMessage.ToolCallID)
			if _, exists := toolResults[toolCallID]; exists {
				continue
			}
			toolResults[toolCallID] = providerMessage
		}
	}
	return toolResults
}

func isPairedToolResultMessage(message providers.Message) bool {
	return strings.TrimSpace(message.Role) == string(MessageRoleTool) && strings.TrimSpace(message.ToolCallID) != ""
}

func isToolCallOnlyProviderMessage(message providers.Message) bool {
	return len(message.ToolCalls) > 0 && strings.TrimSpace(message.Content) == ""
}

func batchAdjacentToolCallMessages(entries []providerConversationEntry, start int) (providers.Message, int) {
	batched := entries[start].messages[0]
	for i := start + 1; i < len(entries); i++ {
		if len(entries[i].messages) != 1 {
			return batched, i - 1
		}
		next := entries[i].messages[0]
		if !isAdditionalBatchableToolCallMessage(next) {
			return batched, i - 1
		}
		batched.ToolCalls = append(batched.ToolCalls, next.ToolCalls...)
	}
	return batched, len(entries) - 1
}

func isAdditionalBatchableToolCallMessage(message providers.Message) bool {
	return strings.TrimSpace(message.Role) == string(MessageRoleAssistant) && isToolCallOnlyProviderMessage(message)
}

func appendProviderToolResults(conversation []providers.Message, toolCalls []providers.ToolCall, toolResults map[string]providers.Message) []providers.Message {
	for _, toolCall := range toolCalls {
		toolCallID := strings.TrimSpace(toolCall.ID)
		if toolCallID == "" {
			continue
		}
		if toolResult, ok := toolResults[toolCallID]; ok {
			conversation = append(conversation, toolResult)
			delete(toolResults, toolCallID)
			continue
		}
		conversation = append(conversation, syntheticFailedToolResult(toolCallID))
	}
	return conversation
}

func syntheticFailedToolResult(toolCallID string) providers.Message {
	return providers.Message{
		Role:       string(MessageRoleTool),
		ToolCallID: toolCallID,
		Content:    "Tool execution failed before completion.",
	}
}

func buildTextOnlyProviderConversationForRun(history []Message, currentRunID string) []providers.Message {
	conversation := make([]providers.Message, 0, len(history))
	for _, message := range history {
		if message.Role == MessageRoleSystem || skipInternalPlanPromptForProvider(message, currentRunID) {
			continue
		}
		role := string(message.Role)
		content := textOnlyProviderContent(message)
		if strings.TrimSpace(content) == "" {
			continue
		}
		if message.Role == MessageRoleTool {
			role = string(MessageRoleUser)
		}
		conversation = append(conversation, providers.Message{
			Role:    role,
			Content: content,
		})
	}
	return conversation
}

func skipInternalPlanPromptForProvider(message Message, currentRunID string) bool {
	if !IsPlanRunPromptMessage(message) {
		return false
	}
	currentRunID = strings.TrimSpace(currentRunID)
	if currentRunID == "" {
		return true
	}
	return strings.TrimSpace(message.RunID) != currentRunID
}

// IsPlanRunPromptMessage reports whether message is an internal plan runner prompt.
// These prompts are stored so clients can audit local actions, but provider context
// already receives the current plan through sessionPlanPrompt.
func IsPlanRunPromptMessage(message Message) bool {
	if message.Role != MessageRoleUser {
		return false
	}
	content := strings.TrimSpace(message.Content)
	return strings.HasPrefix(content, "Execute the current session plan.") ||
		strings.HasPrefix(content, "Execute the next session plan item.") ||
		strings.HasPrefix(content, "The session plan was updated.")
}

func textOnlyProviderContent(message Message) string {
	var values []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}

	hasToolPart := false
	for _, part := range message.Parts {
		if part.ToolCall != nil || part.ToolResult != nil {
			hasToolPart = true
			break
		}
	}
	if !hasToolPart {
		if message.Role == MessageRoleTool {
			if content := strings.TrimSpace(message.Content); content != "" {
				add("Previous tool result:\n" + content)
			}
		} else {
			add(message.Content)
		}
		if strings.TrimSpace(message.Content) == "" {
			for _, part := range message.Parts {
				if part.Text != nil {
					add(part.Text.Text)
				}
				if part.Image != nil {
					add("Attached image: " + imagePartLabel(*part.Image))
				}
			}
		}
		return strings.Join(values, "\n\n")
	}

	if message.Role != MessageRoleTool {
		add(message.Content)
	}
	for _, part := range message.Parts {
		if part.ToolCall != nil {
			add(formatToolCallAsText(*part.ToolCall))
		}
		if part.ToolResult != nil {
			add(formatToolResultAsText(*part.ToolResult, message.Content))
		}
	}
	return strings.Join(values, "\n\n")
}

func formatToolCallAsText(part ToolCallPart) string {
	name := strings.TrimSpace(part.Name)
	if name == "" {
		name = "unknown"
	}
	input := strings.TrimSpace(part.Input)
	if input == "" {
		return "Previous tool call: " + name
	}
	return "Previous tool call: " + name + "\nInput:\n" + input
}

func formatToolResultAsText(part ToolResultPart, fallbackContent string) string {
	name := strings.TrimSpace(part.Name)
	if name == "" {
		name = "unknown"
	}
	content := strings.TrimSpace(part.Content)
	if content == "" {
		content = strings.TrimSpace(fallbackContent)
	}
	if content == "" {
		content = "(empty result)"
	}
	return "Previous tool result from " + name + ":\n" + content
}

func toProviderMessages(ctx context.Context, message Message, reader AttachmentReader) ([]providers.Message, error) {
	if message.Role == MessageRoleSystem {
		return nil, nil
	}
	if len(message.Parts) == 0 {
		if strings.TrimSpace(message.Content) == "" {
			return nil, nil
		}
		return []providers.Message{{
			Role:    string(message.Role),
			Content: message.Content,
		}}, nil
	}

	var toolCalls []providers.ToolCall
	var imageParts []ImagePart
	var images []providers.ImageContent
	reasoningContent := messageReasoningContent(message.Parts)
	for _, part := range message.Parts {
		if part.Image != nil {
			imagePart := *part.Image
			imageParts = append(imageParts, imagePart)
			image, err := providerImageContent(ctx, imagePart, reader)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(image.DataBase64) != "" {
				images = append(images, image)
			}
		}
		if part.ToolCall != nil {
			toolName := strings.TrimSpace(part.ToolCall.Name)
			toolInput := strings.TrimSpace(part.ToolCall.Input)
			toolCalls = append(toolCalls, providers.ToolCall{
				ID:        strings.TrimSpace(part.ToolCall.ID),
				Name:      toolName,
				Arguments: []byte(toolInput),
			})
		}
	}
	if len(toolCalls) > 0 {
		return []providers.Message{{
			Role:             string(message.Role),
			Content:          messageContentWithAttachmentRefs(message.Content, imageParts),
			ReasoningContent: reasoningContent,
			Images:           images,
			ToolCalls:        toolCalls,
		}}, nil
	}

	for _, part := range message.Parts {
		if part.ToolResult == nil {
			continue
		}
		content := part.ToolResult.Content
		if strings.TrimSpace(content) == "" {
			content = message.Content
		}
		if strings.TrimSpace(content) == "" {
			content = "(empty result)"
		}
		return []providers.Message{{
			Role:       string(message.Role),
			Content:    content,
			ToolCallID: strings.TrimSpace(part.ToolResult.ToolCallID),
		}}, nil
	}

	if strings.TrimSpace(message.Content) == "" && len(images) == 0 {
		return nil, nil
	}
	return []providers.Message{{
		Role:             string(message.Role),
		Content:          messageContentWithAttachmentRefs(message.Content, imageParts),
		ReasoningContent: reasoningContent,
		Images:           images,
	}}, nil
}

func messageReasoningContent(parts []MessagePart) *string {
	var values []string
	for _, part := range parts {
		if part.Reasoning == nil {
			continue
		}
		values = append(values, part.Reasoning.Text)
	}
	if len(values) == 0 {
		return nil
	}
	value := strings.Join(values, "\n")
	return &value
}

func imagePartLabel(part ImagePart) string {
	name := strings.TrimSpace(part.Name)
	mimeType := strings.TrimSpace(part.MIMEType)
	storagePath := strings.TrimSpace(part.StoragePath)
	if storagePath != "" {
		location := "storage_path=" + storagePath
		if part.Temporary {
			location = "temp_path=" + storagePath
		}
		if name != "" && mimeType != "" {
			return name + " (" + mimeType + ", " + location + ")"
		}
		if name != "" {
			return name + " (" + location + ")"
		}
		if mimeType != "" {
			return mimeType + " (" + location + ")"
		}
		return location
	}
	switch {
	case name != "" && mimeType != "":
		return name + " (" + mimeType + ")"
	case name != "":
		return name
	case mimeType != "":
		return mimeType
	default:
		return "image"
	}
}

func providerImageContent(ctx context.Context, part ImagePart, reader AttachmentReader) (providers.ImageContent, error) {
	image := providers.ImageContent{
		MIMEType:    strings.TrimSpace(part.MIMEType),
		DataBase64:  strings.TrimSpace(part.DataBase64),
		Name:        strings.TrimSpace(part.Name),
		StoragePath: strings.TrimSpace(part.StoragePath),
		Temporary:   part.Temporary,
		Size:        part.Size,
	}
	if image.DataBase64 != "" {
		return image, nil
	}
	if image.StoragePath == "" {
		return image, nil
	}
	if reader == nil {
		return image, fmt.Errorf("read image attachment %s: attachment storage is not configured", image.StoragePath)
	}
	data, err := reader.ReadAttachment(ctx, image.StoragePath, image.Temporary, maxProviderImageBytes)
	if err != nil {
		return image, fmt.Errorf("read image attachment %s: %w", image.StoragePath, err)
	}
	if int64(len(data.Data)) > maxProviderImageBytes {
		return image, fmt.Errorf("read image attachment %s: image is too large: %d bytes exceeds %d", image.StoragePath, len(data.Data), maxProviderImageBytes)
	}
	if image.MIMEType == "" {
		image.MIMEType = strings.TrimSpace(data.MIMEType)
	}
	if image.Name == "" {
		image.Name = strings.TrimSpace(data.Name)
	}
	if image.Size == 0 {
		image.Size = data.Size
	}
	image.DataBase64 = base64.StdEncoding.EncodeToString(data.Data)
	return image, nil
}

func messageContentWithAttachmentRefs(content string, images []ImagePart) string {
	content = strings.TrimSpace(content)
	var refs []string
	for _, image := range images {
		path := strings.TrimSpace(image.StoragePath)
		if path == "" {
			continue
		}
		key := "storage_path"
		if image.Temporary {
			key = "temp_path"
		}
		var fields []string
		fields = append(fields, key+"="+quoteAttachmentValue(path))
		if name := strings.TrimSpace(image.Name); name != "" {
			fields = append(fields, "name="+quoteAttachmentValue(name))
		}
		if mimeType := strings.TrimSpace(image.MIMEType); mimeType != "" {
			fields = append(fields, "mime_type="+quoteAttachmentValue(mimeType))
		}
		if image.Size > 0 {
			fields = append(fields, fmt.Sprintf("size=%d", image.Size))
		}
		refs = append(refs, "- kind=image "+strings.Join(fields, " "))
	}
	if len(refs) == 0 {
		return content
	}
	block := "Attached files:\n" + strings.Join(refs, "\n")
	if content == "" {
		return block
	}
	return content + "\n\n" + block
}

func quoteAttachmentValue(value string) string {
	return fmt.Sprintf("%q", strings.TrimSpace(value))
}
