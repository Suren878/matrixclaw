package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpbridge "github.com/Suren878/matrixclaw/internal/mcp"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type remoteTools struct{ ids []string }

func (r remoteTools) List() []tools.Spec {
	out := []tools.Spec{}
	for _, id := range r.ids {
		out = append(out, tools.Spec{ID: id, Description: id, InputJSONSchema: json.RawMessage(`{"type":"object"}`)})
	}
	return out
}

func (r remoteTools) Execute(context.Context, string, tools.Call) (tools.Result, error) {
	return tools.Result{Content: "ok"}, nil
}

func mcpServer(t *testing.T, ids ...string) string {
	t.Helper()
	server := mcpbridge.NewToolServer(remoteTools{ids: ids})
	httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil))
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

func countingModule(extra ExtraServer) (*Module, *atomic.Int32) {
	m := New(extra)
	var connects atomic.Int32
	connect := m.connect
	m.connect = func(ctx context.Context, cfg mcpbridge.ServerConfig) (session, error) {
		connects.Add(1)
		return connect(ctx, cfg)
	}
	return m, &connects
}

func toolIDs(m *Module) []string {
	ids := []string{}
	for _, tool := range m.Tools() {
		ids = append(ids, tool.Spec().ID)
	}
	slices.Sort(ids)
	return ids
}

func withServers(enabled bool, servers ...setup.MCPServerConfig) setup.Config {
	return setup.Config{Modules: setup.ModulesConfig{MCP: setup.MCPConfig{Enabled: enabled, Servers: servers}}}
}

func TestApplyConnectsChangedServersOnly(t *testing.T) {
	ctx := context.Background()
	docs := setup.MCPServerConfig{ID: "docs", Enabled: true, Transport: "http", Endpoint: mcpServer(t, "search")}
	files := setup.MCPServerConfig{ID: "files", Enabled: true, Transport: "http", Endpoint: mcpServer(t, "read")}
	m, connects := countingModule(nil)
	defer func() { _ = m.Close() }()

	if err := m.Apply(ctx, withServers(true, docs)); err != nil {
		t.Fatal(err)
	}
	if ids := toolIDs(m); !slices.Equal(ids, []string{"mcp_docs_search"}) {
		t.Fatalf("tools = %v", ids)
	}
	if err := m.Apply(ctx, withServers(true, docs, files)); err != nil {
		t.Fatal(err)
	}
	if got := connects.Load(); got != 2 {
		t.Fatalf("connects = %d, want the unchanged server kept", got)
	}
	if ids := toolIDs(m); !slices.Equal(ids, []string{"mcp_docs_search", "mcp_files_read"}) {
		t.Fatalf("tools = %v", ids)
	}
	docs.ToolPrefix = "kb"
	if err := m.Apply(ctx, withServers(true, docs)); err != nil {
		t.Fatal(err)
	}
	if ids := toolIDs(m); !slices.Equal(ids, []string{"mcp_kb_search"}) || connects.Load() != 3 {
		t.Fatalf("tools = %v after %d connects", ids, connects.Load())
	}
	if err := m.Apply(ctx, withServers(false, docs)); err != nil {
		t.Fatal(err)
	}
	if ids := toolIDs(m); len(ids) != 0 || m.Context() != "" {
		t.Fatalf("disabled MCP still offers %v", ids)
	}
}

func TestFailingServerDoesNotStopTheOthers(t *testing.T) {
	ctx := context.Background()
	docs := setup.MCPServerConfig{ID: "docs", Enabled: true, Transport: "http", Endpoint: mcpServer(t, "search")}
	broken := setup.MCPServerConfig{ID: "broken", Enabled: true, Transport: "http", Endpoint: "http://127.0.0.1:1/mcp", TimeoutSeconds: 2}
	m, _ := countingModule(nil)
	defer func() { _ = m.Close() }()
	if err := m.Apply(ctx, withServers(true, docs, broken)); err != nil {
		t.Fatal(err)
	}
	if ids := toolIDs(m); !slices.Equal(ids, []string{"mcp_docs_search"}) {
		t.Fatalf("tools = %v", ids)
	}
	status := m.Status(ctx)
	if status.Ready || status.State != "1 connected, 1 failed" || status.Detail == "" {
		t.Fatalf("status = %+v", status)
	}
}

func TestExtraServerRunsWithoutTheMCPSwitch(t *testing.T) {
	endpoint := mcpServer(t, "navigate")
	m, _ := countingModule(func(setup.Config) (setup.MCPServerConfig, bool) {
		return setup.MCPServerConfig{ID: "browser", Enabled: true, Transport: "http", Endpoint: endpoint, ToolPrefix: "browser"}, true
	})
	defer func() { _ = m.Close() }()
	if err := m.Apply(context.Background(), withServers(false)); err != nil {
		t.Fatal(err)
	}
	if ids := toolIDs(m); !slices.Equal(ids, []string{"mcp_browser_navigate"}) {
		t.Fatalf("tools = %v", ids)
	}
}
