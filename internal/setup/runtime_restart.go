package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (m *systemdUserDaemonManager) Restart(ctx context.Context, setupPath string, cfg Config) (DaemonSummary, error) {
	summary := daemonConfiguredSummary(cfg)
	if err := writeDaemonEnvironmentFile(setupPath, cfg); err != nil {
		return summary, err
	}

	if !m.systemdUsable(ctx) {
		if err := m.restartDirect(ctx, setupPath, cfg); err != nil {
			return summary, err
		}
		return m.Inspect(ctx, setupPath, cfg)
	}

	daemonBin, err := m.resolveDaemon()
	if err != nil {
		return summary, fmt.Errorf("resolve matrixclawd binary: %w", err)
	}
	usr, err := m.currentUser()
	if err != nil {
		return summary, fmt.Errorf("resolve current user: %w", err)
	}
	if strings.TrimSpace(usr.HomeDir) == "" {
		return summary, errors.New("resolve HOME for systemd user service")
	}

	// A daemon the unit does not run would keep the data lock from the unit's.
	if m.healthCheck(ctx, cfg.Daemon.HTTPAddr) == nil && m.runner.Run(ctx, "systemctl", "--user", "is-active", "--quiet", daemonUnitName) != nil {
		if err := m.stopDirect(ctx, cfg); err != nil {
			return summary, err
		}
	}

	warnings := []string{}
	if err := m.installAndRestartSystemd(ctx, setupPath, daemonBin, usr, cfg, &warnings); err != nil {
		return summary, err
	}
	if err := m.waitForHealth(ctx, cfg.Daemon.HTTPAddr, 10*time.Second); err != nil {
		return summary, fmt.Errorf("wait for service health: %w", err)
	}

	inspected, err := m.Inspect(ctx, setupPath, cfg)
	if err != nil {
		return summary, err
	}
	if len(warnings) > 0 {
		inspected.Warning = strings.Join(warnings, " | ")
	}
	return inspected, nil
}

func (m *systemdUserDaemonManager) installAndRestartSystemd(ctx context.Context, setupPath string, daemonBin string, usr systemUser, cfg Config, warnings *[]string) error {
	if err := m.writeSystemdUnit(ctx, setupPath, daemonBin, usr, cfg); err != nil {
		return err
	}
	if cfg.Daemon.AutostartOnBoot {
		if err := m.runner.Run(ctx, "systemctl", "--user", "enable", daemonUnitName); err != nil {
			return fmt.Errorf("enable user daemon service: %w", err)
		}
		if linger, err := m.checkLinger(ctx, usr.Username); err != nil {
			*warnings = append(*warnings, "user service installed, but linger could not be verified")
		} else if !linger {
			*warnings = append(*warnings, "user service installed, but reboot autostart requires loginctl enable-linger")
		}
	} else {
		_ = m.runner.Run(ctx, "systemctl", "--user", "disable", daemonUnitName)
	}
	if err := m.runner.Run(ctx, "systemctl", "--user", "restart", daemonUnitName); err != nil {
		if startErr := m.runner.Run(ctx, "systemctl", "--user", "start", daemonUnitName); startErr != nil {
			return fmt.Errorf("start user daemon service: %w", startErr)
		}
	}
	return nil
}
