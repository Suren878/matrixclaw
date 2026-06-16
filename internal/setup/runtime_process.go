package setup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (m *systemdUserDaemonManager) startDirect(ctx context.Context, daemonBin string, setupPath string, cfg Config) error {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = devNull.Close() }()

	cmd := exec.CommandContext(ctx, daemonBin)
	cmd.Env = daemonProcessEnvironment(setupPath, cfg)
	cmd.Stdin = nil
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	return m.waitForHealth(ctx, cfg.Daemon.HTTPAddr, 8*time.Second)
}

func (execRunner) Run(ctx context.Context, name string, args ...string) error {
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Env = os.Environ()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}

func (execRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Env = os.Environ()
	return cmd.CombinedOutput()
}

func resolveDaemonBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MATRIXCLAW_DAEMON_BIN")); override != "" {
		if !isStableExecutable(override) {
			return "", fmt.Errorf("MATRIXCLAW_DAEMON_BIN points to a temporary executable")
		}
		return override, nil
	}

	exe, err := os.Executable()
	if err == nil {
		sibling := filepath.Join(filepath.Dir(exe), "matrixclawd")
		if info, statErr := os.Stat(sibling); statErr == nil && !info.IsDir() && isStableExecutable(sibling) {
			return sibling, nil
		}
	}

	path, err := exec.LookPath("matrixclawd")
	if err != nil {
		return "", fmt.Errorf("stable matrixclawd binary not found; install matrixclawd to ~/.local/bin or set MATRIXCLAW_DAEMON_BIN")
	}
	if !isStableExecutable(path) {
		return "", fmt.Errorf("matrixclawd binary looks temporary; install a stable binary before enabling daemon apply")
	}
	return path, nil
}

func isStableExecutable(path string) bool {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" {
		return false
	}
	tmpPrefix := filepath.Clean(os.TempDir()) + string(filepath.Separator)
	if strings.HasPrefix(clean, tmpPrefix) {
		return false
	}
	return !stableBinaryPattern.MatchString(clean)
}
