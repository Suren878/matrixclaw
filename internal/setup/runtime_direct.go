package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/xdg"
)

const directStopTimeout = 20 * time.Second

// DirectLogPath is the file a directly launched daemon, one that no service
// manager runs, writes its log to.
func DirectLogPath() string {
	return filepath.Join(xdg.StateHome(), "matrixclaw", "matrixclawd.log")
}

// ReadDirectLog returns the last lines of DirectLogPath.
func ReadDirectLog(lines int) (string, error) {
	return tailFile(DirectLogPath(), lines)
}

func tailFile(path string, lines int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	all := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n") + "\n", nil
}

// systemdUsable reports whether a systemd user manager answers.
func (m *systemdUserDaemonManager) systemdUsable(ctx context.Context) bool {
	if _, err := m.lookPath("systemctl"); err != nil {
		return false
	}
	_, err := m.runner.Output(ctx, "systemctl", "--user", "show-environment")
	return err == nil
}

// stopDirect asks the running daemon to stop through its API and waits until
// it is down; no daemon running is success.
func (m *systemdUserDaemonManager) stopDirect(ctx context.Context, cfg Config) error {
	if m.healthCheck(ctx, cfg.Daemon.HTTPAddr) != nil {
		return nil
	}
	if err := m.adminPost(ctx, cfg, "stop"); err != nil {
		return fmt.Errorf("ask the running daemon to stop: %w", err)
	}
	if err := m.waitForDown(ctx, cfg.Daemon.HTTPAddr, directStopTimeout); err != nil {
		return fmt.Errorf("stop daemon: %w (a daemon older than 0.2.1 cannot stop itself without systemd; kill it by hand)", err)
	}
	return nil
}

// restartDirect stops the running daemon, if any, and launches a fresh one;
// the new one answering after the old one went down proves it replaced it.
func (m *systemdUserDaemonManager) restartDirect(ctx context.Context, setupPath string, cfg Config) error {
	daemonBin, err := m.resolveDaemon()
	if err != nil {
		return fmt.Errorf("resolve matrixclawd binary: %w", err)
	}
	if err := m.stopDirect(ctx, cfg); err != nil {
		return err
	}
	return m.startDirect(ctx, daemonBin, setupPath, cfg)
}
