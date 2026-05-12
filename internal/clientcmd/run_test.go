package clientcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tuiruntime "github.com/Suren878/matrixclaw/clients/terminal/chat/runtime"
	"github.com/Suren878/matrixclaw/internal/setup"
	appstore "github.com/Suren878/matrixclaw/internal/store"
)

func TestHelpUsesBinaryName(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(IO{
		Stdout: &stdout,
		Stderr: &stderr,
	}, "matrixclaw", []string{"help"})

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "matrixclaw setup") {
		t.Fatalf("stdout = %q, want help mentioning matrixclaw setup", out)
	}
	if !strings.Contains(out, "matrixclaw status") {
		t.Fatalf("stdout = %q, want help mentioning matrixclaw status", out)
	}
	if !strings.Contains(out, "matrixclaw service restart") {
		t.Fatalf("stdout = %q, want help mentioning matrixclaw service restart", out)
	}
	if !strings.Contains(out, "matrixclaw providers") {
		t.Fatalf("stdout = %q, want help mentioning matrixclaw providers", out)
	}
	if !strings.Contains(out, "matrixclaw providers verify") {
		t.Fatalf("stdout = %q, want help mentioning matrixclaw providers verify", out)
	}
	if !strings.Contains(out, "matrixclaw tui [WORKDIR]") {
		t.Fatalf("stdout = %q, want help mentioning matrixclaw tui [WORKDIR]", out)
	}
}

func TestStatusMasksAPIKeyAndShowsSetupStates(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	store := setup.NewFileStore(setupPath)
	if err := store.Save(setup.Config{
		Version:          setup.CurrentVersion,
		ActiveProviderID: "openai",
		Providers: []setup.ProviderConfig{{
			ID:      "openai",
			Name:    "OpenAI",
			Type:    "openai-compatible",
			APIKey:  "test-api-key",
			Model:   "gpt-5.4-mini",
			BaseURL: "https://api.openai.com/v1",
		}},
		Daemon: setup.DaemonConfig{
			HTTPAddr: "127.0.0.1:8080",
			DBPath:   "/tmp/matrixclaw.db",
		},
		Clients: setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{
		Stdout: &stdout,
		Stderr: &stderr,
	}, "matrixclaw", []string{"status"})

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	if strings.Contains(out, "test-api-key") {
		t.Fatalf("stdout leaked raw api key: %q", out)
	}
	if !strings.Contains(out, "api key: ****-key") {
		t.Fatalf("stdout = %q, want masked api key", out)
	}
	if !strings.Contains(out, "telegram: Disabled") {
		t.Fatalf("stdout = %q, want telegram status", out)
	}
	if !strings.Contains(out, "provider: OpenAI (gpt-5.4-mini) [Configured]") {
		t.Fatalf("stdout = %q, want configured provider line", out)
	}
	if !strings.Contains(out, "service:") {
		t.Fatalf("stdout = %q, want service runtime line", out)
	}
}

func TestServiceStatusShowsDaemonSummary(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	store := setup.NewFileStore(setupPath)
	if err := store.Save(setup.Config{
		Version: setup.CurrentVersion,
		Daemon: setup.DaemonConfig{
			HTTPAddr:        "127.0.0.1:8080",
			DBPath:          "/tmp/matrixclaw.db",
			AutostartOnBoot: true,
		},
		Clients: setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{Stdout: &stdout, Stderr: &stderr}, "matrixclaw", []string{"service", "status"})

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "service:") {
		t.Fatalf("stdout = %q, want service status", out)
	}
	if !strings.Contains(out, "api: 127.0.0.1:8080") {
		t.Fatalf("stdout = %q, want api address", out)
	}
	if !strings.Contains(out, "autostart: yes") {
		t.Fatalf("stdout = %q, want autostart yes", out)
	}
}

func TestServiceRestartUsesSetupService(t *testing.T) {
	originalService := newSetupService
	originalRestart := restartService
	defer func() {
		newSetupService = originalService
		restartService = originalRestart
	}()

	setupPath := filepath.Join(t.TempDir(), "setup.json")
	store := setup.NewFileStore(setupPath)
	if err := store.Save(setup.Config{
		Version: setup.CurrentVersion,
		Daemon: setup.DaemonConfig{
			HTTPAddr:        "127.0.0.1:8080",
			DBPath:          "/tmp/matrixclaw.db",
			AutostartOnBoot: true,
		},
		Clients: setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	service := setup.NewService(store)
	newSetupService = func() (*setup.Service, error) {
		return service, nil
	}

	restartCalls := 0
	restartService = func(_ context.Context, got *setup.Service) (setup.DaemonSummary, error) {
		restartCalls++
		if got != service {
			t.Fatal("restart got unexpected service")
		}
		return setup.DaemonSummary{
			RuntimeStatus: "Running",
			HTTPAddr:      "127.0.0.1:8080",
			DBPath:        "/tmp/matrixclaw.db",
			Autostart:     true,
			Running:       true,
		}, nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{Stdout: &stdout, Stderr: &stderr}, "matrixclaw", []string{"service", "restart"})

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0, stderr=%q", code, stderr.String())
	}
	if restartCalls != 1 {
		t.Fatalf("restartCalls = %d, want 1", restartCalls)
	}
	if !strings.Contains(stdout.String(), "matrixclaw service restarted") {
		t.Fatalf("stdout = %q, want restart success", stdout.String())
	}
}

func TestServiceLogsUsesJournalReader(t *testing.T) {
	originalReadLogs := readServiceLogs
	defer func() {
		readServiceLogs = originalReadLogs
	}()

	var gotLines int
	readServiceLogs = func(_ context.Context, lines int) (string, error) {
		gotLines = lines
		return "log line\n", nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{Stdout: &stdout, Stderr: &stderr}, "matrixclaw", []string{"service", "logs", "12"})

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0, stderr=%q", code, stderr.String())
	}
	if gotLines != 12 {
		t.Fatalf("gotLines = %d, want 12", gotLines)
	}
	if stdout.String() != "log line\n" {
		t.Fatalf("stdout = %q, want log line", stdout.String())
	}
}

func TestProvidersCommandIncludesGemini(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	store := setup.NewFileStore(setupPath)
	if err := store.Save(setup.Config{
		Version: setup.CurrentVersion,
		Daemon: setup.DaemonConfig{
			HTTPAddr: "127.0.0.1:8080",
			DBPath:   "/tmp/matrixclaw.db",
		},
		Clients: setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{Stdout: &stdout, Stderr: &stderr}, "matrixclaw", []string{"providers"})

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Google Gemini [available] gemini-2.5-flash") {
		t.Fatalf("stdout = %q, want Gemini provider", stdout.String())
	}
}

func TestVersionPrintsClientAndDaemon(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			t.Fatalf("path = %q, want /v1/health", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"version": map[string]string{
				"version": "test-daemon",
				"commit":  "abc123",
			},
		})
	}))
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore, err := appstore.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	defer sqliteStore.Close()

	store := setup.NewFileStore(setupPath)
	if err := store.Save(setup.Config{
		Version: setup.CurrentVersion,
		Daemon: setup.DaemonConfig{
			HTTPAddr: server.URL,
			DBPath:   dbPath,
			APIToken: "test-token",
		},
		Clients: setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{Stdout: &stdout, Stderr: &stderr}, "matrixclaw", []string{"version"})
	if code != 0 {
		t.Fatalf("Run() code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "client:") || !strings.Contains(out, "daemon: test-daemon (abc123)") {
		t.Fatalf("stdout = %q, want client and daemon versions", out)
	}
}

func TestDoctorPassesWhenRuntimeMatchesSetup(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": map[string]string{"version": "test-daemon"}})
		case "/v1/session-providers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"providers": []map[string]any{{
					"id":            "openai",
					"label":         "OpenAI",
					"type":          "openai-compatible",
					"default_model": "gpt-5.4-mini",
					"configured":    true,
				}},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore, err := appstore.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	defer sqliteStore.Close()

	store := setup.NewFileStore(setupPath)
	if err := store.Save(setup.Config{
		Version:          setup.CurrentVersion,
		ActiveProviderID: "openai",
		Providers: []setup.ProviderConfig{{
			ID:      "openai",
			Name:    "OpenAI",
			Type:    "openai-compatible",
			APIKey:  "test-api-key",
			Model:   "gpt-5.4-mini",
			BaseURL: "https://api.openai.com/v1",
		}},
		Daemon: setup.DaemonConfig{
			HTTPAddr: server.URL,
			DBPath:   dbPath,
			APIToken: "test-token",
		},
		Clients: setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{Stdout: &stdout, Stderr: &stderr}, "matrixclaw", []string{"doctor"})
	if code != 0 {
		t.Fatalf("Run() code = %d, want 0, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "doctor: ok") {
		t.Fatalf("stdout = %q, want doctor ok", stdout.String())
	}
	if strings.Contains(stdout.String(), "test-api-key") {
		t.Fatalf("doctor leaked raw api key: %q", stdout.String())
	}
}

func TestProvidersVerifyChecksModelsWithoutLeakingKeys(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Fatalf("Authorization = %q, want Bearer test-api-key", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "test-model"}},
		})
	}))
	defer modelServer.Close()

	store := setup.NewFileStore(setupPath)
	if err := store.Save(setup.Config{
		Version:          setup.CurrentVersion,
		ActiveProviderID: "openai",
		Providers: []setup.ProviderConfig{{
			ID:      "openai",
			Name:    "OpenAI",
			Type:    "openai-compatible",
			APIKey:  "test-api-key",
			Model:   "test-model",
			BaseURL: modelServer.URL,
		}},
		Daemon: setup.DaemonConfig{
			HTTPAddr: "127.0.0.1:8080",
			DBPath:   filepath.Join(t.TempDir(), "matrixclaw.db"),
		},
		Clients: setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{Stdout: &stdout, Stderr: &stderr}, "matrixclaw", []string{"providers", "verify"})
	if code != 0 {
		t.Fatalf("Run() code = %d, want 0, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "providers verify: ok") {
		t.Fatalf("stdout = %q, want verify ok", stdout.String())
	}
	if strings.Contains(stdout.String(), "test-api-key") {
		t.Fatalf("providers verify leaked raw api key: %q", stdout.String())
	}
}

func TestProvidersVerifySkipsProvidersWithoutModelDiscovery(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	store := setup.NewFileStore(setupPath)
	if err := store.Save(setup.Config{
		Version:          setup.CurrentVersion,
		ActiveProviderID: "local-text",
		Providers: []setup.ProviderConfig{{
			ID:      "local-text",
			Name:    "Local Text",
			Type:    "text-only",
			APIKey:  "test-api-key",
			Model:   "local-model",
			BaseURL: "http://127.0.0.1:11434/v1",
		}},
		Daemon: setup.DaemonConfig{
			HTTPAddr: "127.0.0.1:8080",
			DBPath:   filepath.Join(t.TempDir(), "matrixclaw.db"),
		},
		Clients: setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{Stdout: &stdout, Stderr: &stderr}, "matrixclaw", []string{"providers", "verify"})
	if code != 0 {
		t.Fatalf("Run() code = %d, want 0, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "provider Local Text: skipped (model discovery unsupported)") {
		t.Fatalf("stdout = %q, want skip message", stdout.String())
	}
	if !strings.Contains(stdout.String(), "providers verify: ok") {
		t.Fatalf("stdout = %q, want verify ok", stdout.String())
	}
}

func TestProvidersVerifyRedactsProviderKeyFromErrors(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "bad key test-api-key"},
		})
	}))
	defer modelServer.Close()

	store := setup.NewFileStore(setupPath)
	if err := store.Save(setup.Config{
		Version:          setup.CurrentVersion,
		ActiveProviderID: "openai",
		Providers: []setup.ProviderConfig{{
			ID:      "openai",
			Name:    "OpenAI",
			Type:    "openai-compatible",
			APIKey:  "test-api-key",
			Model:   "test-model",
			BaseURL: modelServer.URL,
		}},
		Daemon: setup.DaemonConfig{
			HTTPAddr: "127.0.0.1:8080",
			DBPath:   filepath.Join(t.TempDir(), "matrixclaw.db"),
		},
		Clients: setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{Stdout: &stdout, Stderr: &stderr}, "matrixclaw", []string{"providers", "verify"})
	if code != 1 {
		t.Fatalf("Run() code = %d, want 1", code)
	}
	if strings.Contains(stdout.String(), "test-api-key") {
		t.Fatalf("providers verify leaked raw api key: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "****-key") {
		t.Fatalf("stdout = %q, want masked key in error", stdout.String())
	}
}

func TestDoctorFailsWhenRuntimeProviderIsMissing(t *testing.T) {
	setupPath := filepath.Join(t.TempDir(), "setup.json")
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/v1/session-providers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"providers": []map[string]any{{
					"id":            "openai",
					"label":         "OpenAI",
					"type":          "openai-compatible",
					"default_model": "gpt-5.4-mini",
					"configured":    true,
				}},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	store := setup.NewFileStore(setupPath)
	if err := store.Save(setup.Config{
		Version:          setup.CurrentVersion,
		ActiveProviderID: "gemini",
		Providers: []setup.ProviderConfig{{
			ID:      "gemini",
			Name:    "Google Gemini",
			Type:    "gemini",
			APIKey:  "test-api-key",
			Model:   "gemini-2.5-flash",
			BaseURL: "https://generativelanguage.googleapis.com/v1beta",
		}},
		Daemon: setup.DaemonConfig{
			HTTPAddr: server.URL,
			DBPath:   "/tmp/matrixclaw.db",
		},
		Clients: setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{Stdout: &stdout, Stderr: &stderr}, "matrixclaw", []string{"doctor"})
	if code != 1 {
		t.Fatalf("Run() code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "provider gemini is configured but missing from daemon runtime") {
		t.Fatalf("stdout = %q, want missing provider diagnostic", stdout.String())
	}
}

func TestDefaultRunStartsSetupUIWhenSetupIsMissing(t *testing.T) {
	originalService := newSetupService
	originalSetupUI := openSetupUI
	defer func() {
		newSetupService = originalService
		openSetupUI = originalSetupUI
	}()

	setupPath := filepath.Join(t.TempDir(), "setup.json")
	store := setup.NewFileStore(setupPath)
	newSetupService = func() (*setup.Service, error) {
		return setup.NewService(store), nil
	}

	setupCalled := false
	openSetupUI = func(_ context.Context, service *setup.Service) (setup.ApplyResult, error) {
		setupCalled = true
		if service.Path() != setupPath {
			t.Fatalf("service path = %q, want %q", service.Path(), setupPath)
		}
		return setup.ApplyResult{Path: service.Path()}, nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{
		Stdout: &stdout,
		Stderr: &stderr,
	}, "matrixclaw", nil)

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0", code)
	}
	if !setupCalled {
		t.Fatal("setup UI was not called")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestDefaultRunLaunchesTUIWhenSetupExists(t *testing.T) {
	originalService := newSetupService
	originalSetupUI := openSetupUI
	originalTUI := openTUI
	originalEnsureDaemon := ensureDaemon
	defer func() {
		newSetupService = originalService
		openSetupUI = originalSetupUI
		openTUI = originalTUI
		ensureDaemon = originalEnsureDaemon
	}()

	setupPath := filepath.Join(t.TempDir(), "setup.json")
	store := setup.NewFileStore(setupPath)
	if err := store.Save(setup.Config{
		Version:          setup.CurrentVersion,
		ActiveProviderID: "openai",
		Providers: []setup.ProviderConfig{{
			ID:      "openai",
			Name:    "OpenAI",
			Type:    "openai-compatible",
			APIKey:  "test-api-key",
			Model:   "gpt-5.4-mini",
			BaseURL: "https://api.openai.com/v1",
		}},
		Daemon: setup.DaemonConfig{
			HTTPAddr: "127.0.0.1:8080",
			DBPath:   "/tmp/matrixclaw.db",
		},
		Clients: setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	service := setup.NewService(store)
	newSetupService = func() (*setup.Service, error) {
		return service, nil
	}

	setupCalled := false
	openSetupUI = func(_ context.Context, _ *setup.Service) (setup.ApplyResult, error) {
		setupCalled = true
		return setup.ApplyResult{}, nil
	}

	ensureCalls := 0
	ensureDaemon = func(_ context.Context, got *setup.Service) (setup.DaemonSummary, error) {
		ensureCalls++
		if got != service {
			t.Fatal("ensure daemon got unexpected service")
		}
		return setup.DaemonSummary{
			Status:        "Configured",
			RuntimeStatus: "Running",
			HTTPAddr:      "127.0.0.1:8080",
			DBPath:        "/tmp/matrixclaw.db",
			Running:       true,
		}, nil
	}

	tuiCalled := false
	openTUI = func(_ context.Context, cfg tuiruntime.Config) error {
		tuiCalled = true
		if cfg.Provider != "OpenAI" {
			t.Fatalf("Provider = %q, want OpenAI", cfg.Provider)
		}
		return nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{
		Stdout: &stdout,
		Stderr: &stderr,
	}, "matrixclaw", nil)

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0, stderr=%q", code, stderr.String())
	}
	if setupCalled {
		t.Fatal("setup UI was called")
	}
	if !tuiCalled {
		t.Fatal("tui runtime was not launched")
	}
	if ensureCalls != 1 {
		t.Fatalf("ensureCalls = %d, want 1", ensureCalls)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestTUIEnsuresDaemonBeforeLaunching(t *testing.T) {
	originalService := newSetupService
	originalTUI := openTUI
	originalEnsureDaemon := ensureDaemon
	defer func() {
		newSetupService = originalService
		openTUI = originalTUI
		ensureDaemon = originalEnsureDaemon
	}()

	setupPath := filepath.Join(t.TempDir(), "setup.json")
	store := setup.NewFileStore(setupPath)
	if err := store.Save(setup.Config{
		Version:          setup.CurrentVersion,
		ActiveProviderID: "openai",
		Providers: []setup.ProviderConfig{{
			ID:      "openai",
			Name:    "OpenAI",
			Type:    "openai-compatible",
			APIKey:  "test-api-key",
			Model:   "gpt-5.4-mini",
			BaseURL: "https://api.openai.com/v1",
		}},
		Daemon: setup.DaemonConfig{
			HTTPAddr: "127.0.0.1:8080",
			DBPath:   "/tmp/matrixclaw.db",
		},
		Clients: setup.ClientsConfig{
			Terminal: setup.TerminalConfig{Enabled: true},
			Telegram: setup.TelegramConfig{Enabled: false},
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	service := setup.NewService(store)
	newSetupService = func() (*setup.Service, error) {
		return service, nil
	}

	ensureCalls := 0
	ensureDaemon = func(_ context.Context, _ *setup.Service) (setup.DaemonSummary, error) {
		ensureCalls++
		return setup.DaemonSummary{
			Status:        "Configured",
			RuntimeStatus: "Running",
			HTTPAddr:      "127.0.0.1:8080",
			DBPath:        "/tmp/matrixclaw.db",
			Running:       true,
		}, nil
	}

	launched := false
	explicitWorkingDir := t.TempDir()
	openTUI = func(_ context.Context, cfg tuiruntime.Config) error {
		launched = true
		if cfg.BaseURL != "http://127.0.0.1:8080" {
			t.Fatalf("BaseURL = %q, want http://127.0.0.1:8080", cfg.BaseURL)
		}
		if cfg.Provider != "OpenAI" {
			t.Fatalf("Provider = %q, want OpenAI", cfg.Provider)
		}
		if cfg.Model != "gpt-5.4-mini" {
			t.Fatalf("Model = %q, want gpt-5.4-mini", cfg.Model)
		}
		if cfg.WorkingDir != explicitWorkingDir {
			t.Fatalf("WorkingDir = %q, want %q", cfg.WorkingDir, explicitWorkingDir)
		}
		return nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(IO{Stdout: &stdout, Stderr: &stderr}, "matrixclaw", []string{"tui", explicitWorkingDir})

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0, stderr=%q", code, stderr.String())
	}
	if !launched {
		t.Fatal("tui runtime was not launched")
	}
	if ensureCalls != 1 {
		t.Fatalf("ensureCalls = %d, want 1", ensureCalls)
	}
}

func TestSetupReadCommandsFailWhenSetupIsMissing(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "status", args: []string{"status"}},
		{name: "providers", args: []string{"providers"}},
		{name: "service status", args: []string{"service", "status"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupPath := filepath.Join(t.TempDir(), "setup.json")
			t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Run(IO{
				Stdout: &stdout,
				Stderr: &stderr,
			}, "matrixclaw", tt.args)

			if code != 1 {
				t.Fatalf("Run() code = %d, want 1", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			errOut := stderr.String()
			if !strings.Contains(errOut, "setup not found at "+setupPath) {
				t.Fatalf("stderr = %q, want missing setup message", errOut)
			}
			if !strings.Contains(errOut, "run `matrixclaw setup` first") {
				t.Fatalf("stderr = %q, want setup hint", errOut)
			}
		})
	}
}

func TestSetupReadCommandsFailWhenSetupVersionIsUnsupported(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "status", args: []string{"status"}},
		{name: "providers", args: []string{"providers"}},
		{name: "service status", args: []string{"service", "status"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupPath := filepath.Join(t.TempDir(), "setup.json")
			t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)
			if err := os.WriteFile(setupPath, []byte("{\n  \"version\": 2,\n  \"daemon\": {\"http_addr\": \"127.0.0.1:8080\", \"db_path\": \"/tmp/test.db\"},\n  \"clients\": {\"terminal\": {\"enabled\": true}, \"telegram\": {\"enabled\": false}}\n}\n"), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Run(IO{
				Stdout: &stdout,
				Stderr: &stderr,
			}, "matrixclaw", tt.args)

			if code != 1 {
				t.Fatalf("Run() code = %d, want 1", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			errOut := stderr.String()
			if !strings.Contains(errOut, "setup at "+setupPath+" uses an unsupported version") {
				t.Fatalf("stderr = %q, want unsupported version message", errOut)
			}
			if !strings.Contains(errOut, "reopen `matrixclaw setup` to recreate the setup file") {
				t.Fatalf("stderr = %q, want recreate setup hint", errOut)
			}
		})
	}
}
