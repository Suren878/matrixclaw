package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/sessionllm"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/store"
)

func TestSessionLLMUpdateReloadsStaleProviderRegistry(t *testing.T) {
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	defer sqliteStore.Close()

	app := core.New(sqliteStore).WithSessionLLMs(sessionLLMsFromSetupConfig(setup.Config{
		ActiveProviderID: "openai",
		Providers: []setup.ProviderConfig{{
			ID:      "openai",
			Name:    "OpenAI",
			Type:    "openai-compatible",
			Model:   "gpt-5.4",
			BaseURL: "https://api.openai.com/v1",
			APIKey:  "openai-secret",
		}},
	}))
	session, err := app.CreateSession(context.Background(), core.CreateSessionInput{Title: "Docs"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	reloads := 0
	server := New(app)
	server.SetAdminReload(func(context.Context) error {
		reloads++
		app.SetSessionLLMs(sessionLLMsFromSetupConfig(setup.Config{
			ActiveProviderID: "gemini",
			Providers: []setup.ProviderConfig{
				{
					ID:      "openai",
					Name:    "OpenAI",
					Type:    "openai-compatible",
					Model:   "gpt-5.4",
					BaseURL: "https://api.openai.com/v1",
					APIKey:  "openai-secret",
				},
				{
					ID:      "gemini",
					Name:    "Google Gemini",
					Type:    "gemini",
					Model:   "models/gemini-2.5-flash",
					BaseURL: "https://generativelanguage.googleapis.com/v1beta",
					APIKey:  "gemini-secret",
				},
			},
		}))
		return nil
	})

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp := patchSessionLLM(t, httpServer, session.ID, `{"provider_id":"gemini"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if reloads != 1 {
		t.Fatalf("reloads = %d, want 1", reloads)
	}
	var payload core.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(session) error = %v", err)
	}
	if payload.Session.ProviderID != "gemini" || payload.Session.ModelID != "gemini-2.5-flash" {
		t.Fatalf("session llm = provider %q model %q, want gemini/gemini-2.5-flash", payload.Session.ProviderID, payload.Session.ModelID)
	}
}

func TestSessionLLMUpdateReloadsEmptyProviderRegistry(t *testing.T) {
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	defer sqliteStore.Close()

	app := core.New(sqliteStore).WithSessionLLMs(sessionLLMsFromSetupConfig(setup.Config{}))
	session, err := app.CreateSession(context.Background(), core.CreateSessionInput{Title: "Docs"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	reloads := 0
	server := New(app)
	server.SetAdminReload(func(context.Context) error {
		reloads++
		app.SetSessionLLMs(sessionLLMsFromSetupConfig(setup.Config{
			ActiveProviderID: "openai",
			Providers: []setup.ProviderConfig{{
				ID:      "openai",
				Name:    "OpenAI",
				Type:    "openai-compatible",
				Model:   "gpt-5.4",
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "openai-secret",
			}},
		}))
		return nil
	})

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp := patchSessionLLM(t, httpServer, session.ID, `{"provider_id":"openai"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if reloads != 1 {
		t.Fatalf("reloads = %d, want 1", reloads)
	}
	var payload core.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(session) error = %v", err)
	}
	if payload.Session.ProviderID != "openai" || payload.Session.ModelID != "gpt-5.4" {
		t.Fatalf("session llm = provider %q model %q, want openai/gpt-5.4", payload.Session.ProviderID, payload.Session.ModelID)
	}
}

func TestSessionModelUpdateReloadsEmptyProviderRegistry(t *testing.T) {
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	defer sqliteStore.Close()

	app := core.New(sqliteStore).WithSessionLLMs(sessionLLMsFromSetupConfig(setup.Config{}))
	session, err := app.CreateSession(context.Background(), core.CreateSessionInput{Title: "Docs"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	reloads := 0
	server := New(app)
	server.SetAdminReload(func(context.Context) error {
		reloads++
		app.SetSessionLLMs(sessionLLMsFromSetupConfig(setup.Config{
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
		return nil
	})

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp := patchSessionLLM(t, httpServer, session.ID, `{"model_id":"gpt-5.4"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if reloads != 1 {
		t.Fatalf("reloads = %d, want 1", reloads)
	}
	var payload core.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(session) error = %v", err)
	}
	if payload.Session.ProviderID != "openai" || payload.Session.ModelID != "gpt-5.4" {
		t.Fatalf("session llm = provider %q model %q, want openai/gpt-5.4", payload.Session.ProviderID, payload.Session.ModelID)
	}
}

func TestSessionModelUpdatePersistsProviderModelSelection(t *testing.T) {
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	defer sqliteStore.Close()

	setupService := setup.NewService(setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json")))
	setupDraft := setup.Draft{
		ActiveProviderID: "gemini",
		Providers: []setup.ProviderDraft{
			{
				ID:              "openai",
				CatalogID:       "openai",
				Name:            "OpenAI",
				Type:            "openai-compatible",
				Model:           "gpt-5.4",
				BaseURL:         "https://api.openai.com/v1",
				APIKey:          "openai-secret",
				HasStoredAPIKey: true,
			},
			{
				ID:              "gemini",
				CatalogID:       "gemini",
				Name:            "Google Gemini",
				Type:            "gemini",
				Model:           "gemini-2.5-flash",
				BaseURL:         "https://generativelanguage.googleapis.com/v1beta",
				APIKey:          "gemini-secret",
				HasStoredAPIKey: true,
			},
		},
		HTTPAddr:        "127.0.0.1:18081",
		DBPath:          filepath.Join(t.TempDir(), "matrixclaw.db"),
		AutostartOnBoot: "no",
	}
	if err := setupService.SaveDraft(setupDraft); err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}
	if _, err := setupService.ConfigureProviderContext(context.Background(), "gemini", setup.ProviderSetupUpdate{Active: true}); err != nil {
		t.Fatalf("ConfigureProviderContext() error = %v", err)
	}

	loadRegistry := func() *sessionllm.Registry {
		cfg, err := setupService.Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		return sessionLLMsFromSetupConfig(cfg)
	}
	app := core.New(sqliteStore).WithSessionLLMs(loadRegistry())
	session, err := app.CreateSession(context.Background(), core.CreateSessionInput{Title: "Docs"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	reloads := 0
	server := New(app)
	server.SetSetupService(setupService)
	server.SetAdminReload(func(context.Context) error {
		reloads++
		app.SetSessionLLMs(loadRegistry())
		return nil
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp := patchSessionLLM(t, httpServer, session.ID, `{"model_id":"gemma-4"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH model status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if reloads != 1 {
		t.Fatalf("reloads after model update = %d, want 1", reloads)
	}
	cfg, err := setupService.Load()
	if err != nil {
		t.Fatalf("Load() after model update error = %v", err)
	}
	if cfg.Providers[1].Model != "gemma-4" {
		t.Fatalf("gemini config model = %q, want gemma-4", cfg.Providers[1].Model)
	}

	resp = patchSessionLLM(t, httpServer, session.ID, `{"provider_id":"openai"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH openai status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	resp = patchSessionLLM(t, httpServer, session.ID, `{"provider_id":"gemini"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH gemini status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var payload core.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(session) error = %v", err)
	}
	if payload.Session.ProviderID != "gemini" || payload.Session.ModelID != "gemma-4" {
		t.Fatalf("session llm after switching back = %q/%q, want gemini/gemma-4", payload.Session.ProviderID, payload.Session.ModelID)
	}
}

func sessionLLMsFromSetupConfig(cfg setup.Config) *sessionllm.Registry {
	specs := make([]sessionllm.ProviderSpec, 0, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		specs = append(specs, sessionllm.ProviderSpec{
			ID:              provider.ID,
			CatalogID:       provider.CatalogID,
			Name:            provider.Name,
			Type:            provider.Type,
			APIKey:          provider.APIKey,
			BaseURL:         provider.BaseURL,
			Model:           provider.Model,
			MaxOutputTokens: provider.MaxOutputTokens,
			ReasoningEffort: provider.ReasoningEffort,
			ToolUseMode:     provider.ToolUseMode,
		})
	}
	return sessionllm.New(cfg.ActiveProviderID, specs)
}

func patchSessionLLM(t *testing.T, server *httptest.Server, sessionID string, payload string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, server.URL+"/v1/sessions/"+sessionID+"/llm", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("NewRequest(%s) error = %v", payload, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH session llm %s error = %v", payload, err)
	}
	return resp
}
