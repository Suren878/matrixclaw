package openaicompat

import (
	"context"
	"encoding/json"
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
