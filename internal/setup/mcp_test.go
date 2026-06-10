package setup

import (
	"path/filepath"
	"testing"
)

func TestMCPCreateUpdateDeleteServer(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := NewService(store)
	if err := store.Save(Config{Version: CurrentVersion}); err != nil {
		t.Fatal(err)
	}

	created, err := service.CreateMCPServer(MCPServerConfig{ID: "Docs Server"})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Servers) != 1 {
		t.Fatalf("servers = %#v, want one", created.Servers)
	}
	server := created.Servers[0]
	if server.ID != "docs_server" || server.Command != "docs_server" || server.ToolPrefix != "docs_server" || server.Transport != "stdio" {
		t.Fatalf("created server = %#v", server)
	}

	enabled := true
	updated, err := service.UpdateMCPServer("docs_server", MCPServerUpdate{Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Servers[0].Enabled {
		t.Fatalf("updated server = %#v, want enabled", updated.Servers[0])
	}

	deleted, err := service.DeleteMCPServer("docs_server")
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted.Servers) != 0 {
		t.Fatalf("servers after delete = %#v, want none", deleted.Servers)
	}
}

func TestMCPRejectsReservedBrowserServerID(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := NewService(store)
	if err := store.Save(Config{Version: CurrentVersion}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateMCPServer(MCPServerConfig{ID: "browser"}); err == nil {
		t.Fatal("CreateMCPServer(browser) error = nil, want reserved id error")
	}
}
