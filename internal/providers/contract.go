package providers

import (
	"context"
	"encoding/json"
)

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ImageContent struct {
	MIMEType    string `json:"mime_type,omitempty"`
	DataBase64  string `json:"data_base64,omitempty"`
	Name        string `json:"name,omitempty"`
	StoragePath string `json:"storage_path,omitempty"`
	Temporary   bool   `json:"temporary,omitempty"`
	Size        int64  `json:"size,omitempty"`
}

type Message struct {
	Role             string
	Content          string
	ReasoningContent *string
	Images           []ImageContent
	ToolCallID       string
	ToolCalls        []ToolCall
}

type Request struct {
	RunID              string
	SessionID          string
	SystemPrompt       string
	CustomInstructions string
	Messages           []Message
	Tools              []ToolDefinition
}

type Response struct {
	Text             string
	ReasoningContent *string
	Model            string
	Provider         string
	ToolCalls        []ToolCall
	Usage            Usage
}

type Runtime interface {
	Generate(ctx context.Context, request Request) (Response, error)
}

type RuntimeProfiler interface {
	RuntimeProfile() RuntimeProfile
}

type RuntimeCapabilityProvider interface {
	ModelCapabilities() ModelCapabilities
}

// Usage is normalised by every adapter: PromptTokens is the whole input,
// cache reads and writes included; OutputTokens includes ReasoningTokens.
type Usage struct {
	PromptTokens     int64           `json:"prompt_tokens,omitempty"`
	OutputTokens     int64           `json:"output_tokens,omitempty"`
	CacheReadTokens  int64           `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64           `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  int64           `json:"reasoning_tokens,omitempty"`
	ProviderRaw      json.RawMessage `json:"provider_raw,omitempty"`
}

func (u Usage) IsZero() bool {
	return u.PromptTokens == 0 && u.OutputTokens == 0 && u.CacheReadTokens == 0 &&
		u.CacheWriteTokens == 0 && u.ReasoningTokens == 0 && len(u.ProviderRaw) == 0
}
