package setup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

type fakeRunner struct {
	runCalls    [][]string
	outputCalls [][]string
	output      []byte
	runErr      error
	outputErr   error
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	call := append([]string{name}, args...)
	f.runCalls = append(f.runCalls, call)
	return f.runErr
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.outputCalls = append(f.outputCalls, call)
	return append([]byte(nil), f.output...), f.outputErr
}

func newSystemdApplyTestManager(t *testing.T, runner *fakeRunner, httpClient *http.Client, linger bool) (*systemdUserDaemonManager, string, string) {
	t.Helper()

	home := t.TempDir()
	daemonBin := filepath.Join(home, "matrixclawd")
	if err := os.WriteFile(daemonBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("offline")
		})}
	}
	manager := &systemdUserDaemonManager{
		runner:        runner,
		lookPath:      func(string) (string, error) { return "/usr/bin/systemctl", nil },
		resolveDaemon: func() (string, error) { return daemonBin, nil },
		currentUser: func() (systemUser, error) {
			return systemUser{Username: "neo", HomeDir: home}, nil
		},
		checkLinger: func(context.Context, string) (bool, error) { return linger, nil },
		httpClient:  httpClient,
	}
	return manager, home, daemonBin
}

func newLiveReloadServer(t *testing.T, reloadStatus int) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			if r.Method != http.MethodGet {
				t.Fatalf("health method = %s, want GET", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/v1/admin/reload":
			if r.Method != http.MethodPost {
				t.Fatalf("reload method = %s, want POST", r.Method)
			}
			if reloadStatus >= 400 {
				http.Error(w, "provider type is not wired yet", reloadStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
}

func executableOnStablePath(t *testing.T, name string) (string, string) {
	t.Helper()

	dir, err := os.MkdirTemp(".", ".daemon-path-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	dir, err = filepath.Abs(dir)
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	bin := filepath.Join(dir, name)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return dir, bin
}

func joinedRunCalls(calls [][]string) string {
	joined := make([]string, 0, len(calls))
	for _, call := range calls {
		joined = append(joined, strings.Join(call, " "))
	}
	return strings.Join(joined, "\n")
}

func TestSystemdUserDaemonManagerApplyEnableNow(t *testing.T) {
	runner := &fakeRunner{
		output: []byte("LoadState=loaded\nActiveState=active\nUnitFileState=enabled\n"),
	}
	manager, home, daemonBin := newSystemdApplyTestManager(t, runner, nil, true)

	cfg := Config{
		Daemon: DaemonConfig{
			HTTPAddr:        "127.0.0.1:8080",
			DBPath:          filepath.Join(home, "matrixclaw.db"),
			AutostartOnBoot: true,
		},
	}

	summary, warnings, err := manager.Apply(context.Background(), filepath.Join(home, "setup.json"), cfg)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if summary.RuntimeStatus != "Running" || !summary.Enabled {
		t.Fatalf("summary = %#v, want running enabled service", summary)
	}

	unitPath := filepath.Join(home, ".config", "systemd", "user", daemonUnitName)
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("ReadFile(unit) error = %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Environment=MATRIXCLAW_SETUP_PATH="+filepath.Join(home, "setup.json")) {
		t.Fatalf("unit content = %q, want setup path env", content)
	}
	if !strings.Contains(content, "ExecStart="+daemonBin) {
		t.Fatalf("unit content = %q, want daemon binary path", content)
	}

	got := joinedRunCalls(runner.runCalls)
	for _, want := range []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable --now " + daemonUnitName,
		"systemctl --user restart " + daemonUnitName,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("run calls = %q, want %q", got, want)
		}
	}
}

func TestSystemdUserDaemonManagerApplyWritesProviderEnvironmentFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-secret")

	runner := &fakeRunner{
		output: []byte("LoadState=loaded\nActiveState=active\nUnitFileState=enabled\n"),
	}
	manager, home, _ := newSystemdApplyTestManager(t, runner, nil, true)
	setupPath := filepath.Join(home, "setup.json")
	cfg := Config{
		Providers: []ProviderConfig{{
			ID:        "openai",
			Name:      "OpenAI",
			Type:      providers.TypeOpenAICompat,
			APIKeyEnv: "OPENAI_API_KEY",
			BaseURL:   "https://api.openai.com/v1",
			Model:     "gpt-test",
		}},
		Daemon: DaemonConfig{
			HTTPAddr:        "127.0.0.1:8080",
			DBPath:          filepath.Join(home, "matrixclaw.db"),
			AutostartOnBoot: true,
		},
	}

	if _, _, err := manager.Apply(context.Background(), setupPath, cfg); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	envPath := DaemonEnvironmentFilePath(setupPath)
	envData, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile(env) error = %v", err)
	}
	if !strings.Contains(string(envData), `OPENAI_API_KEY="env-secret"`) {
		t.Fatalf("env file content = %q, want provider key", envData)
	}
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("Stat(env) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("env file mode = %v, want 0600", got)
	}

	unitPath := filepath.Join(home, ".config", "systemd", "user", daemonUnitName)
	unitData, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("ReadFile(unit) error = %v", err)
	}
	unitContent := string(unitData)
	if !strings.Contains(unitContent, "EnvironmentFile="+envPath) {
		t.Fatalf("unit content = %q, want EnvironmentFile", unitContent)
	}
	if strings.Contains(unitContent, "env-secret") {
		t.Fatalf("unit content leaked provider secret: %q", unitContent)
	}
}

func TestWriteDaemonEnvironmentFileKeepsExistingSecretWhenEnvMissing(t *testing.T) {
	envName := "MATRIXCLAW_TEST_PROVIDER_KEY"
	t.Setenv(envName, "first-secret")

	setupPath := filepath.Join(t.TempDir(), "setup.json")
	cfg := Config{Providers: []ProviderConfig{{
		ID:        "custom",
		Type:      providers.TypeOpenAICompat,
		APIKeyEnv: envName,
	}}}
	envPath, _, err := writeDaemonEnvironmentFile(setupPath, cfg)
	if err != nil {
		t.Fatalf("writeDaemonEnvironmentFile() error = %v", err)
	}
	t.Setenv(envName, "")

	if _, _, err := writeDaemonEnvironmentFile(setupPath, cfg); err != nil {
		t.Fatalf("writeDaemonEnvironmentFile() with existing secret error = %v", err)
	}
	values, err := loadDaemonEnvironmentFile(envPath)
	if err != nil {
		t.Fatalf("loadDaemonEnvironmentFile() error = %v", err)
	}
	if values[envName] != "first-secret" {
		t.Fatalf("env file %s = %q, want first-secret", envName, values[envName])
	}
}

func TestSystemdUserDaemonManagerApplyAutostartReloadFailureRestarts(t *testing.T) {
	server := newLiveReloadServer(t, http.StatusInternalServerError)
	defer server.Close()

	runner := &fakeRunner{
		output: []byte("LoadState=loaded\nActiveState=active\nUnitFileState=enabled\n"),
	}
	manager, home, _ := newSystemdApplyTestManager(t, runner, server.Client(), true)

	cfg := Config{
		Daemon: DaemonConfig{
			HTTPAddr:        server.URL,
			DBPath:          filepath.Join(home, "matrixclaw.db"),
			AutostartOnBoot: true,
		},
	}

	if _, _, err := manager.Apply(context.Background(), filepath.Join(home, "setup.json"), cfg); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	got := joinedRunCalls(runner.runCalls)
	if !strings.Contains(got, "systemctl --user enable --now "+daemonUnitName) {
		t.Fatalf("run calls = %q, want enable --now", got)
	}
	if !strings.Contains(got, "systemctl --user restart "+daemonUnitName) {
		t.Fatalf("run calls = %q, want restart after failed live reload", got)
	}
}

func TestSystemdUserDaemonManagerApplySessionOnlyStart(t *testing.T) {
	runner := &fakeRunner{
		output: []byte("LoadState=loaded\nActiveState=active\nUnitFileState=disabled\n"),
	}
	manager, home, _ := newSystemdApplyTestManager(t, runner, nil, false)

	cfg := Config{
		Daemon: DaemonConfig{
			HTTPAddr:        "127.0.0.1:8080",
			DBPath:          filepath.Join(home, "matrixclaw.db"),
			AutostartOnBoot: false,
		},
	}

	summary, _, err := manager.Apply(context.Background(), filepath.Join(home, "setup.json"), cfg)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if summary.Enabled {
		t.Fatalf("summary.Enabled = true, want false")
	}

	got := joinedRunCalls(runner.runCalls)
	if !strings.Contains(got, "systemctl --user disable "+daemonUnitName) {
		t.Fatalf("run calls = %q, want disable call", got)
	}
	if !strings.Contains(got, "systemctl --user restart "+daemonUnitName) && !strings.Contains(got, "systemctl --user start "+daemonUnitName) {
		t.Fatalf("run calls = %q, want restart/start call", got)
	}
}

func TestSystemdUserDaemonManagerApplyLiveReloadWithoutRestart(t *testing.T) {
	server := newLiveReloadServer(t, http.StatusOK)
	defer server.Close()

	runner := &fakeRunner{
		output: []byte("LoadState=loaded\nActiveState=active\nUnitFileState=disabled\n"),
	}
	manager, home, _ := newSystemdApplyTestManager(t, runner, server.Client(), true)

	cfg := Config{
		Daemon: DaemonConfig{
			HTTPAddr:        server.URL,
			DBPath:          filepath.Join(home, "matrixclaw.db"),
			AutostartOnBoot: false,
		},
	}

	_, warnings, err := manager.Apply(context.Background(), filepath.Join(home, "setup.json"), cfg)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}

	got := joinedRunCalls(runner.runCalls)
	if !strings.Contains(got, "systemctl --user daemon-reload") {
		t.Fatalf("run calls = %q, want daemon-reload", got)
	}
	if !strings.Contains(got, "systemctl --user disable "+daemonUnitName) {
		t.Fatalf("run calls = %q, want disable call", got)
	}
	if strings.Contains(got, "systemctl --user restart "+daemonUnitName) || strings.Contains(got, "systemctl --user start "+daemonUnitName) {
		t.Fatalf("run calls = %q, want no restart/start after live reload", got)
	}
}

func TestSystemdUserDaemonManagerApplyLiveReloadWithoutSystemd(t *testing.T) {
	server := newLiveReloadServer(t, http.StatusOK)
	defer server.Close()

	runner := &fakeRunner{}
	manager := &systemdUserDaemonManager{
		runner:     runner,
		lookPath:   func(string) (string, error) { return "", os.ErrNotExist },
		httpClient: server.Client(),
	}

	cfg := Config{
		Daemon: DaemonConfig{
			HTTPAddr: server.URL,
			DBPath:   filepath.Join(t.TempDir(), "matrixclaw.db"),
		},
	}

	_, warnings, err := manager.Apply(context.Background(), filepath.Join(t.TempDir(), "setup.json"), cfg)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(runner.runCalls) != 0 {
		t.Fatalf("runCalls = %#v, want none", runner.runCalls)
	}
	if len(warnings) != 1 || warnings[0] != "systemd unavailable; live daemon reloaded" {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestSystemdUserDaemonManagerInspectOnDarwinDoesNotWarnAboutSystemd(t *testing.T) {
	server := newLiveReloadServer(t, http.StatusOK)
	defer server.Close()

	manager := &systemdUserDaemonManager{
		lookPath:   func(string) (string, error) { return "", os.ErrNotExist },
		runtimeOS:  func() string { return "darwin" },
		httpClient: server.Client(),
	}

	summary, err := manager.Inspect(context.Background(), "", Config{
		Daemon: DaemonConfig{
			HTTPAddr: server.URL,
			DBPath:   filepath.Join(t.TempDir(), "matrixclaw.db"),
		},
	})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !summary.Running || summary.RuntimeStatus != "Running" {
		t.Fatalf("summary = %#v, want running direct daemon", summary)
	}
	if summary.Warning != "" {
		t.Fatalf("warning = %q, want none on macOS direct daemon", summary.Warning)
	}
}

func TestResolveDaemonBinaryFindsMatrixclawdOnPath(t *testing.T) {
	dir, daemonBin := executableOnStablePath(t, "matrixclawd")

	t.Setenv("PATH", dir)
	t.Setenv("MATRIXCLAW_DAEMON_BIN", "")

	got, err := resolveDaemonBinary()
	if err != nil {
		t.Fatalf("resolveDaemonBinary() error = %v", err)
	}
	if got != daemonBin {
		t.Fatalf("resolveDaemonBinary() = %q, want %q", got, daemonBin)
	}
}

func TestResolveDaemonBinaryDoesNotUseDaemonWrapperName(t *testing.T) {
	dir, _ := executableOnStablePath(t, "daemon")

	t.Setenv("PATH", dir)
	t.Setenv("MATRIXCLAW_DAEMON_BIN", "")

	_, err := resolveDaemonBinary()
	if err == nil {
		t.Fatal("resolveDaemonBinary() error = nil, want matrixclawd lookup failure")
	}
	if !strings.Contains(err.Error(), "matrixclawd") {
		t.Fatalf("resolveDaemonBinary() error = %q, want matrixclawd lookup message", err)
	}
}
