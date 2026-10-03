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
	Reasoning        []ReasoningBlock // signed or encrypted reasoning, sent back unchanged
	Images           []ImageContent
	ToolCallID       string
	ToolCalls        []ToolCall
	IsError          bool // a failed tool result
}

// ReasoningBlock is provider reasoning that must be sent back as received.
// Signature: Anthropic thinking signature or Gemini thought signature.
// RedactedData: Anthropic redacted thinking or Responses API encrypted reasoning.
type ReasoningBlock struct {
	Text         string
	Signature    string
	RedactedData string
}

type Request struct {
	SystemPrompt    string
	Messages        []Message
	Tools           []ToolDefinition
	MaxOutputTokens int        // 0 = provider config, then model catalog, then DefaultMaxOutputTokens
	ToolChoice      ToolChoice // tools stay defined with ToolChoiceNone so the cached prefix survives
	CacheKey        string     // session id; adapters use it for prompt_cache_key or cache breakpoints
}

type Response struct {
	Text             string
	ReasoningContent *string
	Model            string
	Provider         string
	Reasoning        []ReasoningBlock
	ToolCalls        []ToolCall
	StopReason       StopReason
	Usage            Usage
}

// StopReason says why generation ended, normalised across providers.
type StopReason string

const (
	StopEndTurn       StopReason = "end_turn"
	StopToolUse       StopReason = "tool_use"
	StopMaxTokens     StopReason = "max_tokens"
	StopRefusal       StopReason = "refusal"
	StopContentFilter StopReason = "content_filter"
)

// ToolChoice limits tool use for one request.
type ToolChoice string

const (
	ToolChoiceAuto ToolChoice = ""
	ToolChoiceNone ToolChoice = "none"
)

type Runtime interface {
	Generate(ctx context.Context, request Request) (Response, error)
}

type RuntimeCapabilityProvider interface {
	ModelCapabilities() ModelCapabilities
}

// RuntimeIdentifier reports the Provider and Model a runtime puts on its
// responses, so signed reasoning is replayed only to the model that made it.
type RuntimeIdentifier interface {
	Identity() (provider string, model string)
}

// OutputLimiter reports the output limit a runtime sends when a request names
// none and the most its model accepts (0 = unknown). A runtime without it sends
// no limit the engine could raise.
type OutputLimiter interface {
	OutputLimits() (current int64, ceiling int64)
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
