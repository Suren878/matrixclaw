package anthropic

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var emptyToolSchema = json.RawMessage(`{"type":"object","properties":{}}`)

const (
	maxImageBase64Bytes = 5 * 1024 * 1024 // the per-image limit of Bedrock and Vertex; the direct API allows 10 MB
	maxImageSide        = 8000
)

// encodeRequest fills system, tools, tool choice and messages of payload.
// Cache breakpoints are set only for session requests (non-empty CacheKey).
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
		markCacheBreakpoints(payload)
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
	ids := &toolIDs{byRaw: map[string]string{}, used: map[string]bool{}}
	out := make([]anthropicMessage, 0, len(messages))
	for _, message := range messages {
		role, blocks := encodeMessage(message, ids)
		if len(blocks) == 0 {
			continue
		}
		if last := len(out) - 1; last >= 0 && out[last].Role == role {
			out[last].Content = append(out[last].Content, blocks...)
			continue
		}
		out = append(out, anthropicMessage{Role: role, Content: blocks})
	}
	loopStart := currentToolLoopStart(out)
	for i := range out {
		if out[i].Role == "user" {
			out[i].Content = toolResultsFirst(out[i].Content)
		} else {
			out[i].Content = replayableThinking(out[i].Content, i > loopStart)
		}
	}
	if last := len(out) - 1; last >= 0 && out[last].Role == "assistant" {
		// The API rejects a final assistant turn that ends with whitespace.
		final := &out[last].Content[len(out[last].Content)-1]
		final.Text = strings.TrimRightFunc(final.Text, unicode.IsSpace)
	}
	return out
}

func encodeMessage(message providers.Message, ids *toolIDs) (string, []anthropicBlock) {
	switch strings.ToLower(strings.TrimSpace(message.Role)) {
	case "assistant":
		return "assistant", assistantBlocks(message, ids)
	case "tool":
		if id := ids.result(message.ToolCallID); id != "" {
			return "user", []anthropicBlock{toolResultBlock(id, message)}
		}
	}
	return "user", userBlocks(message)
}

// assistantBlocks replays only signed or redacted reasoning; unsigned reasoning
// from other providers cannot be verified by Anthropic and is not sent. Text is
// sent as stored, untrimmed.
func assistantBlocks(message providers.Message, ids *toolIDs) []anthropicBlock {
	ids.unnamed = nil
	hasText := strings.TrimSpace(message.Content) != ""
	if !hasText && len(message.ToolCalls) == 0 {
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
	if hasText {
		blocks = append(blocks, anthropicBlock{Type: "text", Text: message.Content})
	}
	for _, call := range message.ToolCalls {
		blocks = append(blocks, anthropicBlock{
			Type:  "tool_use",
			ID:    ids.call(call.ID),
			Name:  strings.TrimSpace(call.Name),
			Input: toolUseInput(call.Arguments),
		})
	}
	return blocks
}

func userBlocks(message providers.Message) []anthropicBlock {
	blocks := make([]anthropicBlock, 0, len(message.Images)+1)
	for _, img := range message.Images {
		if strings.TrimSpace(img.DataBase64) != "" {
			blocks = append(blocks, imageBlock(img))
		}
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

// currentToolLoopStart returns the index of the last user turn without tool
// results, i.e. the last real user message; tool results, even with steering
// text merged in, continue the loop. Thinking signatures cover a prefix that
// older history no longer matches, so thinking is replayed only after it.
func currentToolLoopStart(turns []anthropicMessage) int {
	for i := len(turns) - 1; i >= 0; i-- {
		isResult := func(block anthropicBlock) bool { return block.Type == "tool_result" }
		if turns[i].Role == "user" && !slices.ContainsFunc(turns[i].Content, isResult) {
			return i
		}
	}
	return -1
}

// toolResultsFirst puts tool results first in a user turn, as the API requires.
func toolResultsFirst(blocks []anthropicBlock) []anthropicBlock {
	ordered := make([]anthropicBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "tool_result" {
			ordered = append(ordered, block)
		}
	}
	for _, block := range blocks {
		if block.Type != "tool_result" {
			ordered = append(ordered, block)
		}
	}
	return ordered
}

// replayableThinking keeps thinking only in the current tool loop and only where
// it already leads its turn; blocks are never reordered.
func replayableThinking(blocks []anthropicBlock, inLoop bool) []anthropicBlock {
	kept := make([]anthropicBlock, 0, len(blocks))
	leading := true
	for _, block := range blocks {
		if block.Type == "thinking" || block.Type == "redacted_thinking" {
			if !inLoop || !leading {
				continue
			}
		} else {
			leading = false
		}
		kept = append(kept, block)
	}
	return kept
}

// stripThinking removes every thinking block and reports whether there was any.
func stripThinking(turns []anthropicMessage) bool {
	stripped := false
	for i := range turns {
		turns[i].Content = slices.DeleteFunc(turns[i].Content, func(block anthropicBlock) bool {
			thinking := block.Type == "thinking" || block.Type == "redacted_thinking"
			stripped = stripped || thinking
			return thinking
		})
	}
	return stripped
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

// toolIDs maps tool ids onto Anthropic's ^[a-zA-Z0-9_-]+$ within one request:
// distinct ids stay distinct, the same id always maps the same way, and a call
// without an id gets a synthetic one that the next id-less result answers.
type toolIDs struct {
	byRaw   map[string]string
	used    map[string]bool
	unnamed []string
}

func (t *toolIDs) call(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		id := t.claim("toolu_missing")
		t.unnamed = append(t.unnamed, id)
		return id
	}
	if id, ok := t.byRaw[raw]; ok {
		return id
	}
	id := t.claim(strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, raw))
	t.byRaw[raw] = id
	return id
}

func (t *toolIDs) result(raw string) string {
	if strings.TrimSpace(raw) != "" {
		return t.call(raw)
	}
	if len(t.unnamed) == 0 {
		return ""
	}
	id := t.unnamed[0]
	t.unnamed = t.unnamed[1:]
	return id
}

func (t *toolIDs) claim(base string) string {
	id := base
	for n := 2; t.used[id]; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	t.used[id] = true
	return id
}

// imageBlock sends an image the Messages API accepts and a note otherwise, since
// a rejected image in history would fail every later request of the session.
func imageBlock(img providers.ImageContent) anthropicBlock {
	data := strings.TrimSpace(img.DataBase64)
	mediaType, problem := checkImage(imageMediaType(img.MIMEType), data)
	if problem != "" {
		return anthropicBlock{Type: "text", Text: "[image omitted: " + problem + "]"}
	}
	return anthropicBlock{Type: "image", Source: &anthropicImageSource{Type: "base64", MediaType: mediaType, Data: data}}
}

// checkImage returns the media type to send (detected from the data for jpeg,
// png and gif) or why the image cannot be sent.
func checkImage(mediaType string, data string) (string, string) {
	switch mediaType {
	case "":
		return "", "missing media type"
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return "", "unsupported media type " + mediaType
	}
	if len(data) > maxImageBase64Bytes {
		return "", "larger than 5 MB"
	}
	if mediaType == "image/webp" {
		return mediaType, ""
	}
	config, format, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(data)))
	if err != nil {
		return "", "unreadable image data"
	}
	if config.Width > maxImageSide || config.Height > maxImageSide {
		return "", fmt.Sprintf("%dx%d px exceeds %d px", config.Width, config.Height, maxImageSide)
	}
	return "image/" + format, ""
}

func imageMediaType(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if index := strings.IndexByte(mimeType, ';'); index >= 0 {
		mimeType = strings.TrimSpace(mimeType[:index])
	}
	return mimeType
}

// markCacheBreakpoints caches the stable prefix and the conversation so far:
// the tools, the system prompt, the newest user turn and the one the previous
// request ended with; four breakpoints, the most the API allows.
func markCacheBreakpoints(payload *anthropicRequest) {
	if last := len(payload.Tools) - 1; last >= 0 {
		payload.Tools[last].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}
	if last := len(payload.System) - 1; last >= 0 {
		payload.System[last].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}
	marked := 0
	for i := len(payload.Messages) - 1; i >= 0 && marked < 2; i-- {
		content := payload.Messages[i].Content
		if payload.Messages[i].Role != "user" || len(content) == 0 {
			continue
		}
		content[len(content)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
		marked++
	}
}
