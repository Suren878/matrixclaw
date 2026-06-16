package clientcmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	appsetup "github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/version"
)

func runDoctorCommand(stdout io.Writer, stderr io.Writer, binaryName string, service *appsetup.Service) int {
	cfg, err := service.Load()
	if err != nil {
		return handleSetupReadError(stderr, binaryName, service, "doctor", err)
	}

	issues := 0
	_, _ = fmt.Fprintf(stdout, "%s: doctor: matrixclaw %s\n", binaryName, version.String())
	_, _ = fmt.Fprintf(stdout, "%s: setup: %s\n", binaryName, service.Path())
	_, _ = fmt.Fprintf(stdout, "%s: setup config version: ok (%d)\n", binaryName, appsetup.CurrentVersion)
	_, _ = fmt.Fprintf(stdout, "%s: api: %s\n", binaryName, daemonBaseURL(cfg.Daemon.HTTPAddr))
	if strings.TrimSpace(cfg.Daemon.APIToken) == "" {
		_, _ = fmt.Fprintf(stdout, "%s: ERROR api auth: missing setup daemon api_token\n", binaryName)
		issues++
	} else {
		_, _ = fmt.Fprintf(stdout, "%s: api auth: ok\n", binaryName)
	}
	_, _ = fmt.Fprintf(stdout, "%s: database: %s\n", binaryName, cfg.Daemon.DBPath)
	if _, err := store.CheckSQLite(cfg.Daemon.DBPath); err != nil {
		_, _ = fmt.Fprintf(stdout, "%s: ERROR database: %v\n", binaryName, err)
		issues++
	} else {
		_, _ = fmt.Fprintf(stdout, "%s: database schema: ok\n", binaryName)
	}
	if err := printDaemonBinaryDiagnostic(stdout, binaryName); err != nil {
		_, _ = fmt.Fprintf(stdout, "%s: WARN daemon binary: %v\n", binaryName, err)
	}
	if cfg.Clients.Telegram.Enabled {
		_, _ = fmt.Fprintf(stdout, "%s: telegram: enabled\n", binaryName)
	} else {
		_, _ = fmt.Fprintf(stdout, "%s: telegram: disabled\n", binaryName)
	}

	client := configuredDaemonClient(cfg)
	health, err := client.Health(context.Background())
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "%s: ERROR daemon health: %v\n", binaryName, err)
		return 1
	}
	if !health.OK {
		_, _ = fmt.Fprintf(stdout, "%s: ERROR daemon health: not ok\n", binaryName)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "%s: daemon: ok", binaryName)
	if daemonVersion := strings.TrimSpace(health.Version.Version); daemonVersion != "" {
		_, _ = fmt.Fprintf(stdout, " %s", daemonVersion)
	}
	_, _ = fmt.Fprintln(stdout)

	setupProviders := appsetup.ProviderSetupItemsFromConfig(cfg, service.ProviderOptions())
	runtimeProviders, err := client.ListSessionProviders(context.Background())
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "%s: ERROR runtime providers: %v\n", binaryName, err)
		return 1
	}

	configured := configuredProviderMap(setupProviders)
	runtime := runtimeProviderMap(runtimeProviders)
	for id, setupProvider := range configured {
		runtimeProvider, ok := runtime[id]
		if !ok {
			_, _ = fmt.Fprintf(stdout, "%s: ERROR provider %s is configured but missing from daemon runtime\n", binaryName, id)
			issues++
			continue
		}
		if strings.TrimSpace(setupProvider.Model) != "" && strings.TrimSpace(runtimeProvider.DefaultModel) != "" && strings.TrimSpace(setupProvider.Model) != strings.TrimSpace(runtimeProvider.DefaultModel) {
			_, _ = fmt.Fprintf(stdout, "%s: ERROR provider %s model mismatch: setup=%s runtime=%s\n", binaryName, id, setupProvider.Model, runtimeProvider.DefaultModel)
			issues++
		}
	}
	for id := range runtime {
		if _, ok := configured[id]; !ok {
			_, _ = fmt.Fprintf(stdout, "%s: WARN runtime provider %s is not present in setup provider list\n", binaryName, id)
		}
	}
	if _, ok := runtime[strings.TrimSpace(cfg.ActiveProviderID)]; cfg.ActiveProviderID != "" && !ok {
		_, _ = fmt.Fprintf(stdout, "%s: ERROR active provider %s is not loaded by daemon runtime\n", binaryName, cfg.ActiveProviderID)
		issues++
	}

	if issues > 0 {
		_, _ = fmt.Fprintf(stdout, "%s: doctor: failed (%d issue(s))\n", binaryName, issues)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "%s: provider registry: ok (%d configured, %d runtime)\n", binaryName, len(configured), len(runtime))
	_, _ = fmt.Fprintf(stdout, "%s: doctor: ok\n", binaryName)
	return 0
}

func printDaemonBinaryDiagnostic(stdout io.Writer, binaryName string) error {
	expected, expectedErr := expectedDaemonBinary()
	systemdExec, systemdErr := systemdDaemonExecStart(context.Background())
	if expectedErr == nil {
		_, _ = fmt.Fprintf(stdout, "%s: daemon binary: %s\n", binaryName, expected)
	}
	if systemdErr == nil {
		_, _ = fmt.Fprintf(stdout, "%s: systemd ExecStart: %s\n", binaryName, systemdExec)
	}
	if expectedErr != nil && systemdErr != nil {
		return expectedErr
	}
	if expectedErr == nil && systemdErr == nil && filepath.Clean(expected) != filepath.Clean(systemdExec) {
		return fmt.Errorf("systemd uses %s, expected %s", systemdExec, expected)
	}
	if systemdErr != nil {
		return systemdErr
	}
	return nil
}

func expectedDaemonBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MATRIXCLAW_DAEMON_BIN")); override != "" {
		return filepath.Clean(override), nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	sibling := filepath.Join(filepath.Dir(exe), "matrixclawd")
	if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
		return filepath.Clean(sibling), nil
	}
	if path, err := exec.LookPath("matrixclawd"); err == nil {
		return filepath.Clean(path), nil
	}
	return "", fmt.Errorf("matrixclawd not found next to client or in PATH")
}

func systemdDaemonExecStart(ctx context.Context) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(runCtx, "systemctl", "--user", "show", appsetup.DaemonUnitName, "--property=ExecStart", "--value").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	line := strings.TrimSpace(string(output))
	if line == "" {
		return "", fmt.Errorf("systemd ExecStart is empty")
	}
	if strings.HasPrefix(line, "{") {
		if idx := strings.Index(line, " ;"); idx > 1 {
			line = strings.TrimSpace(strings.TrimPrefix(line[:idx], "{"))
		}
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", fmt.Errorf("systemd ExecStart is empty")
	}
	return filepath.Clean(strings.TrimPrefix(fields[0], "path=")), nil
}

func configuredProviderMap(items []appsetup.ProviderSetupItem) map[string]appsetup.ProviderSetupItem {
	providers := make(map[string]appsetup.ProviderSetupItem, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" || !item.Configured {
			continue
		}
		providers[id] = item
	}
	return providers
}

func runtimeProviderMap(items []core.SessionProviderOption) map[string]core.SessionProviderOption {
	providers := make(map[string]core.SessionProviderOption, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		providers[id] = item
	}
	return providers
}
