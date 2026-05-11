package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestModelsReturnsErrorWhenListingIsUnsupported(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("path = %q, want /models", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "not found"},
		})
	}))
	defer server.Close()

	_, err := Models(context.Background(), ModelDiscoveryInput{
		ID:        "openai",
		CatalogID: "openai",
		Type:      providers.TypeOpenAICompat,
		BaseURL:   server.URL,
		APIKey:    "secret",
		Model:     "gpt-5.4-mini",
	})
	if err == nil {
		t.Fatal("Models() error = nil, want model listing failure")
	}
}

func TestModelsReturnsErrorWhenRemoteValidationFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "invalid api key"},
		})
	}))
	defer server.Close()

	_, err := Models(context.Background(), ModelDiscoveryInput{
		ID:        "deepseek",
		CatalogID: "deepseek",
		Type:      providers.TypeOpenAICompat,
		BaseURL:   server.URL,
		APIKey:    "bad-key",
		Model:     "deepseek-chat",
	})
	if err == nil {
		t.Fatal("Models() error = nil, want validation failure")
	}
}

func TestModelsReturnsErrorOnServerFailureInsteadOfPretendingKeyWorks(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "temporary failure"},
		})
	}))
	defer server.Close()

	_, err := Models(context.Background(), ModelDiscoveryInput{
		ID:        "openai",
		CatalogID: "openai",
		Type:      providers.TypeOpenAICompat,
		BaseURL:   server.URL,
		APIKey:    "secret",
		Model:     "gpt-5.4-mini",
	})
	if err == nil {
		t.Fatal("Models() error = nil, want server failure")
	}
}

func TestModelsUsesNativeGeminiEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "gemini-key" {
			t.Fatalf("x-goog-api-key = %q, want gemini-key", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{"name": "models/gemini-2.5-flash", "supportedGenerationMethods": []string{"generateContent"}},
				{"name": "models/gemini-2.5-pro", "supportedGenerationMethods": []string{"generateContent"}},
				{"name": "models/text-embedding-004", "supportedGenerationMethods": []string{"embedContent"}},
			},
		})
	}))
	defer server.Close()

	models, err := Models(context.Background(), ModelDiscoveryInput{
		ID:        "gemini",
		CatalogID: "gemini",
		Type:      providers.TypeGemini,
		BaseURL:   server.URL,
		APIKey:    "gemini-key",
		Model:     providers.DefaultGeminiModel,
	})
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2: %#v", len(models), models)
	}
	if models[0] != "gemini-2.5-flash" || models[1] != "gemini-2.5-pro" {
		t.Fatalf("models = %#v, want normalized Gemini model ids", models)
	}
}
