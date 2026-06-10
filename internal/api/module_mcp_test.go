package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestMCPModuleAPICreateUpdateDeleteReloads(t *testing.T) {
	store := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := setup.NewService(store)
	if err := store.Save(setup.Config{Version: setup.CurrentVersion}); err != nil {
		t.Fatal(err)
	}
	server := New(nil)
	server.SetSetupService(service)
	reloads := 0
	server.SetAdminReload(func(context.Context) error {
		reloads++
		return nil
	})

	createBody, _ := json.Marshal(setup.MCPServerCreateRequest{Server: setup.MCPServerConfig{ID: "docs", Transport: "stdio", Command: "docs-mcp"}})
	create := httptest.NewRecorder()
	server.Handler().ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/modules/mcp/servers", bytes.NewReader(createBody)))
	if create.Code != http.StatusOK {
		t.Fatalf("POST status = %d body=%s", create.Code, create.Body.String())
	}
	if reloads != 1 {
		t.Fatalf("reloads after create = %d, want 1", reloads)
	}

	enabled := true
	updateBody, _ := json.Marshal(setup.MCPServerUpdate{Enabled: &enabled})
	update := httptest.NewRecorder()
	server.Handler().ServeHTTP(update, httptest.NewRequest(http.MethodPatch, "/v1/modules/mcp/docs/server", bytes.NewReader(updateBody)))
	if update.Code != http.StatusOK {
		t.Fatalf("PATCH server status = %d body=%s", update.Code, update.Body.String())
	}
	if reloads != 2 {
		t.Fatalf("reloads after server update = %d, want 2", reloads)
	}

	configBody, _ := json.Marshal(setup.MCPConfigUpdate{Enabled: &enabled})
	config := httptest.NewRecorder()
	server.Handler().ServeHTTP(config, httptest.NewRequest(http.MethodPatch, "/v1/modules/mcp", bytes.NewReader(configBody)))
	if config.Code != http.StatusOK {
		t.Fatalf("PATCH config status = %d body=%s", config.Code, config.Body.String())
	}
	if reloads != 3 {
		t.Fatalf("reloads after config update = %d, want 3", reloads)
	}

	deleteRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteRec, httptest.NewRequest(http.MethodDelete, "/v1/modules/mcp/docs/server", nil))
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	if reloads != 4 {
		t.Fatalf("reloads after delete = %d, want 4", reloads)
	}
	var got setup.MCPConfigResponse
	if err := json.Unmarshal(deleteRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Config.Servers) != 0 {
		t.Fatalf("servers after delete = %#v, want none", got.Config.Servers)
	}
}
