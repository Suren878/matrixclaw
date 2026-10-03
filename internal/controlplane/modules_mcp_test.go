package controlplane

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/api"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func mcpDaemon(t *testing.T) (*apiDaemon, *setup.Service) {
	store := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	if err := store.Save(setup.Config{Version: setup.CurrentVersion}); err != nil {
		t.Fatal(err)
	}
	service := setup.NewService(store)
	return newAPIDaemon(t, api.Deps{Setup: service, Reload: func(context.Context) error { return nil }}), service
}

func TestMCPServerAddToggleAndDelete(t *testing.T) {
	daemon, service := mcpDaemon(t)

	add := daemon.run("/modules mcp add")
	if add.Prompt == nil {
		t.Fatalf("add = %+v", add)
	}
	form := daemon.run(add.Prompt.SubmitCommandPrefix + "docs")
	if form.Form == nil && form.Picker == nil {
		t.Fatalf("after create = %+v", form)
	}
	cfg, _ := service.Load()
	if len(cfg.Modules.MCP.Servers) != 1 || cfg.Modules.MCP.Servers[0].ID != "docs" || cfg.Modules.MCP.Servers[0].Enabled {
		t.Fatalf("servers = %+v", cfg.Modules.MCP.Servers)
	}

	daemon.run("/modules mcp docs set-enabled on")
	daemon.run("/modules mcp set-enabled on")
	if cfg, _ := service.Load(); !cfg.Modules.MCP.Enabled || !cfg.Modules.MCP.Servers[0].Enabled {
		t.Fatalf("mcp = %+v", cfg.Modules.MCP)
	}
	list := daemon.run("/modules mcp")
	if ids := pickerItemIDs(list); len(ids) != 3 {
		t.Fatalf("mcp list = %v", ids)
	}

	asked := daemon.run("/modules mcp docs delete")
	if asked.Confirm == nil {
		t.Fatalf("delete = %+v", asked)
	}
	daemon.run(asked.Confirm.ConfirmCommand)
	if cfg, _ := service.Load(); len(cfg.Modules.MCP.Servers) != 0 {
		t.Fatalf("servers after delete = %+v", cfg.Modules.MCP.Servers)
	}
}

func TestReservedMCPServerIDIsRefused(t *testing.T) {
	daemon, service := mcpDaemon(t)

	if result := daemon.run("/modules mcp create browser"); result.Text == "" {
		t.Fatalf("result = %+v", result)
	}
	if cfg, _ := service.Load(); len(cfg.Modules.MCP.Servers) != 0 {
		t.Fatalf("servers = %+v", cfg.Modules.MCP.Servers)
	}
}

func TestMCPServerSwitchesToHTTPThroughTheForm(t *testing.T) {
	daemon, service := mcpDaemon(t)
	daemon.run("/modules mcp create docs")

	asked := daemon.run("/modules mcp docs set transport http")
	if asked.Prompt == nil {
		t.Fatalf("transport http = %+v", asked)
	}
	form := daemon.run(asked.Prompt.SubmitCommandPrefix + "https://docs.example/mcp")
	if form.Form == nil {
		t.Fatalf("after endpoint = %+v", form)
	}
	cfg, _ := service.Load()
	if servers := cfg.Modules.MCP.Servers; len(servers) != 1 || servers[0].Transport != "http" || servers[0].Endpoint != "https://docs.example/mcp" {
		t.Fatalf("servers = %+v", servers)
	}
	if result := daemon.run("/modules mcp docs set endpoint"); result.Text == "" {
		t.Fatalf("clearing the endpoint = %+v", result)
	}
	if cfg, _ := service.Load(); len(cfg.Modules.MCP.Servers) != 1 {
		t.Fatalf("servers after clearing the endpoint = %+v", cfg.Modules.MCP.Servers)
	}
}
