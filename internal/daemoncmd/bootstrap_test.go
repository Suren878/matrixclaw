package daemoncmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/clients/telegram"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func saveBootstrapTestConfig(t *testing.T, setupPath string, mutate func(*setup.Config)) {
	t.Helper()

	cfg := setup.Config{
		Version:          setup.CurrentVersion,
		ActiveProviderID: "openai",
		Providers: []setup.ProviderConfig{{
			ID:      "openai",
			Name:    "OpenAI",
			Type:    providers.TypeOpenAICompat,
			APIKey:  "secret",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-test",
		}},
		Daemon: setup.DaemonConfig{
			HTTPAddr: "127.0.0.1:8081",
			DBPath:   "/tmp/test.db",
		},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	if err := setup.NewFileStore(setupPath).Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
}

func TestLoadBootstrapRequiresSetup(t *testing.T) {
	t.Setenv("MATRIXCLAW_SETUP_PATH", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("MATRIXCLAW_HTTP_ADDR", "")
	t.Setenv("MATRIXCLAW_DB_PATH", "")

	_, err := loadBootstrap()
	if !errors.Is(err, ErrSetupRequired) {
		t.Fatalf("loadBootstrap() error = %v, want ErrSetupRequired", err)
	}
}

func TestLoadBootstrapUsesSetupAndEnvOverrides(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)
	t.Setenv("MATRIXCLAW_HTTP_ADDR", "127.0.0.1:9999")
	t.Setenv("MATRIXCLAW_DB_PATH", "/tmp/override.db")

	saveBootstrapTestConfig(t, setupPath, func(cfg *setup.Config) {
		cfg.Assistant = setup.AssistantConfig{
			Name:               "Clawdia",
			SystemPrompt:       "Base system prompt.",
			CustomInstructions: "Prefer short answers.",
		}
		cfg.Providers[0].MaxOutputTokens = 2048
		cfg.Providers[0].ReasoningEffort = "medium"
		cfg.Daemon = setup.DaemonConfig{
			HTTPAddr:        "127.0.0.1:8081",
			DBPath:          "/var/lib/matrixclaw.db",
			AutostartOnBoot: true,
		}
		cfg.Clients = setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{
				Enabled:       true,
				BotToken:      "bot-token",
				AllowedUserID: "12345",
			},
		}
	})

	cfg, err := loadBootstrap()
	if err != nil {
		t.Fatalf("loadBootstrap() error = %v", err)
	}
	if cfg.SetupPath != setupPath {
		t.Fatalf("SetupPath = %q, want %q", cfg.SetupPath, setupPath)
	}
	if cfg.Addr != "127.0.0.1:9999" {
		t.Fatalf("Addr = %q, want env override", cfg.Addr)
	}
	if cfg.DBPath != "/tmp/override.db" {
		t.Fatalf("DBPath = %q, want env override", cfg.DBPath)
	}
	activeProviderID, activeModelID := cfg.SessionLLMs.ActiveSelection()
	if activeProviderID != "openai" || activeModelID != "gpt-test" {
		t.Fatalf("ActiveSelection() = %q/%q, want openai/gpt-test", activeProviderID, activeModelID)
	}
	providerOptions := cfg.SessionLLMs.Providers()
	if len(providerOptions) != 1 || providerOptions[0].ID != "openai" || providerOptions[0].Type != providers.TypeOpenAICompat {
		t.Fatalf("SessionLLMs.Providers() = %#v, want configured OpenAI provider", providerOptions)
	}
	if cfg.Assistant.Name != "Clawdia" {
		t.Fatalf("Assistant.Name = %q, want Clawdia", cfg.Assistant.Name)
	}
	if !strings.Contains(cfg.Assistant.SystemPrompt, "Base system prompt.") {
		t.Fatalf("Assistant.SystemPrompt = %q, want base prompt", cfg.Assistant.SystemPrompt)
	}
	if cfg.Assistant.CustomInstructions != "Prefer short answers." {
		t.Fatalf("Assistant.CustomInstructions = %q, want custom instructions", cfg.Assistant.CustomInstructions)
	}
	telegramClient := cfg.Clients[telegram.ClientName]
	if !telegramClient.Enabled {
		t.Fatal("telegram client bootstrap disabled, want enabled")
	}
	if telegramClient.Values["bot_token"] != "bot-token" || telegramClient.Values["allowed_user_id"] != "12345" {
		t.Fatalf("telegram client bootstrap = %#v", telegramClient)
	}
}

func TestLoadBootstrapUsesDaemonEnvironmentFile(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	envName := "MATRIXCLAW_BOOTSTRAP_PROVIDER_KEY"
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)
	t.Setenv("MATRIXCLAW_HTTP_ADDR", "")
	t.Setenv("MATRIXCLAW_DB_PATH", "")
	t.Setenv(envName, "")

	saveBootstrapTestConfig(t, setupPath, func(cfg *setup.Config) {
		cfg.Providers[0].APIKey = ""
		cfg.Providers[0].APIKeyEnv = envName
	})
	envPath := setup.DaemonEnvironmentFilePath(setupPath)
	if err := os.WriteFile(envPath, []byte(envName+"=\"env-secret\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(env) error = %v", err)
	}

	cfg, err := loadBootstrap()
	if err != nil {
		t.Fatalf("loadBootstrap() error = %v", err)
	}
	if _, _, _, err := cfg.SessionLLMs.Resolve(context.Background(), "", ""); err != nil {
		t.Fatalf("SessionLLMs.Resolve() error = %v", err)
	}
}

func TestLoadBootstrapAllowsSetupWithoutProvider(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)
	t.Setenv("MATRIXCLAW_HTTP_ADDR", "")
	t.Setenv("MATRIXCLAW_DB_PATH", "")

	saveBootstrapTestConfig(t, setupPath, func(cfg *setup.Config) {
		cfg.ActiveProviderID = ""
		cfg.Providers = nil
	})

	cfg, err := loadBootstrap()
	if err != nil {
		t.Fatalf("loadBootstrap() error = %v", err)
	}
	if providerID, modelID := cfg.SessionLLMs.ActiveSelection(); providerID != "" || modelID != "" {
		t.Fatalf("ActiveSelection() = %q/%q, want empty", providerID, modelID)
	}
}

func TestLoadBootstrapFailsOnBrokenSetupProvider(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)
	t.Setenv("MATRIXCLAW_HTTP_ADDR", "")
	t.Setenv("MATRIXCLAW_DB_PATH", "")

	saveBootstrapTestConfig(t, setupPath, func(cfg *setup.Config) {
		cfg.ActiveProviderID = "anthropic"
		cfg.Providers = []setup.ProviderConfig{{
			ID:      "anthropic",
			Name:    "Anthropic",
			Type:    providers.TypeAnthropic,
			APIKey:  "",
			BaseURL: "https://api.anthropic.com/v1",
			Model:   "claude-test",
		}}
	})

	if _, err := loadBootstrap(); err == nil {
		t.Fatalf("loadBootstrap() error = nil, want non-nil")
	}
}

func TestLoadBootstrapRejectsInvalidClientConfig(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)
	t.Setenv("MATRIXCLAW_HTTP_ADDR", "")
	t.Setenv("MATRIXCLAW_DB_PATH", "")

	saveBootstrapTestConfig(t, setupPath, func(cfg *setup.Config) {
		cfg.Clients = setup.ClientsConfig{
			Telegram: setup.TelegramConfig{
				Enabled:       true,
				BotToken:      "bot-token",
				AllowedUserID: "not-an-int",
			},
		}
	})

	_, err := loadBootstrap()
	if err == nil || !strings.Contains(err.Error(), "parse telegram allowed user id") {
		t.Fatalf("loadBootstrap() error = %v, want telegram allowed user id parse error", err)
	}
}

func TestLoadBootstrapRejectsRemoteHTTPBindByDefault(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)
	t.Setenv("MATRIXCLAW_HTTP_ADDR", "")
	t.Setenv("MATRIXCLAW_DB_PATH", "")
	t.Setenv("MATRIXCLAW_ALLOW_REMOTE_HTTP", "")

	saveBootstrapTestConfig(t, setupPath, func(cfg *setup.Config) {
		cfg.Daemon = setup.DaemonConfig{
			HTTPAddr: "0.0.0.0:8080",
			DBPath:   "/tmp/test.db",
		}
	})

	_, err := loadBootstrap()
	if err == nil || !strings.Contains(err.Error(), "non-loopback") {
		t.Fatalf("loadBootstrap() error = %v, want non-loopback rejection", err)
	}
}

func TestIsLoopbackHTTPAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8080":        true,
		"localhost:8080":        true,
		"http://127.0.0.1:8080": true,
		"[::1]:8080":            true,
		":8080":                 false,
		"0.0.0.0:8080":          false,
		"192.168.1.10:8080":     false,
	}
	for addr, want := range cases {
		if got := isLoopbackHTTPAddr(addr); got != want {
			t.Fatalf("isLoopbackHTTPAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}
