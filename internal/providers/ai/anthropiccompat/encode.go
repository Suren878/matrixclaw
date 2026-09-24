package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var emptyToolSchema = json.RawMessage(`{"type":"object","properties":{}}`)

// encodeRequest fills system, tools, tool choice and messages of payload.
// The tools cache breakpoint is set only for session requests (non-empty CacheKey).
func encodeRequest(payload *anthropicRequest, request providers.Request) error {
	if system := combinedSystemPrompt(request.SystemPrompt, request.CustomInstructions); system != "" {
		payload.System = []anthropicBlock{{Type: "text", Text: system}}
	}
	payload.Tools = encodeTools(request.Tools)
	if len(payload.Tools) > 0 && request.ToolChoice == providers.ToolChoiceNone {
		payload.ToolChoice = &anthropicToolChoice{Type: "none"}
	}
	payload.Messages = encodeMessages(request.Messages)
	if len(payload.Messages) == 0 {
		return errors.New("anthropic: no messages")
	}
	if strings.TrimSpace(request.CacheKey) != "" {
		markToolsCacheBreakpoint(payload.Tools)
	}
	return nil
}

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

func encodeTools(tools []providers.ToolDefinition) []anthropicTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]anthropicTool, 0, len(tools))
	for _, tool := range tools {
		schema := json.RawMessage(bytes.TrimSpace(tool.InputSchema))
		if len(schema) == 0 {
			schema = emptyToolSchema
		}
		out = append(out, anthropicTool{Name: tool.Name, Description: tool.Description, InputSchema: schema})
	}
	return out
}

// encodeMessages merges consecutive same-role messages into one turn, so the
// results of one tool step and any user text after them form one user turn.
func encodeMessages(messages []providers.Message) []anthropicMessage {
	out := make([]anthropicMessage, 0, len(messages))
	for _, message := range messages {
		role, blocks := encodeMessage(message)
		if len(blocks) == 0 {
			continue
		}
		if last := len(out) - 1; last >= 0 && out[last].Role == role {
			out[last].Content = orderTurnBlocks(role, append(out[last].Content, blocks...))
			continue
		}
		out = append(out, anthropicMessage{Role: role, Content: orderTurnBlocks(role, blocks)})
	}
	return out
}

func encodeMessage(message providers.Message) (string, []anthropicBlock) {
	switch strings.ToLower(strings.TrimSpace(message.Role)) {
	case "assistant":
		return "assistant", assistantBlocks(message)
	case "tool":
		if id := sanitizeToolID(message.ToolCallID); id != "" {
			return "user", []anthropicBlock{toolResultBlock(id, message)}
		}
	}
	return "user", userBlocks(message)
}

// assistantBlocks replays only signed or redacted reasoning; unsigned reasoning
// from other providers cannot be verified by Anthropic and is not sent.
func assistantBlocks(message providers.Message) []anthropicBlock {
	text := strings.TrimSpace(message.Content)
	if text == "" && len(message.ToolCalls) == 0 {
		return nil
	}
	blocks := make([]anthropicBlock, 0, len(message.Reasoning)+1+len(message.ToolCalls))
	for _, reasoning := range message.Reasoning {
		switch {
		case reasoning.RedactedData != "":
			blocks = append(blocks, anthropicBlock{Type: "redacted_thinking", Data: reasoning.RedactedData})
		case reasoning.Signature != "":
			thinking := reasoning.Text
			blocks = append(blocks, anthropicBlock{Type: "thinking", Thinking: &thinking, Signature: reasoning.Signature})
		}
	}
	if text != "" {
		blocks = append(blocks, anthropicBlock{Type: "text", Text: text})
	}
	for _, call := range message.ToolCalls {
		blocks = append(blocks, anthropicBlock{
			Type:  "tool_use",
			ID:    sanitizeToolID(call.ID),
			Name:  strings.TrimSpace(call.Name),
			Input: toolUseInput(call.Arguments),
		})
	}
	return blocks
}

func userBlocks(message providers.Message) []anthropicBlock {
	blocks := make([]anthropicBlock, 0, len(message.Images)+1)
	for _, image := range message.Images {
		data := strings.TrimSpace(image.DataBase64)
		if data == "" {
			continue
		}
		blocks = append(blocks, anthropicBlock{
			Type:   "image",
			Source: &anthropicImageSource{Type: "base64", MediaType: imageMediaType(image.MIMEType), Data: data},
		})
	}
	if text := strings.TrimSpace(message.Content); text != "" {
		blocks = append(blocks, anthropicBlock{Type: "text", Text: text})
	}
	return blocks
}

func toolResultBlock(toolUseID string, message providers.Message) anthropicBlock {
	content := strings.TrimSpace(message.Content)
	if content == "" {
		content = "(empty result)"
	}
	return anthropicBlock{Type: "tool_result", ToolUseID: toolUseID, Content: content, IsError: message.IsError}
}

// orderTurnBlocks puts thinking first in an assistant turn and tool results
// first in a user turn, as the Messages API requires.
func orderTurnBlocks(role string, blocks []anthropicBlock) []anthropicBlock {
	leads := func(block anthropicBlock) bool {
		if role == "assistant" {
			return block.Type == "thinking" || block.Type == "redacted_thinking"
		}
		return block.Type == "tool_result"
	}
	ordered := make([]anthropicBlock, 0, len(blocks))
	for _, block := range blocks {
		if leads(block) {
			ordered = append(ordered, block)
		}
	}
	for _, block := range blocks {
		if !leads(block) {
			ordered = append(ordered, block)
		}
	}
	return ordered
}

// toolUseInput returns the call arguments as a JSON object; anything else is
// sent as {} because the Messages API rejects non-object input.
func toolUseInput(arguments json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(arguments)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(trimmed)
}

// sanitizeToolID maps ids from other providers onto Anthropic's ^[a-zA-Z0-9_-]+$.
func sanitizeToolID(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, strings.TrimSpace(id))
}

func imageMediaType(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if index := strings.IndexByte(mimeType, ';'); index >= 0 {
		mimeType = strings.TrimSpace(mimeType[:index])
	}
	if mimeType == "" {
		return "image/jpeg"
	}
	return mimeType
}

// markToolsCacheBreakpoint caches the tool definitions: they open the cached
// prefix and stay the same across the turns of a session.
func markToolsCacheBreakpoint(tools []anthropicTool) {
	if last := len(tools) - 1; last >= 0 {
		tools[last].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}
}
