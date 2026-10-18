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
	if !isScreen[PromptData](add) {
		t.Fatalf("add = %+v", add)
	}
	form := daemon.answer(add, "docs")
	if !isScreen[FormData](form) && !isScreen[PickerData](form) {
		t.Fatalf("after create = %+v", form)
	}
	cfg, _ := service.Load()
	if len(cfg.Modules.MCP.Servers) != 1 || cfg.Modules.MCP.Servers[0].ID != "docs" || cfg.Modules.MCP.Servers[0].Enabled {
		t.Fatalf("servers = %+v", cfg.Modules.MCP.Servers)
	}

	daemon.run("/modules mcp server-set-enabled docs on")
	daemon.run("/modules mcp set-enabled on")
	if cfg, _ := service.Load(); !cfg.Modules.MCP.Enabled || !cfg.Modules.MCP.Servers[0].Enabled {
		t.Fatalf("mcp = %+v", cfg.Modules.MCP)
	}
	list := daemon.run("/modules mcp")
	if ids := pickerItemIDs(list); len(ids) != 3 {
		t.Fatalf("mcp list = %v", ids)
	}

	asked := daemon.run("/modules mcp delete docs")
	if !isScreen[ConfirmData](asked) {
		t.Fatalf("delete = %+v", asked)
	}
	daemon.run(screenOf[ConfirmData](t, asked).ConfirmCommand)
	if cfg, _ := service.Load(); len(cfg.Modules.MCP.Servers) != 0 {
		t.Fatalf("servers after delete = %+v", cfg.Modules.MCP.Servers)
	}
}

func TestReservedMCPServerIDIsRefused(t *testing.T) {
	daemon, service := mcpDaemon(t)

	if result := daemon.run("/modules mcp create browser"); result.Text() == "" {
		t.Fatalf("result = %+v", result)
	}
	if cfg, _ := service.Load(); len(cfg.Modules.MCP.Servers) != 0 {
		t.Fatalf("servers = %+v", cfg.Modules.MCP.Servers)
	}
}

func TestMCPServerSwitchesToHTTPThroughTheForm(t *testing.T) {
	daemon, service := mcpDaemon(t)
	daemon.run("/modules mcp create docs")

	asked := daemon.run("/modules mcp set docs transport http")
	if !isScreen[PromptData](asked) {
		t.Fatalf("transport http = %+v", asked)
	}
	form := daemon.answer(asked, "https://docs.example/mcp")
	if !isScreen[FormData](form) {
		t.Fatalf("after endpoint = %+v", form)
	}
	cfg, _ := service.Load()
	if servers := cfg.Modules.MCP.Servers; len(servers) != 1 || servers[0].Transport != "http" || servers[0].Endpoint != "https://docs.example/mcp" {
		t.Fatalf("servers = %+v", servers)
	}
	if result := daemon.run("/modules mcp set docs endpoint"); result.Text() == "" {
		t.Fatalf("clearing the endpoint = %+v", result)
	}
	if cfg, _ := service.Load(); len(cfg.Modules.MCP.Servers) != 1 {
		t.Fatalf("servers after clearing the endpoint = %+v", cfg.Modules.MCP.Servers)
	}
}

func TestMCPServersNamedLikeVerbsOpen(t *testing.T) {
	daemon, _ := mcpDaemon(t)
	for _, id := range []string{"add", "enabled", "create", "open"} {
		daemon.answer(daemon.run("/modules mcp add"), id)
	}

	list := screenOf[PickerData](t, daemon.run("/modules mcp"))
	opened := 0
	for _, item := range list.Items {
		if item.ID == "enabled" && item.Title == "External MCP" || item.Role != PickerItemRoleNormal || item.Command == "" {
			continue
		}
		server := screenOf[PickerData](t, daemon.run(item.Command))
		if server.Kind != PickerMCPServer || server.Title != item.ID {
			t.Fatalf("%s opened %+v", item.ID, server)
		}
		opened++
	}
	if opened != 4 {
		t.Fatalf("opened %d servers of %+v", opened, list.Items)
	}
}
