package setup

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (m *systemdUserDaemonManager) Stop(ctx context.Context, _ string, cfg Config) (DaemonSummary, error) {
	summary := daemonConfiguredSummary(cfg)
	if _, err := m.lookPath("systemctl"); err != nil {
		return summary, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if err := m.runner.Run(ctx, "systemctl", "--user", "stop", daemonUnitName); err != nil {
		return summary, fmt.Errorf("systemctl --user stop %s failed: %w", daemonUnitName, err)
	}
	inspected, err := m.Inspect(ctx, "", cfg)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "not-found") {
		return summary, err
	}
	if err == nil {
		return inspected, nil
	}
	summary.RuntimeStatus = "Stopped"
	summary.Installed = true
	summary.Running = false
	return summary, nil
}
