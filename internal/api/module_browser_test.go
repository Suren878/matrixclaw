package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestBrowserModuleAPIGetAndPatch(t *testing.T) {
	store := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := setup.NewService(store)
	if err := store.Save(setup.Config{
		Version: setup.CurrentVersion,
		Daemon:  setup.DaemonConfig{HTTPAddr: "127.0.0.1:8080", DBPath: filepath.Join(t.TempDir(), "matrixclaw.db")},
	}); err != nil {
		t.Fatal(err)
	}
	server := New(nil)
	server.SetSetupService(service)

	get := httptest.NewRecorder()
	server.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/modules/browser", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", get.Code, get.Body.String())
	}
	var got setup.BrowserModuleResponse
	if err := json.Unmarshal(get.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Module.ProviderID != setup.BrowserProviderPlaywright || got.Module.Enabled {
		t.Fatalf("GET module = %#v, want disabled local playwright", got.Module)
	}

	body, _ := json.Marshal(setup.BrowserModuleUpdate{
		Enabled:    boolPtr(true),
		ProviderID: setup.BrowserProviderPlaywright,
		ProviderConfig: &setup.BrowserProviderConfig{
			RuntimeMode: "always_running",
		},
	})
	patch := httptest.NewRecorder()
	server.Handler().ServeHTTP(patch, httptest.NewRequest(http.MethodPatch, "/v1/modules/browser", bytes.NewReader(body)))
	if patch.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d body=%s", patch.Code, patch.Body.String())
	}
	if err := json.Unmarshal(patch.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Module.Enabled || got.Module.Config.RuntimeMode != "always_running" {
		t.Fatalf("PATCH module = %#v, want enabled always_running", got.Module)
	}
}

func TestBrowserProviderActionReportsInstalledRuntime(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", runtimeDir)
	runtime := localruntime.New("")
	writeAPIExecutable(t, runtime.ManagedPlaywrightMCPBinaryPathForTest())
	writeAPIFile(t, runtime.ManagedPlaywrightMCPBrowsersJSONPathForTest(), `{"browsers":[{"name":"chromium","revision":"1224"}]}`)
	if err := os.MkdirAll(filepath.Join(runtime.PlaywrightBrowsersDirForTest(), "chromium-1224"), 0o755); err != nil {
		t.Fatal(err)
	}

	store := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := setup.NewService(store)
	if err := store.Save(setup.Config{
		Version: setup.CurrentVersion,
		Daemon:  setup.DaemonConfig{HTTPAddr: "127.0.0.1:8080", DBPath: filepath.Join(t.TempDir(), "matrixclaw.db")},
	}); err != nil {
		t.Fatal(err)
	}
	server := New(nil)
	server.SetSetupService(service)
	reloads := 0
	server.SetAdminReload(func(context.Context) error {
		reloads++
		return nil
	})

	body, _ := json.Marshal(setup.BrowserProviderActionRequest{Action: "test"})
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/modules/browser/providers/playwright/action", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var got setup.BrowserProviderActionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Provider.RuntimeInstalled || !got.Provider.BrowserInstalled {
		t.Fatalf("provider = %#v, want installed runtime and browser", got.Provider)
	}
	if reloads != 0 {
		t.Fatalf("reloads = %d, want 0 for test action", reloads)
	}
}

func TestBrowserProviderDeleteRuntimeReloadsEnabledModule(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", runtimeDir)
	runtime := localruntime.New("")
	writeAPIExecutable(t, runtime.ManagedPlaywrightMCPBinaryPathForTest())
	writeAPIFile(t, runtime.ManagedPlaywrightMCPBrowsersJSONPathForTest(), `{"browsers":[{"name":"chromium","revision":"1224"}]}`)
	if err := os.MkdirAll(filepath.Join(runtime.PlaywrightBrowsersDirForTest(), "chromium-1224"), 0o755); err != nil {
		t.Fatal(err)
	}

	store := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	service := setup.NewService(store)
	if err := store.Save(setup.Config{
		Version: setup.CurrentVersion,
		Daemon:  setup.DaemonConfig{HTTPAddr: "127.0.0.1:8080", DBPath: filepath.Join(t.TempDir(), "matrixclaw.db")},
		Modules: setup.ModulesConfig{Browser: setup.BrowserConfig{
			Enabled:    true,
			ProviderID: setup.BrowserProviderPlaywright,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	server := New(nil)
	server.SetSetupService(service)
	reloads := 0
	server.SetAdminReload(func(context.Context) error {
		reloads++
		return nil
	})

	body, _ := json.Marshal(setup.BrowserProviderActionRequest{Action: "delete-runtime"})
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/modules/browser/providers/playwright/action", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if reloads != 1 {
		t.Fatalf("reloads = %d, want 1", reloads)
	}
}

func boolPtr(value bool) *bool { return &value }

func writeAPIExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeAPIFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
