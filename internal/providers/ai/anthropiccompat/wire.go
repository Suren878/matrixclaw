package anthropic

import "encoding/json"

type anthropicRequest struct {
	Model      string               `json:"model"`
	MaxTokens  int64                `json:"max_tokens"`
	System     []anthropicBlock     `json:"system,omitempty"`
	Messages   []anthropicMessage   `json:"messages"`
	Tools      []anthropicTool      `json:"tools,omitempty"`
	ToolChoice *anthropicToolChoice `json:"tool_choice,omitempty"`
	Stream     bool                 `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

// anthropicBlock is any Messages API content block, sent or received.
// Thinking is a pointer so a thinking block always carries the key, even empty.
type anthropicBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text,omitempty"`
	Thinking     *string                `json:"thinking,omitempty"`
	Signature    string                 `json:"signature,omitempty"`
	Data         string                 `json:"data,omitempty"`
	ID           string                 `json:"id,omitempty"`
	Name         string                 `json:"name,omitempty"`
	Input        json.RawMessage        `json:"input,omitempty"`
	ToolUseID    string                 `json:"tool_use_id,omitempty"`
	Content      string                 `json:"content,omitempty"`
	IsError      bool                   `json:"is_error,omitempty"`
	Source       *anthropicImageSource  `json:"source,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicCacheControl struct {
	Type string `json:"type"`
}

type anthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  json.RawMessage        `json:"input_schema"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
}

type anthropicResponse struct {
	Content    []anthropicBlock      `json:"content"`
	StopReason string                `json:"stop_reason,omitempty"`
	Usage      anthropicUsagePayload `json:"usage"`
}
