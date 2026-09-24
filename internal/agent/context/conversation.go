package agentcontext

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/agent/prompt"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const maxProviderImageBytes int64 = 8 * 1024 * 1024

const restartFinishReason = "daemon_restart"

// Identity is the Provider and Model a runtime stamps on its replies; signed or
// encrypted reasoning is replayed only to the same pair.
type Identity struct {
	Provider string
	Model    string
}

// Conversation converts history into provider messages, pairing every tool call with its result; signed reasoning is kept only for target.
func Conversation(ctx context.Context, history []transcript.Message, reader AttachmentReader, currentRunID string, allowImageInput bool, target Identity) ([]providers.Message, error) {
	entries, err := convertProviderConversationHistory(ctx, history, reader, currentRunID, allowImageInput, target)
	if err != nil {
		return nil, err
	}
	toolResults := collectProviderToolResults(entries)

	conversation := make([]providers.Message, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		if isToolStepStart(entries[i]) {
			step, next := collectToolStep(entries, i)
			if len(step.ToolCalls) > 0 {
				conversation = append(conversation, step)
				conversation = appendProviderToolResults(conversation, step.ToolCalls, toolResults)
				i = next - 1
				continue
			}
		}
		for _, providerMessage := range entries[i].messages {
			if !isPairedToolResultMessage(providerMessage) {
				conversation = append(conversation, providerMessage)
			}
		}
	}
	return conversation, nil
}

type providerConversationEntry struct {
	source   transcript.Message
	messages []providers.Message
}

// isToolStepStart: a model response that called tools starts with its saved
// reply (finish reason tool_calls) or, in older histories, with its first call.
func isToolStepStart(entry providerConversationEntry) bool {
	return isToolStepReply(entry.source) || (len(entry.messages) == 1 && len(entry.messages[0].ToolCalls) > 0)
}

func isToolStepReply(message transcript.Message) bool {
	if message.Role != transcript.MessageRoleAssistant {
		return false
	}
	for _, part := range message.Parts {
		if part.Finish != nil && part.Finish.Reason == "tool_calls" {
			return true
		}
	}
	return false
}

// collectToolStep merges one model response (its reply, reasoning and every call
// it made) into one assistant message; results are paired after it in call order.
// Without a reply, a step ends at its first result, so older responses stay apart.
func collectToolStep(entries []providerConversationEntry, start int) (providers.Message, int) {
	step := providers.Message{Role: string(transcript.MessageRoleAssistant)}
	var parts []transcript.MessagePart
	hasReply := isToolStepReply(entries[start].source)
	i := start
collect:
	for ; i < len(entries); i++ {
		entry := entries[i]
		switch {
		case i == start:
			if len(entry.messages) == 1 {
				step.Content = entry.messages[0].Content
				step.Images = entry.messages[0].Images
				step.ToolCalls = append(step.ToolCalls, entry.messages[0].ToolCalls...)
			}
		case hasReply && len(entry.messages) == 1 && isPairedToolResultMessage(entry.messages[0]):
			continue
		case len(entry.messages) == 1 && isAdditionalBatchableToolCallMessage(entry.messages[0]):
			step.ToolCalls = append(step.ToolCalls, entry.messages[0].ToolCalls...)
		default:
			break collect
		}
		parts = append(parts, entry.source.Parts...)
	}
	step.ReasoningContent = messageReasoningContent(parts)
	step.Reasoning = messageReasoningBlocks(parts)
	return step, i
}

func convertProviderConversationHistory(ctx context.Context, history []transcript.Message, reader AttachmentReader, currentRunID string, allowImageInput bool, target Identity) ([]providerConversationEntry, error) {
	entries := make([]providerConversationEntry, 0, len(history))
	for _, message := range history {
		if skipInternalPlanPromptForProvider(message, currentRunID) || transcript.HasFinishReason(message, restartFinishReason) {
			continue
		}
		message = withoutForeignSignedReasoning(message, target)
		providerMessages, err := toProviderMessages(ctx, message, reader, allowImageInput)
		if err != nil {
			return nil, err
		}
		entries = append(entries, providerConversationEntry{source: message, messages: providerMessages})
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
	return strings.TrimSpace(message.Role) == string(transcript.MessageRoleTool) && strings.TrimSpace(message.ToolCallID) != ""
}

func isToolCallOnlyProviderMessage(message providers.Message) bool {
	return len(message.ToolCalls) > 0 && strings.TrimSpace(message.Content) == ""
}

func isAdditionalBatchableToolCallMessage(message providers.Message) bool {
	return strings.TrimSpace(message.Role) == string(transcript.MessageRoleAssistant) && isToolCallOnlyProviderMessage(message)
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
		Role:       string(transcript.MessageRoleTool),
		ToolCallID: toolCallID,
		Content:    "Tool execution failed before completion.",
		IsError:    true,
	}
}

// TextOnlyConversation renders history as plain text for models without tool calling.
func TextOnlyConversation(history []transcript.Message, currentRunID string) []providers.Message {
	conversation := make([]providers.Message, 0, len(history))
	for _, message := range history {
		if message.Role == transcript.MessageRoleSystem || skipInternalPlanPromptForProvider(message, currentRunID) || transcript.HasFinishReason(message, restartFinishReason) {
			continue
		}
		role := string(message.Role)
		content := textOnlyProviderContent(message)
		if strings.TrimSpace(content) == "" {
			continue
		}
		if message.Role == transcript.MessageRoleTool {
			role = string(transcript.MessageRoleUser)
		}
		conversation = append(conversation, providers.Message{
			Role:    role,
			Content: content,
		})
	}
	return conversation
}

func skipInternalPlanPromptForProvider(message transcript.Message, currentRunID string) bool {
	if !prompt.IsPlanRunPrompt(message) {
		return false
	}
	currentRunID = strings.TrimSpace(currentRunID)
	if currentRunID == "" {
		return true
	}
	return strings.TrimSpace(message.RunID) != currentRunID
}

func textOnlyProviderContent(message transcript.Message) string {
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
		if message.Role == transcript.MessageRoleTool {
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

	if message.Role != transcript.MessageRoleTool {
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

func formatToolCallAsText(part transcript.ToolCallPart) string {
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

func formatToolResultAsText(part transcript.ToolResultPart, fallbackContent string) string {
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
	content = providerVisibleToolResultContent(part.Name, content)
	return "Previous tool result from " + name + ":\n" + content
}

const (
	maxProviderToolResultRunes         = 12_000
	maxProviderBrowserSnapshotRunes    = 5_000
	providerToolResultTruncationNotice = "[provider context truncated; full tool result remains available in session history/UI. Use a narrower browser_snapshot or targeted browser_evaluate if more detail is needed.]"
)

func providerVisibleToolResultContent(toolName string, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "(empty result)"
	}
	limit := maxProviderToolResultRunes
	if isBrowserSnapshotToolName(toolName) {
		limit = maxProviderBrowserSnapshotRunes
	}
	return trimProviderToolResult(content, limit)
}

func isBrowserSnapshotToolName(name string) bool {
	name = strings.TrimSpace(name)
	return name == "mcp_browser_browser_snapshot" || name == "browser_snapshot" || strings.HasSuffix(name, "_browser_snapshot")
}

func trimProviderToolResult(content string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(content) <= maxRunes {
		return content
	}
	runes := []rune(content)
	head := maxRunes * 3 / 4
	tail := maxRunes - head
	return strings.TrimSpace(string(runes[:head])) + "\n[...truncated for provider context...]\n" + strings.TrimSpace(string(runes[len(runes)-tail:])) + "\n" + providerToolResultTruncationNotice
}

func toProviderMessages(ctx context.Context, message transcript.Message, reader AttachmentReader, allowImageInput bool) ([]providers.Message, error) {
	if message.Role == transcript.MessageRoleSystem {
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
	var imageParts []transcript.ImagePart
	var images []providers.ImageContent
	var attachmentWarnings []string
	reasoningContent := messageReasoningContent(message.Parts)
	for _, part := range message.Parts {
		if part.Image != nil {
			imagePart := *part.Image
			imageParts = append(imageParts, imagePart)
			// Do not try to read formats that cannot be sent to a provider in the
			// first place. In particular, an expired temporary SVG attachment in
			// conversation history must not make every later run fail.
			if strings.TrimSpace(imagePart.MIMEType) != "" && !providers.IsSupportedImageMIMEType(imagePart.MIMEType) {
				continue
			}
			if !allowImageInput {
				attachmentWarnings = append(attachmentWarnings, unsupportedModelImageWarning(imagePart))
				continue
			}
			image, err := providerImageContent(ctx, imagePart, reader)
			if err != nil {
				if errors.Is(err, ErrAttachmentUnavailable) {
					attachmentWarnings = append(attachmentWarnings, unavailableImageWarning(imagePart))
					continue
				}
				return nil, err
			}
			if strings.TrimSpace(image.DataBase64) != "" && providers.IsSupportedImageMIMEType(image.MIMEType) {
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
	providerContent := messageContentWithAttachmentRefs(message.Content, imageParts)
	providerContent = messageContentWithAttachmentWarnings(providerContent, attachmentWarnings)
	if len(toolCalls) > 0 {
		return []providers.Message{{
			Role:             string(message.Role),
			Content:          providerContent,
			ReasoningContent: reasoningContent,
			Reasoning:        messageReasoningBlocks(message.Parts),
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
		content = providerVisibleToolResultContent(part.ToolResult.Name, content)
		return []providers.Message{{
			Role:       string(message.Role),
			Content:    content,
			ToolCallID: strings.TrimSpace(part.ToolResult.ToolCallID),
			IsError:    part.ToolResult.IsError,
		}}, nil
	}

	if strings.TrimSpace(providerContent) == "" && len(images) == 0 {
		return nil, nil
	}
	return []providers.Message{{
		Role:             string(message.Role),
		Content:          providerContent,
		ReasoningContent: reasoningContent,
		Reasoning:        messageReasoningBlocks(message.Parts),
		Images:           images,
	}}, nil
}

func unsupportedModelImageWarning(part transcript.ImagePart) string {
	return "Image attachment " + imagePartLabel(part) + " was not sent because the selected model does not support image input."
}

func messageReasoningContent(parts []transcript.MessagePart) *string {
	var values []string
	for _, part := range parts {
		if part.Reasoning == nil || isSignedReasoning(*part.Reasoning) {
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

// messageReasoningBlocks returns reasoning the provider signed or encrypted; it
// goes back unchanged, unlike plain reasoning_content text.
func messageReasoningBlocks(parts []transcript.MessagePart) []providers.ReasoningBlock {
	var blocks []providers.ReasoningBlock
	for _, part := range parts {
		if part.Reasoning != nil && isSignedReasoning(*part.Reasoning) {
			blocks = append(blocks, providers.ReasoningBlock{Text: part.Reasoning.Text, Signature: part.Reasoning.Signature, RedactedData: part.Reasoning.RedactedData})
		}
	}
	return blocks
}

func isSignedReasoning(part transcript.ReasoningPart) bool {
	return part.Signature != "" || part.RedactedData != ""
}

// withoutForeignSignedReasoning drops signed or encrypted reasoning another
// provider or model produced; the target would reject it. Plain text stays.
func withoutForeignSignedReasoning(message transcript.Message, target Identity) transcript.Message {
	if strings.TrimSpace(message.Provider) == strings.TrimSpace(target.Provider) && strings.TrimSpace(message.Model) == strings.TrimSpace(target.Model) {
		return message
	}
	parts := make([]transcript.MessagePart, 0, len(message.Parts))
	for _, part := range message.Parts {
		if part.Reasoning == nil || !isSignedReasoning(*part.Reasoning) {
			parts = append(parts, part)
		}
	}
	message.Parts = parts
	return message
}

func imagePartLabel(part transcript.ImagePart) string {
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

func providerImageContent(ctx context.Context, part transcript.ImagePart, reader AttachmentReader) (providers.ImageContent, error) {
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

func messageContentWithAttachmentRefs(content string, images []transcript.ImagePart) string {
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

func unavailableImageWarning(image transcript.ImagePart) string {
	return "- " + imagePartLabel(image) + ": file is no longer available in storage"
}

func messageContentWithAttachmentWarnings(content string, warnings []string) string {
	if len(warnings) == 0 {
		return strings.TrimSpace(content)
	}
	block := "Unavailable attachments:\n" + strings.Join(warnings, "\n")
	if content = strings.TrimSpace(content); content == "" {
		return block
	}
	return content + "\n\n" + block
}

func quoteAttachmentValue(value string) string {
	return fmt.Sprintf("%q", strings.TrimSpace(value))
}
