package setup

import (
	"strings"
	"testing"
)

func TestIncompleteMCPServerUpdateIsRefusedAndKeepsTheServer(t *testing.T) {
	service, store := testService(t, Config{})
	if _, err := service.CreateMCPServer(MCPServerConfig{ID: "docs", Transport: "stdio", Env: map[string]string{"TOKEN": "secret"}}); err != nil {
		t.Fatal(err)
	}
	http, empty := "http", ""
	if _, err := service.UpdateMCPServer("docs", MCPServerUpdate{Transport: &http}); err == nil {
		t.Fatal("switching to http without an endpoint was accepted")
	}
	if _, err := service.UpdateMCPServer("docs", MCPServerUpdate{Command: &empty}); err == nil {
		t.Fatal("clearing the command of a stdio server was accepted")
	}
	endpoint := "https://docs.example/mcp"
	if _, err := service.UpdateMCPServer("docs", MCPServerUpdate{Transport: &http, Endpoint: &endpoint}); err != nil {
		t.Fatal(err)
	}
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	servers := cfg.Modules.MCP.Servers
	if len(servers) != 1 || servers[0].Transport != "http" || servers[0].Endpoint != endpoint || servers[0].Env["TOKEN"] != "secret" {
		t.Fatalf("servers = %+v", servers)
	}
}

func TestMCPToolPrefixMustBeFreeAndNotBrowser(t *testing.T) {
	service, store := testService(t, Config{})
	if _, err := service.CreateMCPServer(MCPServerConfig{ID: "github", Command: "gh-mcp", ToolPrefix: "gh"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateMCPServer(MCPServerConfig{ID: "github_http", Transport: "http", Endpoint: "https://gh.example/mcp", ToolPrefix: "gh"}); err == nil || !strings.Contains(err.Error(), "gh") {
		t.Fatalf("create with a taken prefix = %v", err)
	}
	if _, err := service.CreateMCPServer(MCPServerConfig{ID: "playwright", Command: "npx", ToolPrefix: "browser"}); err == nil {
		t.Fatal("create with the browser prefix was accepted")
	}
	if _, err := service.CreateMCPServer(MCPServerConfig{ID: "docs", Command: "docs-mcp"}); err != nil {
		t.Fatal(err)
	}
	taken := "GH"
	if _, err := service.UpdateMCPServer("docs", MCPServerUpdate{ToolPrefix: &taken}); err == nil {
		t.Fatal("update to a taken prefix was accepted")
	}
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if servers := cfg.Modules.MCP.Servers; len(servers) != 2 || servers[1].ToolPrefix != "docs" {
		t.Fatalf("servers = %+v", servers)
	}
}
