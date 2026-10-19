package setup

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (m *systemdUserDaemonManager) Stop(ctx context.Context, _ string, cfg Config) (DaemonSummary, error) {
	summary := daemonConfiguredSummary(cfg)
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if m.systemdUsable(ctx) {
		err := m.runner.Run(ctx, "systemctl", "--user", "stop", daemonUnitName)
		if err == nil {
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
		// A daemon launched without the unit is not the unit's to stop.
		if m.healthCheck(ctx, cfg.Daemon.HTTPAddr) != nil {
			return summary, fmt.Errorf("systemctl --user stop %s failed: %w", daemonUnitName, err)
		}
	}
	if err := m.stopDirect(ctx, cfg); err != nil {
		return summary, err
	}
	summary.RuntimeStatus = "Stopped"
	return summary, nil
}
