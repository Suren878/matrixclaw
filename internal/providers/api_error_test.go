package providers

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
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
		{"openai tpm", errors.New("openai: status 429: Request too large for gpt-4o in organization org-abc on tokens per min (TPM): Limit 30000, Requested 45210. The input or output tokens must be reduced in order to run successfully."), false},
		{"groq tpm", errors.New("groq: status 413: Request too large for model `llama-3.3-70b-versatile` in organization `org_01` service tier `on_demand` on tokens per minute (TPM): Limit 12000, Requested 18532, please reduce your message size and try again. Need more tokens? Upgrade to Dev Tier today at https://console.groq.com/settings/billing"), false},
		{"rate_limit code", errors.New(`openai: status 429: {"error":{"message":"You exceeded the token limit for this period","type":"tokens","code":"rate_limit_exceeded"}}`), false},
		{"auth", errors.New("anthropic: status 401: authentication_error: invalid x-api-key"), false},
		{"overloaded", errors.New("anthropic: status 529: overloaded_error: Overloaded"), false},
		{"nil", nil, false},
	} {
		if got := IsContextOverflow(tc.err); got != tc.want {
			t.Errorf("%s: IsContextOverflow = %t, want %t", tc.name, got, tc.want)
		}
	}
}

func TestAPIErrorKindFollowsStatusThenWording(t *testing.T) {
	header := http.Header{"Retry-After": {"7"}}
	for _, tc := range []struct {
		status    int
		message   string
		kind      ErrorKind
		retryable bool
	}{
		{429, "Rate limit reached", ErrorRateLimit, true},
		{529, "overloaded_error: Overloaded", ErrorOverloaded, true},
		{503, "", ErrorOverloaded, true},
		{401, "invalid x-api-key", ErrorAuth, false},
		{400, "prompt is too long: 208310 tokens > 200000 maximum", ErrorContextOverflow, false},
		{413, "Request too large for model on tokens per minute (TPM)", ErrorInvalid, false},
		{400, "unknown parameter", ErrorInvalid, false},
	} {
		err := NewAPIError("p", tc.status, tc.message, header)
		if err.Kind != tc.kind || err.Retryable() != tc.retryable || IsRetryableGenerationError(fmt.Errorf("wrapped: %w", err)) != tc.retryable {
			t.Errorf("%d %q: kind=%s retryable=%v, want %s %v", tc.status, tc.message, err.Kind, err.Retryable(), tc.kind, tc.retryable)
		}
		if err.RetryAfter != 7*time.Second {
			t.Errorf("%d: retry after %s, want 7s", tc.status, err.RetryAfter)
		}
	}
	if !IsContextOverflow(fmt.Errorf("x: %w", NewAPIError("p", 400, "context_length_exceeded", nil))) {
		t.Error("typed overflow not recognised")
	}
	if IsContextOverflow(NewAPIError("p", 503, "maximum context length", nil)) {
		t.Error("a typed server error was read from its wording")
	}
}
