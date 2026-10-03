package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type failingTelegramValidator struct{ t *testing.T }

func (v failingTelegramValidator) Validate(context.Context, string) (TelegramSummary, error) {
	v.t.Fatal("telegram was contacted")
	return TelegramSummary{}, nil
}

type countingTelegramValidator struct{ calls int }

func (v *countingTelegramValidator) Validate(context.Context, string) (TelegramSummary, error) {
	v.calls++
	return TelegramSummary{Status: "Configured", Username: "bot"}, nil
}

// testService saves cfg and returns a service over it that must not reach
// Telegram or systemd.
func testService(t *testing.T, cfg Config) (*Service, *FileStore) {
	t.Helper()
	store := NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	cfg.Version = CurrentVersion
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	service.daemonManager = nil
	service.telegramValidate = failingTelegramValidator{t}
	return service, store
}

func liveConfig() Config {
	return Config{
		ActiveProviderID: "openai",
		Assistant:        AssistantConfig{Name: "live"},
		Providers: []ProviderConfig{
			{ID: "openai", APIKey: "sk-openai", Model: "gpt-a"},
			{ID: "local-ai", Name: "Local AI", Type: "openai-compatible", BaseURL: "http://127.0.0.1:8000/v1", APIKey: "sk-local", Model: "qwen"},
		},
		Daemon: DaemonConfig{
			HTTPAddr:         "127.0.0.1:7777",
			DBPath:           "/tmp/matrixclaw.db",
			Timezone:         "UTC",
			APIToken:         "token",
			Budgets:          RunBudgetsConfig{User: RunBudgetConfig{Steps: 500, ActiveTime: "6h"}},
			ContextWindowCap: 100000,
			ModelConcurrency: 2,
		},
		Clients: ClientsConfig{Telegram: TelegramConfig{Enabled: true, BotToken: "123:abc", AllowedUserID: "42"}},
		Modules: ModulesConfig{MCP: MCPConfig{Enabled: true, Servers: []MCPServerConfig{{ID: "files", Command: "mcp-files"}}}},
	}
}

func TestProviderEditsKeepTheRestOfTheConfigAndStayOffline(t *testing.T) {
	service, store := testService(t, liveConfig())

	model := "gpt-b"
	if _, err := service.ConfigureProvider("openai", ProviderSetupUpdate{Model: &model}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfigureProvider("anthropic", ProviderSetupUpdate{APIKey: ptr("sk-ant")}); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteProvider("local-ai"); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if openai, _ := got.Provider("openai"); openai.Model != "gpt-b" || openai.APIKey != "sk-openai" {
		t.Fatalf("openai = %+v", openai)
	}
	if _, ok := got.Provider("local-ai"); ok {
		t.Fatal("custom provider was not deleted")
	}
	if got.ActiveProviderID != "anthropic" {
		t.Fatalf("active = %q, want the newly added provider", got.ActiveProviderID)
	}
	if got.Daemon.Budgets.User.Steps != 500 || got.Daemon.ContextWindowCap != 100000 || got.Daemon.ModelConcurrency != 2 || got.Daemon.APIToken != "token" {
		t.Fatalf("daemon settings lost: %+v", got.Daemon)
	}
	if got.Assistant.Name != "live" || !got.Clients.Telegram.Enabled || len(got.Modules.MCP.Servers) != 1 {
		t.Fatalf("config lost: %+v", got)
	}
}

func TestProviderBaseURLChangeNeedsTheKeyAgain(t *testing.T) {
	service, _ := testService(t, liveConfig())
	other := "https://evil.example/v1"
	if _, err := service.ConfigureProvider("local-ai", ProviderSetupUpdate{BaseURL: &other}); err == nil {
		t.Fatal("base URL change kept the stored key")
	}
	if _, err := service.ProviderModelCatalogFor(context.Background(), "local-ai", ProviderSetupUpdate{BaseURL: &other}); err == nil {
		t.Fatal("model discovery would send the stored key to a new base URL")
	}
	if _, err := service.ConfigureProvider("local-ai", ProviderSetupUpdate{BaseURL: &other, APIKey: ptr("sk-new")}); err != nil {
		t.Fatal(err)
	}
}

func TestProviderRulesAreCheckedBeforeSaving(t *testing.T) {
	service, _ := testService(t, liveConfig())
	t.Setenv("ANTHROPIC_API_KEY", "")
	for name, run := range map[string]func() error{
		"built-in without key": func() error {
			_, err := service.ConfigureProvider("anthropic", ProviderSetupUpdate{Model: ptr("claude")})
			return err
		},
		"custom without type": func() error {
			_, err := service.ConfigureProvider("my-box", ProviderSetupUpdate{Model: ptr("m"), APIKey: ptr("k")})
			return err
		},
		"custom without base URL": func() error {
			_, err := service.ConfigureProvider("my-box", ProviderSetupUpdate{Type: ptr("openai-compatible"), Model: ptr("m"), APIKey: ptr("k")})
			return err
		},
		"built-in delete": func() error { return service.DeleteProvider("openai") },
	} {
		if run() == nil {
			t.Errorf("%s: saved", name)
		}
	}
}

func TestApplyKeepsWhatTheWizardDoesNotEdit(t *testing.T) {
	service, store := testService(t, liveConfig())
	validator := &countingTelegramValidator{}
	service.telegramValidate = validator
	edited, existing, err := service.EditableConfig()
	if err != nil || !existing {
		t.Fatalf("existing=%t err=%v", existing, err)
	}
	edited.Assistant.Name = "wizard"
	edited.Daemon.Budgets = RunBudgetsConfig{}
	edited.Daemon.APIToken = ""
	edited.Modules = ModulesConfig{}

	result, err := service.Apply(context.Background(), edited)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.Load()
	if got.Assistant.Name != "wizard" || got.Daemon.Budgets.User.Steps != 500 || got.Daemon.APIToken != "token" || len(got.Modules.MCP.Servers) != 1 {
		t.Fatalf("apply result %+v", got)
	}
	if validator.calls != 1 || result.Summary.Telegram.Username != "bot" {
		t.Fatalf("telegram checks = %d, summary %+v", validator.calls, result.Summary.Telegram)
	}
}

func TestApplyCreatesTheFirstConfig(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := NewService(store)
	service.daemonManager = nil
	cfg, existing, err := service.EditableConfig()
	if err != nil || existing {
		t.Fatalf("existing=%t err=%v", existing, err)
	}
	if _, err := service.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Daemon.APIToken == "" || got.Daemon.HTTPAddr == "" {
		t.Fatalf("daemon = %+v", got.Daemon)
	}
}

func TestConcurrentModuleUpdatesAreAllSaved(t *testing.T) {
	service, store := testService(t, Config{})
	const servers = 20
	var wg sync.WaitGroup
	for i := range servers {
		wg.Go(func() {
			if _, err := service.CreateMCPServer(MCPServerConfig{ID: fmt.Sprintf("server-%d", i), Command: "true"}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(cfg.Modules.MCP.Servers); got != servers {
		t.Fatalf("saved %d mcp servers, want %d", got, servers)
	}
}

func TestSkillsStayOnUnlessTurnedOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	if err := os.WriteFile(path, []byte(`{"version":3,"modules":{"skills":{"auto_invoke":false}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewFileStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Modules.Skills.IsEnabled() || cfg.Modules.Skills.IsAutoInvoke() {
		t.Fatalf("enabled=%t auto_invoke=%t, want true false", cfg.Modules.Skills.IsEnabled(), cfg.Modules.Skills.IsAutoInvoke())
	}
}

func ptr[T any](value T) *T {
	return &value
}
