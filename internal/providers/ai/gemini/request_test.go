package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func capturePayloads(t *testing.T, requests ...providers.Request) []generateContentRequest {
	t.Helper()
	var payloads []generateContentRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload generateContentRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		payloads = append(payloads, payload)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}` + "\n\n"))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range requests {
		if _, err := runtime.Generate(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	return payloads
}

func TestToolChoiceNoneAndOutputLimitReachTheWire(t *testing.T) {
	tools := []providers.ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	messages := []providers.Message{{Role: "user", Content: "hello"}}
	payloads := capturePayloads(t,
		providers.Request{Messages: messages, Tools: tools, ToolChoice: providers.ToolChoiceNone},
		providers.Request{Messages: messages, Tools: tools, MaxOutputTokens: 5000},
	)
	final, normal := payloads[0], payloads[1]
	if final.ToolConfig == nil || final.ToolConfig.FunctionCallingConfig.Mode != "NONE" || len(final.Tools) != 1 {
		t.Fatalf("final turn must keep tools and set mode NONE: %+v", final)
	}
	if final.GenerationConfig.MaxOutputTokens != providers.DefaultMaxOutputTokens {
		t.Fatalf("default maxOutputTokens=%d", final.GenerationConfig.MaxOutputTokens)
	}
	if normal.ToolConfig != nil || normal.GenerationConfig.MaxOutputTokens != 5000 {
		t.Fatalf("normal turn=%+v", normal)
	}
}

func TestListModelsRegistersOutputLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-maxout-test","supportedGenerationMethods":["generateContent"],"inputTokenLimit":1048576,"outputTokenLimit":8192}]}`))
	}))
	defer server.Close()
	if _, err := ListModels(context.Background(), Config{ProviderID: "gemini-maxout", APIKey: "test", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	metadata := providers.ResolveModelMetadata("gemini-maxout", providers.TypeGemini, "models/gemini-maxout-test")
	if metadata.MaxOutputTokens != 8192 || metadata.ContextWindow != 1048576 {
		t.Fatalf("metadata=%+v", metadata)
	}
}
