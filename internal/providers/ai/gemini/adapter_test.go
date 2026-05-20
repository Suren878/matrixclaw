package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestGenerateUsesNativeFunctionCalling(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-2.5-flash:generateContent" {
			t.Fatalf("path = %q, want native generateContent path", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "secret" {
			t.Fatalf("x-goog-api-key = %q, want secret", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		contents := body["contents"].([]any)
		if len(contents) != 3 {
			t.Fatalf("len(contents) = %d, want 3", len(contents))
		}
		modelParts := contents[1].(map[string]any)["parts"].([]any)
		if _, ok := modelParts[0].(map[string]any)["functionCall"]; !ok {
			t.Fatalf("assistant tool call = %#v, want functionCall", modelParts[0])
		}
		userParts := contents[2].(map[string]any)["parts"].([]any)
		functionResponse := userParts[0].(map[string]any)["functionResponse"].(map[string]any)
		if functionResponse["name"] != "read" {
			t.Fatalf("functionResponse.name = %#v, want read", functionResponse["name"])
		}
		tools := body["tools"].([]any)
		declarations := tools[0].(map[string]any)["functionDeclarations"].([]any)
		if declarations[0].(map[string]any)["name"] != "read" {
			t.Fatalf("function declaration = %#v, want read", declarations[0])
		}
		parameters := declarations[0].(map[string]any)["parameters"].(map[string]any)
		if _, ok := parameters["additionalProperties"]; ok {
			t.Fatalf("parameters still contain additionalProperties: %#v", parameters)
		}
		filePath := parameters["properties"].(map[string]any)["file_path"].(map[string]any)
		if _, ok := filePath["minimum"]; ok {
			t.Fatalf("nested schema still contains minimum: %#v", filePath)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{
					"parts": []map[string]any{{
						"functionCall": map[string]any{
							"name": "edit",
							"args": map[string]any{"file_path": "notes.txt"},
						},
					}},
				},
			}},
			"usageMetadata": map[string]any{
				"promptTokenCount":     11,
				"candidatesTokenCount": 3,
				"totalTokenCount":      14,
			},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "gemini-2.5-flash",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response, err := runtime.Generate(context.Background(), providers.Request{
		RunID: "run_1",
		Messages: []providers.Message{
			{Role: "user", Content: "read notes"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_read", Name: "read", Arguments: json.RawMessage(`{"file_path":"notes.txt"}`)}}},
			{Role: "tool", ToolCallID: "call_read", Content: "<file>old</file>"},
		},
		Tools: []providers.ToolDefinition{{
			Name:        "read",
			Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string","minimum":1}},"additionalProperties":false}`),
		}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if response.Provider != providers.TypeGemini {
		t.Fatalf("Provider = %q, want %q", response.Provider, providers.TypeGemini)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "edit" {
		t.Fatalf("ToolCalls = %#v, want one edit call", response.ToolCalls)
	}
	if response.ToolCalls[0].ID != "gemini_run_1_0_edit" {
		t.Fatalf("ToolCall ID = %q", response.ToolCalls[0].ID)
	}
	if got := string(response.ToolCalls[0].Arguments); got != `{"file_path":"notes.txt"}` {
		t.Fatalf("Arguments = %s", got)
	}
	if response.Usage.TotalTokens != 14 {
		t.Fatalf("Usage = %#v, want total 14", response.Usage)
	}
}

func TestGenerateSendsInlineImages(t *testing.T) {
	t.Parallel()

	imageData := base64.StdEncoding.EncodeToString([]byte("image bytes"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		parts := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
		if len(parts) != 2 {
			t.Fatalf("len(parts) = %d, want text and image", len(parts))
		}
		inlineData := parts[1].(map[string]any)["inlineData"].(map[string]any)
		if inlineData["mimeType"] != "image/png" || inlineData["data"] != imageData {
			t.Fatalf("inlineData = %#v, want image/png payload", inlineData)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{
					"parts": []map[string]any{{"text": "seen"}},
				},
			}},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "gemini-2.5-flash",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response, err := runtime.Generate(context.Background(), providers.Request{
		Messages: []providers.Message{{
			Role:    "user",
			Content: "what is this?",
			Images:  []providers.ImageContent{{MIMEType: "image/png", DataBase64: imageData}},
		}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if response.Text != "seen" {
		t.Fatalf("Text = %q, want seen", response.Text)
	}
}

func TestListModelsUsesNativeModelsEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "secret" {
			t.Fatalf("x-goog-api-key = %q, want secret", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{"name": "models/gemini-2.5-flash", "supportedGenerationMethods": []string{"generateContent"}, "inputTokenLimit": 1_048_576},
				{"name": "models/text-embedding-004", "supportedGenerationMethods": []string{"embedContent"}},
			},
		})
	}))
	defer server.Close()

	models, err := ListModels(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 1 || models[0] != "models/gemini-2.5-flash" {
		t.Fatalf("models = %#v, want one generateContent model", models)
	}
	if got := providers.ResolveContextWindowTokens("gemini", providers.TypeGemini, "gemini-2.5-flash"); got != 1_048_576 {
		t.Fatalf("ResolveContextWindowTokens() = %d, want Gemini inputTokenLimit", got)
	}
}

func TestGenerateSkipsThoughtParts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{
					"parts": []map[string]any{
						{"text": "private reasoning", "thought": true},
						{"text": "Hello!"},
					},
				},
			}},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "gemini-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response, err := runtime.Generate(context.Background(), providers.Request{
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if response.Text != "Hello!" {
		t.Fatalf("Text = %q, want visible non-thought response only", response.Text)
	}
}

func TestGenerateRetriesTransientServerError(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"message": "overloaded"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{
					"parts": []map[string]any{{"text": "ok"}},
				},
			}},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "gemini-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response, err := runtime.Generate(context.Background(), providers.Request{
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if response.Text != "ok" {
		t.Fatalf("Text = %q, want ok", response.Text)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}
