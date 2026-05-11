package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/store"
)

func TestHandleSetupProvidersReturnsConfiguredAndAvailable(t *testing.T) {
	openai := openAIProviderDraft()
	openai.Model = "gpt-5.4"
	openai.APIKey = ""
	openai.StoredAPIKeyPreview = "****1234"

	setupService := newSetupService(t, setup.Draft{
		ActiveProviderID: "openai",
		Providers:        []setup.ProviderDraft{openai},
	})
	httpServer := newSetupProviderHTTPServer(t, newTestCore(t), setupService, nil)

	resp, err := http.Get(httpServer.URL + "/v1/setup/providers")
	if err != nil {
		t.Fatalf("GET /v1/setup/providers error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/setup/providers status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var payload setup.ProviderSetupListResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode providers error = %v", err)
	}
	if len(payload.Providers) < 2 {
		t.Fatalf("providers len = %d, want configured plus available entries", len(payload.Providers))
	}

	first := payload.Providers[0]
	if first.ID != "openai" || !first.Configured || !first.Active || first.APIKeyPreview != "****1234" {
		t.Fatalf("first provider = %#v, want default configured OpenAI with masked key", first)
	}
	if first.Status != "Configured · gpt-5.4 · Active" {
		t.Fatalf("first provider status = %q", first.Status)
	}
}

func TestHandleSetupProviderPatchConfiguresAndReloads(t *testing.T) {
	setupService := newSetupService(t, setup.Draft{
		ActiveProviderID: "openai",
		Providers:        []setup.ProviderDraft{openAIProviderDraft()},
	})

	reloads := 0
	app := newTestCore(t).WithSessionLLMs(sessionLLMsFromSetupConfig(setup.Config{
		ActiveProviderID: "openai",
		Providers: []setup.ProviderConfig{{
			ID:      "openai",
			Name:    "OpenAI",
			Type:    "openai-compatible",
			Model:   "gpt-5.4-mini",
			BaseURL: "https://api.openai.com/v1",
			APIKey:  "openai-secret",
		}},
	}))
	httpServer := newSetupProviderHTTPServer(t, app, setupService, func(server *Server) {
		server.SetAdminReload(func(_ context.Context) error {
			reloads++
			cfg, err := setupService.Load()
			if err != nil {
				return err
			}
			app.SetSessionLLMs(sessionLLMsFromSetupConfig(cfg))
			return nil
		})
	})

	reqBody := bytes.NewBufferString(`{"api_key":"anthropic-secret","active":true}`)
	req, err := http.NewRequest(http.MethodPatch, httpServer.URL+"/v1/setup/providers/anthropic", reqBody)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /v1/setup/providers/anthropic error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if reloads != 1 {
		t.Fatalf("reloads = %d, want 1", reloads)
	}

	cfg, err := setupService.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ActiveProviderID != "anthropic" {
		t.Fatalf("ActiveProviderID = %q, want anthropic", cfg.ActiveProviderID)
	}
	if provider, ok := setup.ActiveProviderConfig(cfg); !ok || provider.APIKey != "anthropic-secret" {
		t.Fatalf("active provider = %#v, ok=%v", provider, ok)
	}
}

func TestHandleSetupProviderPatchIgnoresLegacyToolFields(t *testing.T) {
	setupService := newSetupService(t, setup.Draft{})
	httpServer := newSetupProviderHTTPServer(t, newTestCore(t), setupService, nil)

	reqBody := bytes.NewBufferString(`{"name":"Local AI","type":"openai-compatible","api_key":"test-api-key","base_url":"http://127.0.0.1:11434/v1","model":"llama3","tool_use_mode":"prompted","tool_schema_dialect":"gemini","active":true}`)
	req, err := http.NewRequest(http.MethodPatch, httpServer.URL+"/v1/setup/providers/local-ai", reqBody)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /v1/setup/providers/local-ai error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var responsePayload map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&responsePayload); err != nil {
		t.Fatalf("Decode response error = %v", err)
	}
	var responseProvider map[string]json.RawMessage
	if err := json.Unmarshal(responsePayload["provider"], &responseProvider); err != nil {
		t.Fatalf("Decode provider response error = %v", err)
	}
	if _, ok := responseProvider["tool_schema_dialect"]; ok {
		t.Fatalf("provider response exposes legacy tool_schema_dialect: %#v", responseProvider)
	}

	cfg, err := setupService.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	provider, ok := setup.ActiveProviderConfig(cfg)
	if !ok {
		t.Fatal("active provider not found")
	}
	if provider.ToolUseMode != "" {
		t.Fatalf("ToolUseMode = %q, want unsupported prompted mode dropped", provider.ToolUseMode)
	}
}

func TestHandleSetupProviderDeleteRemovesCustomAndReloads(t *testing.T) {
	setupService := newSetupService(t, setup.Draft{
		ActiveProviderID: "local-ai",
		Providers: []setup.ProviderDraft{
			openAIProviderDraft(),
			{
				ID:              "local-ai",
				Name:            "Local AI",
				Type:            "openai-compatible",
				Model:           "llama3",
				BaseURL:         "http://127.0.0.1:11434/v1",
				APIKey:          "local-secret",
				HasStoredAPIKey: true,
			},
		},
	})

	reloads := 0
	httpServer := newSetupProviderHTTPServer(t, newTestCore(t), setupService, func(server *Server) {
		server.SetAdminReload(func(context.Context) error {
			reloads++
			return nil
		})
	})

	req, err := http.NewRequest(http.MethodDelete, httpServer.URL+"/v1/setup/providers/local-ai", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /v1/setup/providers/local-ai error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if reloads != 1 {
		t.Fatalf("reloads = %d, want 1", reloads)
	}
	cfg, err := setupService.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ActiveProviderID != "openai" || len(cfg.Providers) != 1 || cfg.Providers[0].ID != "openai" {
		t.Fatalf("config after delete = %#v, want only OpenAI", cfg)
	}
}

func TestSetupProvidersUseSetupClientPolicy(t *testing.T) {
	setupService := newSetupService(t, setup.Draft{
		ActiveProviderID:      "openai",
		Providers:             []setup.ProviderDraft{openAIProviderDraft()},
		TelegramEnabled:       "yes",
		TelegramBotToken:      "bot-token",
		TelegramAllowedUID:    "123",
		TelegramProviderSetup: "no",
	})
	httpServer := newSetupProviderHTTPServer(t, newTestCore(t), setupService, nil)

	resp, err := http.Get(httpServer.URL + "/v1/setup/providers?client=telegram")
	if err != nil {
		t.Fatalf("GET setup providers error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET setup providers status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var payload setup.ProviderSetupListResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode providers error = %v", err)
	}
	if len(payload.Providers) != 1 || payload.Providers[0].ID != "openai" {
		t.Fatalf("telegram providers = %#v, want only configured OpenAI", payload.Providers)
	}

	resp, err = http.Get(httpServer.URL + "/v1/setup/providers?client=custom")
	if err != nil {
		t.Fatalf("GET setup providers for custom client error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET custom client setup providers status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	payload = setup.ProviderSetupListResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode custom client providers error = %v", err)
	}
	if len(payload.Providers) < 2 {
		t.Fatalf("custom client providers = %#v, want configured plus available providers", payload.Providers)
	}

	req, err := http.NewRequest(http.MethodPatch, httpServer.URL+"/v1/setup/providers/anthropic?client=telegram", bytes.NewBufferString(`{"api_key":"secret"}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH setup provider error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("PATCH status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	customSetupService := newSetupService(t, setup.Draft{
		ActiveProviderID: "openai",
		Providers:        []setup.ProviderDraft{openAIProviderDraft()},
	})
	customHTTPServer := newSetupProviderHTTPServer(t, newTestCore(t), customSetupService, nil)

	req, err = http.NewRequest(http.MethodPatch, customHTTPServer.URL+"/v1/setup/providers/openai?client=custom", bytes.NewBufferString(`{"api_key":"secret"}`))
	if err != nil {
		t.Fatalf("NewRequest(custom client) error = %v", err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH setup provider for custom client error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH custom client status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandleSetupProvidersRequiresSetupService(t *testing.T) {
	server := httptest.NewServer(New(newTestCore(t)).Handler())
	defer server.Close()

	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(server.URL + "/v1/setup/providers")
	if err != nil {
		t.Fatalf("GET /v1/setup/providers error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
}

func newSetupService(t *testing.T, draft setup.Draft) *setup.Service {
	t.Helper()

	if draft.HTTPAddr == "" {
		draft.HTTPAddr = "127.0.0.1:18081"
	}
	if draft.DBPath == "" {
		draft.DBPath = filepath.Join(t.TempDir(), "matrixclaw.db")
	}
	if draft.AutostartOnBoot == "" {
		draft.AutostartOnBoot = "no"
	}

	service := setup.NewService(setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json")))
	if err := service.SaveDraft(draft); err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}
	return service
}

func newTestCore(t *testing.T) *core.Core {
	t.Helper()

	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	t.Cleanup(func() {
		sqliteStore.Close()
	})
	return core.New(sqliteStore)
}

func newSetupProviderHTTPServer(t *testing.T, app *core.Core, setupService *setup.Service, configure func(*Server)) *httptest.Server {
	t.Helper()

	server := New(app)
	server.SetSetupService(setupService)
	if configure != nil {
		configure(server)
	}

	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer
}

func openAIProviderDraft() setup.ProviderDraft {
	return setup.ProviderDraft{
		ID:              "openai",
		CatalogID:       "openai",
		Name:            "OpenAI",
		Type:            "openai-compatible",
		Model:           "gpt-5.4-mini",
		BaseURL:         "https://api.openai.com/v1",
		APIKey:          "openai-secret",
		HasStoredAPIKey: true,
	}
}
