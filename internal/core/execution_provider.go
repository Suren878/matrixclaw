package core

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (c *Core) buildProviderRequest(ctx context.Context, turn turnExecution) (providers.Request, error) {
	history, err := c.store.ListMessages(ctx, turn.SessionID, 0)
	if err != nil {
		return providers.Request{}, err
	}
	toolUseAllowed := runtimeToolUseAllowed(turn.Runtime)

	assistant := c.assistantProfile()
	compactSummary, effectiveHistory := latestCompactSummary(history)
	systemPrompt := AssistantSystemPrompt(assistant)
	if compactSummary != "" {
		systemPrompt = strings.TrimSpace(systemPrompt + "\n\nSession compact summary:\n" + compactSummary)
	}
	request := providers.Request{
		RunID:              turn.RunID,
		SessionID:          turn.SessionID,
		SystemPrompt:       systemPrompt,
		CustomInstructions: assistant.CustomInstructions,
	}
	if !toolUseAllowed {
		request.Messages = buildTextOnlyProviderConversation(effectiveHistory)
		request.Messages = providers.NormalizeMessages(request.Messages, providers.ToolUseDisabled)
		return request, nil
	}
	request.Messages, err = c.buildProviderConversation(ctx, effectiveHistory)
	if err != nil {
		return providers.Request{}, err
	}
	if c.tools != nil {
		specs := c.tools.List()
		request.Tools = make([]providers.ToolDefinition, 0, len(specs))
		for _, spec := range specs {
			request.Tools = append(request.Tools, providers.ToolDefinition{
				Name:        spec.ID,
				Description: spec.Description,
				InputSchema: spec.InputJSONSchema,
			})
		}
	}
	return request, nil
}

const maxProviderImageBytes int64 = 8 * 1024 * 1024

func runtimeToolUseMode(runtime providers.Runtime) (providers.ToolUseMode, bool) {
	profiler, ok := runtime.(providers.RuntimeProfiler)
	if !ok {
		return "", false
	}
	profile := providers.NormalizeRuntimeProfile(profiler.RuntimeProfile())
	return profile.ToolUseMode, true
}

func runtimeToolUseAllowed(runtime providers.Runtime) bool {
	if toolUseMode, ok := runtimeToolUseMode(runtime); ok && toolUseMode == providers.ToolUseDisabled {
		return false
	}
	capabilityProvider, ok := runtime.(providers.RuntimeCapabilityProvider)
	if !ok {
		return true
	}
	return capabilityProvider.ModelCapabilities().ToolCalling
}

func AssistantSystemPrompt(profile AssistantProfile) string {
	name := strings.Join(strings.Fields(profile.Name), " ")
	systemPrompt := strings.TrimSpace(profile.SystemPrompt)
	if name == "" {
		return systemPrompt
	}
	identity := fmt.Sprintf("Assistant identity:\n- Your configured assistant name is %q. Use this exact name when asked who you are. If older/default instructions mention a different assistant name, this configured name takes precedence.", name)
	if systemPrompt == "" {
		return identity
	}
	return identity + "\n\n" + systemPrompt
}

func buildProviderConversation(history []Message) []providers.Message {
	conversation, _ := buildProviderConversationWithAttachments(context.Background(), history, nil)
	return conversation
}

func (c *Core) buildProviderConversation(ctx context.Context, history []Message) ([]providers.Message, error) {
	return buildProviderConversationWithAttachments(ctx, history, c.attachments)
}

func buildProviderConversationWithAttachments(ctx context.Context, history []Message, reader AttachmentReader) ([]providers.Message, error) {
	conversation := make([]providers.Message, 0, len(history))
	toolResults := make(map[string]providers.Message)

	for _, message := range history {
		providerMessages, err := toProviderMessages(ctx, message, reader)
		if err != nil {
			return nil, err
		}
		for _, providerMessage := range providerMessages {
			if strings.TrimSpace(providerMessage.Role) != string(MessageRoleTool) {
				continue
			}
			toolCallID := strings.TrimSpace(providerMessage.ToolCallID)
			if toolCallID == "" {
				continue
			}
			if _, exists := toolResults[toolCallID]; exists {
				continue
			}
			toolResults[toolCallID] = providerMessage
		}
	}

	for i := 0; i < len(history); i++ {
		providerMessages, err := toProviderMessages(ctx, history[i], reader)
		if err != nil {
			return nil, err
		}
		if len(providerMessages) == 0 {
			continue
		}

		providerMessage := providerMessages[0]
		if strings.TrimSpace(providerMessage.Role) == string(MessageRoleTool) && strings.TrimSpace(providerMessage.ToolCallID) != "" {
			continue
		}

		if len(providerMessage.ToolCalls) == 0 || strings.TrimSpace(providerMessage.Content) != "" {
			conversation = append(conversation, providerMessages...)
			continue
		}

		batched := providerMessage
		for j := i + 1; j < len(history); j++ {
			nextMessages, err := toProviderMessages(ctx, history[j], reader)
			if err != nil {
				return nil, err
			}
			if len(nextMessages) != 1 {
				break
			}
			next := nextMessages[0]
			if strings.TrimSpace(next.Role) != string(MessageRoleAssistant) || len(next.ToolCalls) == 0 || strings.TrimSpace(next.Content) != "" {
				break
			}
			batched.ToolCalls = append(batched.ToolCalls, next.ToolCalls...)
			i = j
		}

		conversation = append(conversation, batched)
		for _, toolCall := range batched.ToolCalls {
			toolCallID := strings.TrimSpace(toolCall.ID)
			if toolCallID == "" {
				continue
			}
			if toolResult, ok := toolResults[toolCallID]; ok {
				conversation = append(conversation, toolResult)
				delete(toolResults, toolCallID)
				continue
			}
			conversation = append(conversation, providers.Message{
				Role:       string(MessageRoleTool),
				ToolCallID: toolCallID,
				Content:    "Tool execution failed before completion.",
			})
		}
	}
	return conversation, nil
}

func buildTextOnlyProviderConversation(history []Message) []providers.Message {
	conversation := make([]providers.Message, 0, len(history))
	for _, message := range history {
		if message.Role == MessageRoleSystem {
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
			Role:      string(message.Role),
			Content:   messageContentWithAttachmentRefs(message.Content, imageParts),
			Images:    images,
			ToolCalls: toolCalls,
		}}, nil
	}

	for _, part := range message.Parts {
		if part.ToolResult == nil {
			continue
		}
		return []providers.Message{{
			Role:       string(message.Role),
			Content:    part.ToolResult.Content,
			ToolCallID: strings.TrimSpace(part.ToolResult.ToolCallID),
		}}, nil
	}

	if strings.TrimSpace(message.Content) == "" && len(images) == 0 {
		return nil, nil
	}
	return []providers.Message{{
		Role:    string(message.Role),
		Content: messageContentWithAttachmentRefs(message.Content, imageParts),
		Images:  images,
	}}, nil
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
