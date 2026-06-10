package clientruntime_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/clientruntime"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func TestControlplaneRuntimeSupportsBrowserModule(t *testing.T) {
	var _ controlplane.BrowserModuleRuntime = clientruntime.ControlplaneRuntime{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/modules/browser" {
			t.Fatalf("request = %s %s, want GET /v1/modules/browser", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"module":{"id":"browser","title":"Browser","enabled":false,"provider_id":"playwright","provider_name":"Local Playwright","local":true,"status":"Disabled","config":{"runtime_mode":"per_task"},"providers":[{"id":"playwright","name":"Local Playwright","local":true,"status":"Local · not installed","action_ids":{"install_runtime":"install-runtime"},"config":{"runtime_mode":"per_task"}}]}}`))
	}))
	defer server.Close()

	runtime := clientruntime.ControlplaneRuntime{
		Daemon: func(externalKey string) (*daemonclient.Client, error) {
			return daemonclient.New(server.URL, "terminal", externalKey), nil
		},
	}
	result, err := controlplane.New(runtime, "").Handle(t.Context(), "", "/modules browser")
	if err != nil {
		t.Fatal(err)
	}
	if result.Text == "Command runtime does not support browser commands." {
		t.Fatalf("browser module was not wired into ControlplaneRuntime")
	}
	if result.Picker == nil || result.Picker.Title != "Browser" {
		t.Fatalf("picker = %#v, want Browser picker", result.Picker)
	}
}

func TestControlplaneRuntimeSupportsMCPModule(t *testing.T) {
	var _ controlplane.MCPRuntime = clientruntime.ControlplaneRuntime{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/modules/mcp" {
			t.Fatalf("request = %s %s, want GET /v1/modules/mcp", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"config":{"enabled":true},"enabled":true,"status":"Enabled · no servers"}`))
	}))
	defer server.Close()

	runtime := clientruntime.ControlplaneRuntime{
		Daemon: func(externalKey string) (*daemonclient.Client, error) {
			return daemonclient.New(server.URL, "terminal", externalKey), nil
		},
	}
	result, err := controlplane.New(runtime, "").Handle(t.Context(), "", "/modules mcp")
	if err != nil {
		t.Fatal(err)
	}
	if result.Text == "Command runtime does not support mcp commands." {
		t.Fatalf("mcp module was not wired into ControlplaneRuntime")
	}
	if result.Picker == nil || result.Picker.Title != "External MCP Servers" {
		t.Fatalf("picker = %#v, want External MCP Servers picker", result.Picker)
	}
}
