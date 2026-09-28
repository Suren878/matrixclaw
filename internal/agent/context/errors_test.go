package agentcontext

import (
	"errors"
	"fmt"
	"testing"
)

func TestContextLengthErrorsOfEveryProviderAreRecognised(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"anthropic", errors.New("anthropic: status 400: invalid_request_error: prompt is too long: 208310 tokens > 200000 maximum"), true},
		{"anthropic stream", errors.New("anthropic: stream error: invalid_request_error: prompt is too long: 1000512 tokens > 1000000 maximum"), true},
		{"gemini", errors.New("gemini: status 400: INVALID_ARGUMENT: The input token count (1236429) exceeds the maximum number of tokens allowed (1048575)."), true},
		{"openai chat", errors.New("openai: status 400: This model's maximum context length is 128000 tokens. However, your messages resulted in 130412 tokens. Please reduce the length of the messages."), true},
		{"openai code", errors.New(`openai: status 400: {"error":{"message":"Input too long.","type":"invalid_request_error","code":"context_length_exceeded"}}`), true},
		{"openai responses", errors.New("codex: response failed: Your input exceeds the context window of this model. Please adjust your input and try again."), true},
		{"openrouter", errors.New("openrouter: status 400: This endpoint's maximum context length is 131072 tokens. However, you requested about 140213 tokens (132021 of text input, 8192 in the output). Please reduce the length of either one, or use the \"middle-out\" transform to compress your prompt automatically."), true},
		{"deepseek", errors.New("deepseek: status 400: This model's maximum context length is 65536 tokens. However, you requested 70214 tokens (62022 in the messages, 8192 in the completion). Please reduce the length of the messages or completion."), true},
		{"xai", errors.New(`xai: status 400: {"code":"Client specified an invalid argument","error":"This model's maximum prompt length is 131072 but the request contains 149617 tokens."}`), true},
		{"mistral", errors.New("mistral: status 400: Prompt contains 40046 tokens and 0 draft tokens, too large for model with 32768 maximum context length"), true},
		{"wrapped", fmt.Errorf("generate: %w", errors.New("prompt is too long: 208310 tokens > 200000 maximum")), true},
		{"rate limit", errors.New("openai: status 429: Rate limit reached for gpt-4o on requests per min (RPM): Limit 500, Used 500, Requested 1."), false},
		{"auth", errors.New("anthropic: status 401: authentication_error: invalid x-api-key"), false},
		{"overloaded", errors.New("anthropic: status 529: overloaded_error: Overloaded"), false},
		{"nil", nil, false},
	} {
		if got := IsContextLengthExceeded(tc.err); got != tc.want {
			t.Errorf("%s: IsContextLengthExceeded = %t, want %t", tc.name, got, tc.want)
		}
	}
}
