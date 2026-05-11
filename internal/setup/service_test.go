package setup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

type fakeDaemonManager struct {
	applySummary   DaemonSummary
	inspectSummary DaemonSummary
	warnings       []string
	applyErr       error
	inspectErr     error
	restartErr     error
	applyCalls     *int
	inspectCalls   *int
	restartCalls   *int
}

func (f fakeDaemonManager) Apply(_ context.Context, _ string, cfg Config) (DaemonSummary, []string, error) {
	if f.applyCalls != nil {
		*f.applyCalls = *f.applyCalls + 1
	}
	summary := f.applySummary
	if summary.Status == "" {
		summary = daemonConfiguredSummary(cfg)
		summary.RuntimeStatus = "Running"
		summary.Installed = true
		summary.Running = true
		summary.Enabled = cfg.Daemon.AutostartOnBoot
	}
	return summary, append([]string(nil), f.warnings...), f.applyErr
}

func (f fakeDaemonManager) Inspect(_ context.Context, _ string, cfg Config) (DaemonSummary, error) {
	if f.inspectCalls != nil {
		*f.inspectCalls = *f.inspectCalls + 1
	}
	if f.inspectErr != nil {
		return DaemonSummary{}, f.inspectErr
	}
	summary := f.inspectSummary
	if summary.Status == "" {
		summary = daemonConfiguredSummary(cfg)
		summary.RuntimeStatus = "Not installed"
	}
	return summary, nil
}

func (f fakeDaemonManager) Restart(_ context.Context, _ string, cfg Config) (DaemonSummary, error) {
	if f.restartCalls != nil {
		*f.restartCalls = *f.restartCalls + 1
	}
	if f.restartErr != nil {
		return DaemonSummary{}, f.restartErr
	}
	summary := f.applySummary
	if summary.Status == "" {
		summary = daemonConfiguredSummary(cfg)
		summary.RuntimeStatus = "Running"
		summary.Installed = true
		summary.Running = true
		summary.Enabled = cfg.Daemon.AutostartOnBoot
	}
	return summary, nil
}

type fakeTelegramValidator struct {
	summary TelegramSummary
	err     error
}

func (f fakeTelegramValidator) Validate(_ context.Context, _ string) (TelegramSummary, error) {
	if f.err != nil {
		return TelegramSummary{}, f.err
	}
	if f.summary.Status == "" {
		return TelegramSummary{Status: "Configured", Username: "matrixclaw_bot"}, nil
	}
	return f.summary, nil
}

func newTestService(path string) *Service {
	service := NewService(NewFileStore(path))
	service.daemonManager = fakeDaemonManager{}
	service.telegramValidate = fakeTelegramValidator{}
	return service
}

func saveDaemonDelegationConfig(t *testing.T, service *Service) {
	t.Helper()

	if err := service.store.Save(Config{
		Version:          CurrentVersion,
		ActiveProviderID: "openai",
		Providers: []ProviderConfig{{
			ID:      "openai",
			Name:    "OpenAI",
			Type:    providers.TypeOpenAICompat,
			APIKey:  "secret",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-5.4-nano",
		}},
		Daemon: DaemonConfig{
			HTTPAddr: "127.0.0.1:8080",
			DBPath:   "/tmp/matrixclaw.db",
		},
		Clients: ClientsConfig{
			Terminal: TerminalConfig{Enabled: true},
			Telegram: TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
}

func TestServiceApplyValidatesAndSavesMultipleProviders(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	service := newTestService(setupPath)

	result, err := service.Apply(Draft{
		AssistantName:         "Clawdia",
		AssistantSystemPrompt: "Base system prompt.",
		AssistantCustomPrompt: "Prefer short answers.",
		ActiveProviderID:      "anthropic",
		Providers: []ProviderDraft{
			{
				ID:              "openai",
				CatalogID:       "openai",
				Name:            "OpenAI",
				Type:            providers.TypeOpenAICompat,
				APIKey:          "openai-secret",
				BaseURL:         "https://api.openai.com/v1",
				Model:           "gpt-5.4-mini",
				MaxOutputTokens: "2048",
				ReasoningEffort: "high",
				HasStoredAPIKey: true,
			},
			{
				ID:              "anthropic",
				CatalogID:       "anthropic",
				Name:            "Anthropic",
				Type:            providers.TypeAnthropic,
				APIKey:          "anthropic-secret",
				BaseURL:         "https://api.anthropic.com/v1",
				Model:           "claude-sonnet-4-5",
				MaxOutputTokens: "4096",
				ReasoningEffort: "medium",
				HasStoredAPIKey: true,
			},
		},
		HTTPAddr:           "127.0.0.1:8080",
		DBPath:             "/tmp/matrixclaw.db",
		AutostartOnBoot:    "yes",
		TelegramEnabled:    "yes",
		TelegramBotToken:   "bot-token",
		TelegramAllowedUID: "12345",
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.Config.ActiveProviderID != "anthropic" {
		t.Fatalf("active provider = %q, want anthropic", result.Config.ActiveProviderID)
	}
	if len(result.Config.Providers) != 2 {
		t.Fatalf("configured providers = %d, want 2", len(result.Config.Providers))
	}
	if result.Summary.Daemon.RuntimeStatus != "Running" {
		t.Fatalf("daemon runtime status = %q, want Running", result.Summary.Daemon.RuntimeStatus)
	}
	if result.Summary.Telegram.Username != "matrixclaw_bot" {
		t.Fatalf("telegram username = %q, want matrixclaw_bot", result.Summary.Telegram.Username)
	}
	if !result.Config.Daemon.AutostartOnBoot {
		t.Fatalf("autostart = %v, want true", result.Config.Daemon.AutostartOnBoot)
	}
	if !result.Config.Clients.Telegram.Enabled {
		t.Fatalf("telegram enabled = %v, want true", result.Config.Clients.Telegram.Enabled)
	}
	if result.Config.Assistant.Name != "Clawdia" {
		t.Fatalf("assistant name = %q, want Clawdia", result.Config.Assistant.Name)
	}
	if result.Config.Assistant.SystemPrompt != "Base system prompt." {
		t.Fatalf("assistant system prompt = %q, want Base system prompt.", result.Config.Assistant.SystemPrompt)
	}
	if result.Config.Assistant.CustomInstructions != "Prefer short answers." {
		t.Fatalf("assistant custom instructions = %q, want Prefer short answers.", result.Config.Assistant.CustomInstructions)
	}

	loaded, err := service.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.ActiveProviderID != "anthropic" {
		t.Fatalf("loaded active provider = %q, want anthropic", loaded.ActiveProviderID)
	}
	if got, ok := ActiveProviderConfig(loaded); !ok || got.APIKey != "anthropic-secret" {
		t.Fatalf("active provider API key mismatch: %#v %v", got, ok)
	}
	if loaded.Assistant.Name != "Clawdia" || loaded.Assistant.CustomInstructions != "Prefer short answers." {
		t.Fatalf("loaded assistant profile mismatch: %#v", loaded.Assistant)
	}
}

func TestServiceApplyAcceptsGeminiProvider(t *testing.T) {
	service := newTestService(filepath.Join(t.TempDir(), "setup.json"))

	result, err := service.Apply(Draft{
		ActiveProviderID: "gemini",
		Providers: []ProviderDraft{
			{
				ID:              "gemini",
				CatalogID:       "gemini",
				Name:            "Google Gemini",
				Type:            providers.TypeGemini,
				APIKey:          "secret",
				BaseURL:         "https://generativelanguage.googleapis.com/v1beta",
				Model:           "gemini-2.5-flash",
				HasStoredAPIKey: true,
			},
		},
		HTTPAddr:        "127.0.0.1:8080",
		DBPath:          "/tmp/matrixclaw.db",
		AutostartOnBoot: "no",
		TelegramEnabled: "no",
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Config.ActiveProviderID != "gemini" {
		t.Fatalf("ActiveProviderID = %q, want gemini", result.Config.ActiveProviderID)
	}
	if len(result.Config.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(result.Config.Providers))
	}
	got := result.Config.Providers[0]
	if got.Type != providers.TypeGemini {
		t.Fatalf("Gemini type = %q, want %q", got.Type, providers.TypeGemini)
	}
	if got.BaseURL != "https://generativelanguage.googleapis.com/v1beta" {
		t.Fatalf("Gemini base URL = %q", got.BaseURL)
	}
	if got.Model != "gemini-2.5-flash" {
		t.Fatalf("Gemini model = %q, want configured model", got.Model)
	}
	if got.MaxOutputTokens != 0 {
		t.Fatalf("Gemini max output tokens = %d, want provider default", got.MaxOutputTokens)
	}
}

func TestServiceDeletesOnlyCustomProviders(t *testing.T) {
	service := newTestService(filepath.Join(t.TempDir(), "setup.json"))
	_, err := service.Apply(Draft{
		ActiveProviderID: "local-ai",
		Providers: []ProviderDraft{
			{
				ID:              "openai",
				CatalogID:       "openai",
				Name:            "OpenAI",
				Type:            providers.TypeOpenAICompat,
				APIKey:          "openai-secret",
				BaseURL:         "https://api.openai.com/v1",
				Model:           "gpt-5.4-mini",
				HasStoredAPIKey: true,
			},
			{
				ID:              "local-ai",
				Name:            "Local AI",
				Type:            providers.TypeOpenAICompat,
				APIKey:          "local-secret",
				BaseURL:         "http://127.0.0.1:11434/v1",
				Model:           "llama3",
				HasStoredAPIKey: true,
			},
		},
		HTTPAddr:        "127.0.0.1:8080",
		DBPath:          "/tmp/matrixclaw.db",
		AutostartOnBoot: "no",
		TelegramEnabled: "no",
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if err := service.DeleteProviderContext(context.Background(), "openai"); err == nil {
		t.Fatal("DeleteProviderContext(openai) error = nil, want built-in provider rejection")
	}
	if err := service.DeleteProviderContext(context.Background(), "local-ai"); err != nil {
		t.Fatalf("DeleteProviderContext(local-ai) error = %v", err)
	}

	cfg, err := service.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ActiveProviderID != "openai" || len(cfg.Providers) != 1 || cfg.Providers[0].ID != "openai" {
		t.Fatalf("config after delete = %#v, want only active OpenAI", cfg)
	}
}

func TestServiceDaemonManagerDelegation(t *testing.T) {
	running := DaemonSummary{
		Status:        "Configured",
		RuntimeStatus: "Running",
		HTTPAddr:      "127.0.0.1:8080",
		DBPath:        "/tmp/matrixclaw.db",
		Running:       true,
	}
	unavailable := running
	unavailable.RuntimeStatus = "Unavailable"
	unavailable.Running = false

	tests := []struct {
		name           string
		inspectSummary DaemonSummary
		applySummary   DaemonSummary
		run            func(context.Context, *Service) (DaemonSummary, error)
		wantInspect    int
		wantApply      int
		wantRestart    int
	}{
		{
			name:           "ensure applies when not running",
			inspectSummary: unavailable,
			applySummary:   running,
			run: func(ctx context.Context, service *Service) (DaemonSummary, error) {
				return service.EnsureDaemonContext(ctx)
			},
			wantInspect: 1,
			wantApply:   1,
		},
		{
			name:           "ensure skips apply when running",
			inspectSummary: running,
			run: func(ctx context.Context, service *Service) (DaemonSummary, error) {
				return service.EnsureDaemonContext(ctx)
			},
			wantInspect: 1,
		},
		{
			name:         "restart delegates restart",
			applySummary: running,
			run: func(ctx context.Context, service *Service) (DaemonSummary, error) {
				return service.RestartDaemonContext(ctx)
			},
			wantRestart: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(NewFileStore(filepath.Join(t.TempDir(), "setup.json")))
			applyCalls := 0
			inspectCalls := 0
			restartCalls := 0
			service.daemonManager = fakeDaemonManager{
				inspectCalls:   &inspectCalls,
				applyCalls:     &applyCalls,
				restartCalls:   &restartCalls,
				inspectSummary: tt.inspectSummary,
				applySummary:   tt.applySummary,
			}
			saveDaemonDelegationConfig(t, service)

			summary, err := tt.run(context.Background(), service)
			if err != nil {
				t.Fatalf("daemon operation error = %v", err)
			}
			if inspectCalls != tt.wantInspect || applyCalls != tt.wantApply || restartCalls != tt.wantRestart {
				t.Fatalf("calls inspect/apply/restart = %d/%d/%d, want %d/%d/%d", inspectCalls, applyCalls, restartCalls, tt.wantInspect, tt.wantApply, tt.wantRestart)
			}
			if !summary.Running || summary.RuntimeStatus != "Running" {
				t.Fatalf("summary = %#v, want running daemon", summary)
			}
		})
	}
}

func TestDraftUsesExistingConfigAndMasksStoredKeys(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := newTestService(store.Path())
	service.store = store

	if err := store.Save(Config{
		Version:          CurrentVersion,
		ActiveProviderID: "anthropic",
		Providers: []ProviderConfig{
			{
				ID:              "anthropic",
				CatalogID:       "anthropic",
				Name:            "Anthropic",
				Type:            providers.TypeAnthropic,
				APIKey:          "secret",
				BaseURL:         "https://api.anthropic.com/v1",
				Model:           "claude-sonnet-4-5",
				MaxOutputTokens: 4096,
				ReasoningEffort: "medium",
			},
			{
				ID:              "openai",
				CatalogID:       "openai",
				Name:            "OpenAI",
				Type:            providers.TypeOpenAICompat,
				APIKey:          "openai-secret",
				BaseURL:         "https://api.openai.com/v1",
				Model:           "gpt-5.4-mini",
				MaxOutputTokens: 4096,
				ReasoningEffort: "medium",
			},
		},
		Daemon: DaemonConfig{HTTPAddr: "127.0.0.1:8080", DBPath: "/tmp/matrixclaw.db"},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	draft, err := service.Draft()
	if err != nil {
		t.Fatalf("Draft() error = %v", err)
	}
	if draft.ActiveProviderID != "anthropic" {
		t.Fatalf("active provider = %q, want anthropic", draft.ActiveProviderID)
	}
	if len(draft.Providers) != 2 {
		t.Fatalf("draft providers = %d, want 2", len(draft.Providers))
	}
	provider, ok := FindProviderDraft(draft, "anthropic")
	if !ok {
		t.Fatal("expected anthropic provider in draft")
	}
	if provider.APIKey != "" {
		t.Fatalf("draft API key = %q, want empty", provider.APIKey)
	}
	if !provider.HasStoredAPIKey {
		t.Fatal("HasStoredAPIKey = false, want true")
	}
	if provider.StoredAPIKeyPreview != "****cret" {
		t.Fatalf("stored key preview = %q, want ****cret", provider.StoredAPIKeyPreview)
	}
}

func TestServiceApplyKeepsStoredKeyWhenDraftKeyEmpty(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := newTestService(store.Path())
	service.store = store

	if err := store.Save(Config{
		Version:          CurrentVersion,
		ActiveProviderID: "openai",
		Providers: []ProviderConfig{
			{
				ID:              "openai",
				CatalogID:       "openai",
				Name:            "OpenAI",
				Type:            providers.TypeOpenAICompat,
				APIKey:          "secret-key",
				BaseURL:         "https://api.openai.com/v1",
				Model:           "gpt-5.4-mini",
				MaxOutputTokens: 4096,
				ReasoningEffort: "medium",
			},
		},
		Daemon: DaemonConfig{HTTPAddr: "127.0.0.1:8080", DBPath: "/tmp/matrixclaw.db"},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	_, err := service.Apply(Draft{
		ActiveProviderID: "openai",
		Providers: []ProviderDraft{
			{
				ID:                  "openai",
				CatalogID:           "openai",
				Name:                "OpenAI",
				Type:                providers.TypeOpenAICompat,
				APIKey:              "",
				BaseURL:             "https://api.openai.com/v1",
				Model:               "gpt-5.4-mini",
				MaxOutputTokens:     "4096",
				ReasoningEffort:     "medium",
				HasStoredAPIKey:     true,
				StoredAPIKeyPreview: "****-key",
			},
		},
		HTTPAddr:        "127.0.0.1:8080",
		DBPath:          "/tmp/matrixclaw.db",
		AutostartOnBoot: "no",
		TelegramEnabled: "no",
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	loaded, err := service.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	active, ok := ActiveProviderConfig(loaded)
	if !ok {
		t.Fatal("expected active provider")
	}
	if active.APIKey != "secret-key" {
		t.Fatalf("api key = %q, want secret-key", active.APIKey)
	}
}

func TestServiceDraftPrefersSavedDraftOverConfig(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := newTestService(store.Path())
	service.store = store

	if err := store.Save(Config{
		Version:          CurrentVersion,
		ActiveProviderID: "openai",
		Providers: []ProviderConfig{
			{
				ID:              "openai",
				CatalogID:       "openai",
				Name:            "OpenAI",
				Type:            providers.TypeOpenAICompat,
				APIKey:          "secret-key",
				BaseURL:         "https://api.openai.com/v1",
				Model:           "gpt-5.4-mini",
				MaxOutputTokens: 4096,
				ReasoningEffort: "medium",
			},
		},
		Daemon: DaemonConfig{HTTPAddr: "127.0.0.1:8080", DBPath: "/tmp/matrixclaw.db"},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	savedDraft := Draft{
		ActiveProviderID: "openai",
		Providers: []ProviderDraft{
			{
				ID:                  "openai",
				CatalogID:           "openai",
				Name:                "OpenAI",
				Type:                providers.TypeOpenAICompat,
				Model:               "gpt-5.4",
				HasStoredAPIKey:     true,
				StoredAPIKeyPreview: "****-key",
			},
		},
		HTTPAddr:        "127.0.0.1:9090",
		DBPath:          "/tmp/draft.db",
		AutostartOnBoot: "yes",
	}
	if err := service.SaveDraft(savedDraft); err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}

	draft, err := service.Draft()
	if err != nil {
		t.Fatalf("Draft() error = %v", err)
	}
	if draft.HTTPAddr != "127.0.0.1:9090" {
		t.Fatalf("draft HTTPAddr = %q, want 127.0.0.1:9090", draft.HTTPAddr)
	}
	if draft.DBPath != "/tmp/draft.db" {
		t.Fatalf("draft DBPath = %q, want /tmp/draft.db", draft.DBPath)
	}
	if draft.AutostartOnBoot != "yes" {
		t.Fatalf("draft AutostartOnBoot = %q, want yes", draft.AutostartOnBoot)
	}
}

func TestProviderSetupItemsHideConfiguredBuiltIns(t *testing.T) {
	service := newTestService(filepath.Join(t.TempDir(), "setup.json"))
	draft := Draft{
		Providers: []ProviderDraft{
			{
				ID:                  "openai",
				CatalogID:           "openai",
				Name:                "OpenAI",
				Type:                providers.TypeOpenAICompat,
				HasStoredAPIKey:     true,
				StoredAPIKeyPreview: "****cret",
			},
		},
	}

	items := ProviderSetupItemsFromDraft(draft, service.ProviderOptions())
	if len(items) == 0 {
		t.Fatal("ProviderSetupItemsFromDraft() returned no providers")
	}
	for _, item := range items {
		if !item.Implemented {
			t.Fatalf("provider %q should not be present in setup items", item.ID)
		}
		if item.ID == "openai" && !item.Configured {
			t.Fatalf("provider %q should not be duplicated as unconfigured", item.ID)
		}
	}
	foundGemini := false
	for _, item := range items {
		if item.ID == "gemini" {
			foundGemini = true
			if item.Type != providers.TypeGemini {
				t.Fatalf("gemini type = %q, want %q", item.Type, providers.TypeGemini)
			}
			if item.Status != "" {
				t.Fatalf("gemini status = %q, want empty", item.Status)
			}
		}
	}
	if !foundGemini {
		t.Fatal("gemini should be visible in available provider options")
	}
}

func TestSummaryFromDraftTelegramStatuses(t *testing.T) {
	baseDraft := Draft{
		Providers: []ProviderDraft{
			{ID: "openai", Name: "OpenAI", Model: "gpt-5.4-mini", APIKey: "secret"},
		},
		ActiveProviderID: "openai",
	}

	tests := []struct {
		name string
		edit func(*Draft)
		want string
	}{
		{name: "disabled", want: "Disabled"},
		{
			name: "incomplete",
			edit: func(draft *Draft) {
				draft.TelegramEnabled = "yes"
				draft.TelegramBotToken = "token"
			},
			want: "Incomplete",
		},
		{
			name: "configured",
			edit: func(draft *Draft) {
				draft.TelegramEnabled = "yes"
				draft.TelegramBotToken = "token"
				draft.TelegramAllowedUID = "12345"
			},
			want: "Configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			draft := baseDraft
			if tt.edit != nil {
				tt.edit(&draft)
			}
			summary := SummaryFromDraft(draft)
			if summary.Telegram.Status != tt.want {
				t.Fatalf("telegram status = %q, want %s", summary.Telegram.Status, tt.want)
			}
		})
	}
}

func TestServiceSummaryIncludesDaemonRuntime(t *testing.T) {
	service := newTestService(filepath.Join(t.TempDir(), "setup.json"))
	service.daemonManager = fakeDaemonManager{
		inspectSummary: DaemonSummary{
			Status:        "Configured",
			HTTPAddr:      "127.0.0.1:8080",
			DBPath:        "/tmp/matrixclaw.db",
			RuntimeStatus: "Running",
			Installed:     true,
			Running:       true,
			Enabled:       true,
		},
	}

	if err := service.store.Save(Config{
		Version:          CurrentVersion,
		ActiveProviderID: "openai",
		Providers: []ProviderConfig{{
			ID:      "openai",
			Name:    "OpenAI",
			Type:    providers.TypeOpenAICompat,
			APIKey:  "secret",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-5.4-mini",
		}},
		Daemon: DaemonConfig{HTTPAddr: "127.0.0.1:8080", DBPath: "/tmp/matrixclaw.db", AutostartOnBoot: true},
		Clients: ClientsConfig{
			Terminal: TerminalConfig{Enabled: true},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	summary, err := service.Summary()
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.Daemon.RuntimeStatus != "Running" {
		t.Fatalf("daemon runtime status = %q, want Running", summary.Daemon.RuntimeStatus)
	}
}

func TestServiceApplyReturnsErrorWhenTelegramValidationFails(t *testing.T) {
	service := newTestService(filepath.Join(t.TempDir(), "setup.json"))
	service.telegramValidate = fakeTelegramValidator{err: errors.New("telegram bot token is invalid")}

	_, err := service.Apply(Draft{
		ActiveProviderID: "openai",
		Providers: []ProviderDraft{
			{
				ID:              "openai",
				CatalogID:       "openai",
				Name:            "OpenAI",
				Type:            providers.TypeOpenAICompat,
				APIKey:          "openai-secret",
				BaseURL:         "https://api.openai.com/v1",
				Model:           "gpt-5.4-mini",
				MaxOutputTokens: "2048",
				ReasoningEffort: "medium",
				HasStoredAPIKey: true,
			},
		},
		HTTPAddr:           "127.0.0.1:8080",
		DBPath:             "/tmp/matrixclaw.db",
		AutostartOnBoot:    "no",
		TelegramEnabled:    "yes",
		TelegramBotToken:   "bad-token",
		TelegramAllowedUID: "12345",
	})
	if err == nil {
		t.Fatal("Apply() error = nil, want telegram validation error")
	}
}

func TestServiceApplyKeepsDraftWhenDaemonApplyFails(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := newTestService(store.Path())
	service.store = store
	service.daemonManager = fakeDaemonManager{applyErr: errors.New("systemd apply failed")}

	draft := Draft{
		ActiveProviderID: "openai",
		Providers: []ProviderDraft{
			{
				ID:              "openai",
				CatalogID:       "openai",
				Name:            "OpenAI",
				Type:            providers.TypeOpenAICompat,
				APIKey:          "openai-secret",
				BaseURL:         "https://api.openai.com/v1",
				Model:           "gpt-5.4-mini",
				MaxOutputTokens: "2048",
				ReasoningEffort: "medium",
				HasStoredAPIKey: true,
			},
		},
		HTTPAddr:        "127.0.0.1:8080",
		DBPath:          "/tmp/matrixclaw.db",
		AutostartOnBoot: "yes",
		TelegramEnabled: "no",
	}
	if err := service.SaveDraft(draft); err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}

	result, err := service.Apply(draft)
	if err == nil {
		t.Fatal("Apply() error = nil, want daemon apply error")
	}
	if result.Path != store.Path() {
		t.Fatalf("result.Path = %q, want %q", result.Path, store.Path())
	}

	loadedDraft, draftErr := store.LoadDraft()
	if draftErr != nil {
		t.Fatalf("LoadDraft() error = %v, want persisted draft", draftErr)
	}
	if loadedDraft.ActiveProviderID != "openai" {
		t.Fatalf("draft active provider = %q, want openai", loadedDraft.ActiveProviderID)
	}

	loadedConfig, configErr := store.Load()
	if configErr != nil {
		t.Fatalf("Load() error = %v, want saved config", configErr)
	}
	if loadedConfig.ActiveProviderID != "openai" {
		t.Fatalf("config active provider = %q, want openai", loadedConfig.ActiveProviderID)
	}
}

func TestStoreLoadRejectsUnsupportedConfigVersion(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	if err := os.WriteFile(store.Path(), []byte("{\n  \"version\": 2,\n  \"daemon\": {\"http_addr\": \"127.0.0.1:8080\", \"db_path\": \"/tmp/matrixclaw.db\"},\n  \"clients\": {\"terminal\": {\"enabled\": true}, \"telegram\": {\"enabled\": false}}\n}\n"), 0o600); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	_, err := store.Load()
	if !errors.Is(err, ErrUnsupportedConfigVersion) {
		t.Fatalf("Load() error = %v, want ErrUnsupportedConfigVersion", err)
	}
}

func TestStoreLoadDropsLegacyPromptedToolModeAndDialect(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	if err := os.WriteFile(store.Path(), []byte(`{
  "version": 3,
  "active_provider_id": "local-ai",
  "providers": [{
    "id": "local-ai",
    "name": "Local AI",
    "type": "openai-compatible",
    "api_key": "secret",
    "base_url": "https://api.example.com/v1",
    "model": "model-id",
    "tool_use_mode": "prompted",
    "tool_schema_dialect": "gemini"
  }],
  "daemon": {"http_addr": "127.0.0.1:8080", "db_path": "/tmp/matrixclaw.db", "autostart_on_boot": false},
  "clients": {"terminal": {"enabled": true}, "telegram": {"enabled": false}}
}
`), 0o600); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(cfg.Providers))
	}
	if cfg.Providers[0].ToolUseMode != "" {
		t.Fatalf("ToolUseMode = %q, want unsupported prompted mode dropped", cfg.Providers[0].ToolUseMode)
	}
	data, err := json.Marshal(cfg.Providers[0])
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if jsonContainsKey(t, data, "tool_schema_dialect") {
		t.Fatalf("config JSON contains legacy tool_schema_dialect: %s", data)
	}
}

func TestProviderSetupItemsFromConfigUsesAppliedProviderModel(t *testing.T) {
	items := ProviderSetupItemsFromConfig(Config{
		ActiveProviderID: "gemini",
		Providers: []ProviderConfig{{
			ID:     "gemini",
			Name:   "Google Gemini",
			Type:   providers.TypeGemini,
			APIKey: "secret",
			Model:  "applied-model",
		}},
	}, nil)

	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Model != "applied-model" || !items[0].Active {
		t.Fatalf("provider item = %#v, want active applied model", items[0])
	}
}
