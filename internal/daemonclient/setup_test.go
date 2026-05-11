package daemonclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestClientSetupProviderContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if got := r.URL.Query().Get("client"); got != "tui" {
			t.Errorf("client query = %q, want tui", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/setup/providers":
			_ = json.NewEncoder(w).Encode(setup.ProviderSetupListResponse{
				Providers: []setup.ProviderSetupItem{{ID: "openai", Name: "OpenAI"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/setup/providers/anthropic":
			var update setup.ProviderSetupUpdate
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				t.Errorf("Decode update error = %v", err)
			}
			want := setup.ProviderSetupUpdate{
				Name:        "Anthropic Claude",
				Type:        "anthropic-compatible",
				APIKey:      "secret",
				BaseURL:     "https://api.anthropic.com",
				Model:       "claude-sonnet-4-5",
				ToolUseMode: providers.ToolUseDisabled,
				Active:      true,
			}
			if !reflect.DeepEqual(update, want) {
				t.Errorf("update = %#v, want %#v", update, want)
			}
			_ = json.NewEncoder(w).Encode(setup.ProviderSetupResponse{
				Provider: setup.ProviderSetupItem{ID: "anthropic", Name: "Anthropic", Configured: true},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/setup/providers/local-ai":
			_ = json.NewEncoder(w).Encode(setup.ProviderSetupOKResponse{OK: true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, "tui", "local")
	setupProviders, err := client.ListSetupProviders(context.Background())
	if err != nil {
		t.Fatalf("ListSetupProviders() error = %v", err)
	}
	if len(setupProviders) != 1 || setupProviders[0].ID != "openai" {
		t.Fatalf("ListSetupProviders() = %#v, want OpenAI", setupProviders)
	}

	provider, err := client.ConfigureSetupProvider(context.Background(), "anthropic", setup.ProviderSetupUpdate{
		Name:        "Anthropic Claude",
		Type:        "anthropic-compatible",
		APIKey:      "secret",
		BaseURL:     "https://api.anthropic.com",
		Model:       "claude-sonnet-4-5",
		ToolUseMode: providers.ToolUseDisabled,
		Active:      true,
	})
	if err != nil {
		t.Fatalf("ConfigureSetupProvider() error = %v", err)
	}
	if provider.ID != "anthropic" || !provider.Configured {
		t.Fatalf("ConfigureSetupProvider() = %#v, want configured Anthropic", provider)
	}

	if err := client.DeleteSetupProvider(context.Background(), "local-ai"); err != nil {
		t.Fatalf("DeleteSetupProvider() error = %v", err)
	}
}
