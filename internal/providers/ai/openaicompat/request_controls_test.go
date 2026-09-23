package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func captureChatServer(t *testing.T, bodies *[]map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		*bodies = append(*bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRequestControlsReachTheWire(t *testing.T) {
	var bodies []map[string]any
	server := captureChatServer(t, &bodies)
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "controls-model"})
	if err != nil {
		t.Fatal(err)
	}
	tools := []providers.ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	messages := []providers.Message{{Role: "user", Content: "hello"}}
	if _, err := runtime.Generate(context.Background(), providers.Request{Messages: messages, Tools: tools, ToolChoice: providers.ToolChoiceNone, MaxOutputTokens: 3000, CacheKey: "session-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Generate(context.Background(), providers.Request{Messages: messages, Tools: tools, CacheKey: "session-1"}); err != nil {
		t.Fatal(err)
	}
	if bodies[0]["tool_choice"] != "none" || bodies[0]["max_tokens"] != float64(3000) {
		t.Fatalf("final-turn request=%v", bodies[0])
	}
	if _, ok := bodies[1]["tool_choice"]; ok || bodies[1]["max_tokens"] != float64(providers.DefaultMaxOutputTokens) {
		t.Fatalf("default request=%v", bodies[1])
	}
	if _, ok := bodies[0]["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key sent to a gateway not known to accept it: %v", bodies[0])
	}
}

func TestPromptCacheKeyUsesTheSessionKey(t *testing.T) {
	request := providers.Request{CacheKey: "session-1", Messages: []providers.Message{{Role: "user", Content: "hello"}}}
	if got := (&Runtime{model: "gpt-5.4", promptCacheKey: true}).chatPayload(context.Background(), request).PromptCacheKey; got != "session-1" {
		t.Fatalf("prompt_cache_key=%q, want session-1", got)
	}
}

func TestGenerateDropsARejectedOutputLimit(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		if _, limited := body["max_tokens"]; limited {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid max_tokens value, the valid range of max_tokens is [1, 8192]"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "small-limit-model"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if err != nil || response.Text != "ok" || len(bodies) != 2 {
		t.Fatalf("response=%+v err=%v requests=%d", response, err, len(bodies))
	}
}

func TestGenerateDropsAnUnsupportedOutputLimitField(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		if _, has := body["max_tokens"]; has {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model."}}`))
			return
		}
		if _, has := body["max_completion_tokens"]; has {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unsupported parameter: 'max_completion_tokens' is not supported with this model."}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "unsupported-field-model"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if err != nil || response.Text != "ok" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	final := bodies[len(bodies)-1]
	if _, ok := final["max_tokens"]; ok {
		t.Fatalf("final request still sent max_tokens: %v", final)
	}
	if _, ok := final["max_completion_tokens"]; ok {
		t.Fatalf("final request still sent max_completion_tokens: %v", final)
	}
}

func TestGenerateRemembersALearnedOutputLimit(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		if maxTokens, ok := body["max_tokens"].(float64); ok && maxTokens > 8192 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"max_tokens must be <= 8192"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "learned-limit-model"})
	if err != nil {
		t.Fatal(err)
	}
	request := providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}}
	if _, err := runtime.Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	requestsAfterFirstCall := len(bodies)
	if _, err := runtime.Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if got := len(bodies) - requestsAfterFirstCall; got != 1 {
		t.Fatalf("second call made %d requests, want 1", got)
	}
	if got := bodies[len(bodies)-1]["max_tokens"]; got != float64(8192) {
		t.Fatalf("second call max_tokens=%v, want 8192", got)
	}
}

func TestMaxTokensRejectionLearnsACapBelowTheSentValue(t *testing.T) {
	for _, tc := range []struct {
		name      string
		message   string
		sent      int64
		wantRetry bool
		wantCap   int64
		wantOmit  bool
	}{
		{"range brackets", "the valid range of max_tokens is [1, 8192]", 16384, true, 8192, false},
		{"must be less than or equal to", "max_tokens must be <= 8192", 16384, true, 8192, false},
		{"sent value repeated alongside the range", "max_tokens 16384 exceeds the limit [1, 8192]", 16384, true, 8192, false},
		{"vllm context length, not an output cap", "'max_tokens' is too large: 16384. This model's maximum context length is 32768 tokens and your request has 20000 input tokens", 16384, true, 0, false},
		{"unsupported field", "Unsupported parameter: 'max_tokens' is not supported with this model.", 16384, true, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sent := tc.sent
			payload := chatCompletionRequest{MaxTokens: &sent}
			body := []byte(fmt.Sprintf(`{"error":{"message":%q}}`, tc.message))
			retry, capTokens, omit := maxTokensRejection(payload, http.StatusBadRequest, body)
			if retry != tc.wantRetry || capTokens != tc.wantCap || omit != tc.wantOmit {
				t.Fatalf("maxTokensRejection=(%v,%v,%v), want (%v,%v,%v)", retry, capTokens, omit, tc.wantRetry, tc.wantCap, tc.wantOmit)
			}
		})
	}
}

func TestListModelsRegistersOutputLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/maxout-model","context_length":200000,"top_provider":{"max_completion_tokens":8000}}]}`))
	}))
	defer server.Close()
	if _, err := ListModels(context.Background(), Config{ProviderID: "maxout-router", APIKey: "test", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if got := providers.ResolveModelMetadata("maxout-router", providers.TypeOpenAICompat, "vendor/maxout-model").MaxOutputTokens; got != 8000 {
		t.Fatalf("catalog max output=%d, want 8000", got)
	}
}
