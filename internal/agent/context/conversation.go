package agentcontext

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const maxProviderImageBytes int64 = 8 * 1024 * 1024

// Identity is the Provider and Model a runtime stamps on its replies; signed or
// encrypted reasoning is replayed only to the same pair.
type Identity struct {
	Provider string
	Model    string
}

// Conversation converts history into provider messages, pairing every tool call with its result; signed reasoning is kept only for target.
func Conversation(ctx context.Context, history []transcript.Message, reader AttachmentReader, allowImageInput bool, target Identity) ([]providers.Message, error) {
	entries, err := convertProviderConversationHistory(ctx, history, reader, allowImageInput, target)
	if err != nil {
		return nil, err
	}
	toolResults := collectProviderToolResults(entries)
	deferred := deferredToolCallIDs(history)

	conversation := make([]providers.Message, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		if isToolStepStart(entries[i]) {
			step, next := collectToolStep(entries, i)
			if len(step.ToolCalls) > 0 {
				conversation = append(conversation, step)
				conversation = appendProviderToolResults(conversation, step.ToolCalls, toolResults, deferred)
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
		if part.Finish != nil && part.Finish.Reason == transcript.FinishReasonToolCalls {
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

func convertProviderConversationHistory(ctx context.Context, history []transcript.Message, reader AttachmentReader, allowImageInput bool, target Identity) ([]providerConversationEntry, error) {
	entries := make([]providerConversationEntry, 0, len(history))
	for _, message := range history {
		if transcript.HasFinishReason(message, transcript.FinishReasonDaemonRestart) {
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

// deferredToolCallIDs lists the calls held back behind an approval barrier.
func deferredToolCallIDs(history []transcript.Message) map[string]bool {
	deferred := map[string]bool{}
	for _, message := range history {
		for _, part := range message.Parts {
			if part.ToolCall != nil && part.ToolCall.Deferred {
				deferred[strings.TrimSpace(part.ToolCall.ID)] = true
			}
		}
	}
	return deferred
}

// appendProviderToolResults pairs each call with its result; a call left without
// one gets a closing error: one held back behind a barrier never ran.
func appendProviderToolResults(conversation []providers.Message, toolCalls []providers.ToolCall, toolResults map[string]providers.Message, deferred map[string]bool) []providers.Message {
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
		content := "Tool execution failed before completion."
		if deferred[toolCallID] {
			content = "Not run: the run was canceled."
		}
		conversation = append(conversation, providers.Message{Role: string(transcript.MessageRoleTool), ToolCallID: toolCallID, Content: content, IsError: true})
	}
	return conversation
}

// engineNoteMessage is the user text the model reads for an engine note; other
// system rows never reach the model.
func engineNoteMessage(message transcript.Message) (providers.Message, bool) {
	content := strings.TrimSpace(message.Content)
	if !message.Origin.IsEngine() || content == "" {
		return providers.Message{}, false
	}
	return providers.Message{Role: string(transcript.MessageRoleUser), Content: content}, true
}

// TextOnlyConversation renders history as plain text for models without tool calling.
func TextOnlyConversation(history []transcript.Message) []providers.Message {
	conversation := make([]providers.Message, 0, len(history))
	for _, message := range history {
		if transcript.HasFinishReason(message, transcript.FinishReasonDaemonRestart) {
			continue
		}
		if message.Role == transcript.MessageRoleSystem {
			if note, ok := engineNoteMessage(message); ok {
				conversation = append(conversation, note)
			}
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
	content = providerVisibleToolResultContent(content)
	return "Previous tool result from " + name + ":\n" + content
}

// providerVisibleToolResultContent is a result as the model sees it; results
// written before large outputs went to files are cut to their head and tail.
func providerVisibleToolResultContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "(empty result)"
	}
	return HeadTail(content, LargeOutputTokens)
}

func toProviderMessages(ctx context.Context, message transcript.Message, reader AttachmentReader, allowImageInput bool) ([]providers.Message, error) {
	if message.Role == transcript.MessageRoleSystem {
		if note, ok := engineNoteMessage(message); ok {
			return []providers.Message{note}, nil
		}
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
		content = providerVisibleToolResultContent(content)
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
	return withoutSignedReasoning(message)
}

// WithoutSignedReasoning drops the signed or encrypted reasoning of messages up
// to throughSeq, which was signed over a history since edited; plain reasoning
// stays. messages itself is not changed.
func WithoutSignedReasoning(messages []transcript.Message, throughSeq int64) []transcript.Message {
	if throughSeq == 0 {
		return messages
	}
	out := make([]transcript.Message, len(messages))
	for i, message := range messages {
		if message.Seq <= throughSeq {
			message = withoutSignedReasoning(message)
		}
		out[i] = message
	}
	return out
}

func withoutSignedReasoning(message transcript.Message) transcript.Message {
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
