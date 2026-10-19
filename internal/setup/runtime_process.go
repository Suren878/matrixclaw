package setup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// startDirect launches the daemon detached from this process, logging to
// DirectLogPath, and waits until it answers; a daemon that exits first is an
// error carrying the end of its log.
func (m *systemdUserDaemonManager) startDirect(ctx context.Context, daemonBin string, setupPath string, cfg Config) error {
	logPath := DirectLogPath()
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()

	cmd := exec.Command(daemonBin)
	cmd.Env = daemonProcessEnvironment(setupPath, cfg)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
		cancel()
	}()
	err = m.waitForHealth(waitCtx, cfg.Daemon.HTTPAddr, 8*time.Second)
	if err == nil {
		return nil
	}
	select {
	case exitErr := <-exited:
		tail, _ := tailFile(logPath, 5)
		return fmt.Errorf("matrixclawd exited at start (%v): %s", exitErr, strings.TrimSpace(tail))
	default:
		return fmt.Errorf("%w (log: %s)", err, logPath)
	}
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
