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

type Usage struct {
	InputTokens     int64
	OutputTokens    int64
	TotalTokens     int64
	CachedTokens    int64
	ReasoningTokens int64
	ProviderRaw     json.RawMessage
}
