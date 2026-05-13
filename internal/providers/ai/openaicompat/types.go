package openaicompat

import "encoding/json"

type chatCompletionRequest struct {
	Model               string                  `json:"model"`
	Messages            []chatCompletionMessage `json:"messages"`
	Tools               []chatCompletionTool    `json:"tools,omitempty"`
	ToolChoice          string                  `json:"tool_choice,omitempty"`
	MaxTokens           *int64                  `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int64                  `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string                  `json:"reasoning_effort,omitempty"`
	Stream              bool                    `json:"stream,omitempty"`
}

type chatCompletionMessage struct {
	Role             string                   `json:"role"`
	Content          any                      `json:"content"`
	ReasoningContent *string                  `json:"reasoning_content,omitempty"`
	ToolCallID       string                   `json:"tool_call_id,omitempty"`
	ToolCalls        []chatCompletionToolCall `json:"tool_calls,omitempty"`
}

type chatCompletionContentPart struct {
	Type     string                         `json:"type"`
	Text     string                         `json:"text,omitempty"`
	ImageURL *chatCompletionContentImageURL `json:"image_url,omitempty"`
}

type chatCompletionContentImageURL struct {
	URL string `json:"url"`
}

type chatCompletionTool struct {
	Type     string                       `json:"type"`
	Function chatCompletionToolDefinition `json:"function"`
}

type chatCompletionToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatCompletionToolCall struct {
	ID       string                         `json:"id,omitempty"`
	Type     string                         `json:"type,omitempty"`
	Function chatCompletionToolFunctionCall `json:"function"`
}

type chatCompletionToolFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content          string                   `json:"content"`
			ReasoningContent *string                  `json:"reasoning_content,omitempty"`
			ToolCalls        []chatCompletionToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage chatCompletionUsage `json:"usage,omitempty"`
}

type chatCompletionChunk struct {
	Choices []struct {
		Delta struct {
			Content          string                        `json:"content"`
			ReasoningContent *string                       `json:"reasoning_content,omitempty"`
			ToolCalls        []chatCompletionToolCallDelta `json:"tool_calls,omitempty"`
		} `json:"delta"`
		Message struct {
			Content          string  `json:"content"`
			ReasoningContent *string `json:"reasoning_content,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage chatCompletionUsage `json:"usage,omitempty"`
}

type chatCompletionUsage struct {
	PromptTokens        int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens    int64 `json:"completion_tokens,omitempty"`
	TotalTokens         int64 `json:"total_tokens,omitempty"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens,omitempty"`
	} `json:"completion_tokens_details,omitempty"`
}

type chatCompletionToolCallDelta struct {
	Index    int                            `json:"index"`
	ID       string                         `json:"id,omitempty"`
	Type     string                         `json:"type,omitempty"`
	Function chatCompletionToolFunctionCall `json:"function,omitempty"`
}
