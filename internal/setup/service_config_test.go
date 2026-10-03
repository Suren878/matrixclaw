package setup

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestConfiguringAProviderKeepsTheRestOfTheConfig(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := NewService(store)
	draft := defaultDraft()
	draft.AssistantName = "live"
	draft.Providers = []ProviderDraft{{ID: "openai", CatalogID: "openai", Type: "openai-compatible", Name: "OpenAI", APIKey: "sk-test", BaseURL: "https://api.openai.com/v1", Model: "gpt-a"}}
	draft.ActiveProviderID = "openai"
	if err := service.saveRuntimeConfig(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Daemon.Budgets.User = RunBudgetConfig{Steps: 500, ActiveTime: "6h"}
	cfg.Daemon.ContextWindowCap = 100000
	cfg.Daemon.ModelConcurrency = 2
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	stale := draft
	stale.AssistantName = "abandoned wizard"
	if err := store.SaveDraft(stale); err != nil {
		t.Fatal(err)
	}

	if _, err := service.ConfigureProviderContext(context.Background(), "openai", ProviderSetupUpdate{Model: "gpt-b"}); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers[0].Model != "gpt-b" {
		t.Fatalf("model = %q", got.Providers[0].Model)
	}
	if got.Daemon.Budgets.User.Steps != 500 || got.Daemon.Budgets.User.ActiveTime != "6h" || got.Daemon.ContextWindowCap != 100000 || got.Daemon.ModelConcurrency != 2 {
		t.Fatalf("daemon settings lost: %+v", got.Daemon)
	}
	if got.Assistant.Name != "live" {
		t.Fatalf("assistant name = %q, want the saved config's, not the abandoned draft's", got.Assistant.Name)
	}
}

func TestConcurrentModuleUpdatesAreAllSaved(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	if err := store.Save(Config{Version: CurrentVersion}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
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
