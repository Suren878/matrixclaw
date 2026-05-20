package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

func (m *systemdUserDaemonManager) applyWithSystemd(ctx context.Context, setupPath string, daemonBin string, usr systemUser, cfg Config, liveReloaded bool, warnings *[]string) error {
	if err := m.writeSystemdUnit(ctx, setupPath, daemonBin, usr, cfg); err != nil {
		return err
	}

	if cfg.Daemon.AutostartOnBoot {
		enableArgs := []string{"--user", "enable", daemonUnitName}
		if !liveReloaded {
			enableArgs = []string{"--user", "enable", "--now", daemonUnitName}
		}
		if err := m.runner.Run(ctx, "systemctl", enableArgs...); err != nil {
			return fmt.Errorf("enable user daemon service: %w", err)
		}
		if !liveReloaded {
			if err := m.restartOrStart(ctx); err != nil {
				return err
			}
		}
		if linger, err := m.checkLinger(ctx, usr.Username); err != nil {
			*warnings = append(*warnings, "user service installed, but linger could not be verified")
		} else if !linger {
			*warnings = append(*warnings, "user service installed, but reboot autostart requires loginctl enable-linger")
		}
		return nil
	}

	_ = m.runner.Run(ctx, "systemctl", "--user", "disable", daemonUnitName)
	if liveReloaded {
		return nil
	}
	return m.restartOrStart(ctx)
}

func (m *systemdUserDaemonManager) restartOrStart(ctx context.Context) error {
	if err := m.runner.Run(ctx, "systemctl", "--user", "restart", daemonUnitName); err != nil {
		if startErr := m.runner.Run(ctx, "systemctl", "--user", "start", daemonUnitName); startErr != nil {
			return fmt.Errorf("start user daemon service: %w", startErr)
		}
	}
	return nil
}

func (m *systemdUserDaemonManager) writeSystemdUnit(ctx context.Context, setupPath string, daemonBin string, usr systemUser, cfg Config) error {
	unitPath := filepath.Join(usr.HomeDir, ".config", "systemd", "user", daemonUnitName)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return fmt.Errorf("create systemd user unit dir: %w", err)
	}
	envFilePath := ""
	if len(daemonEnvironmentNames(cfg)) > 0 {
		envFilePath = DaemonEnvironmentFilePath(setupPath)
	}
	if err := os.WriteFile(unitPath, []byte(renderSystemdUserUnit(daemonBin, setupPath, usr.HomeDir, envFilePath)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write systemd user unit: %w", err)
	}
	if err := m.runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %w", err)
	}
	return nil
}

func renderSystemdUserUnit(daemonBin string, setupPath string, homeDir string, envFilePath string) string {
	daemonBin = systemdUnitValue(daemonBin)
	setupPath = systemdUnitValue(setupPath)
	homeDir = systemdUnitValue(homeDir)
	path := systemdUnitValue(daemonSystemdPath(homeDir))
	envFileLine := ""
	if strings.TrimSpace(envFilePath) != "" {
		envFileLine = "EnvironmentFile=" + systemdUnitValue(envFilePath) + "\n"
	}
	return strings.TrimSpace(fmt.Sprintf(`
[Unit]
Description=matrixclaw service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=%s
Environment=HOME=%s
Environment=PATH=%s
%sEnvironment=MATRIXCLAW_SETUP_PATH=%s
ExecStart=%s
Restart=always
RestartSec=2
NoNewPrivileges=true
PrivateTmp=true
LockPersonality=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX

[Install]
WantedBy=default.target
`, homeDir, homeDir, path, envFileLine, setupPath, daemonBin))
}

func daemonSystemdPath(homeDir string) string {
	homeDir = strings.TrimRight(strings.TrimSpace(homeDir), string(os.PathSeparator))
	parts := []string{}
	if homeDir != "" {
		parts = append(parts,
			filepath.Join(homeDir, ".local", "bin"),
			filepath.Join(homeDir, ".npm-global", "bin"),
			filepath.Join(homeDir, ".npm", "bin"),
			filepath.Join(homeDir, ".volta", "bin"),
			filepath.Join(homeDir, ".bun", "bin"),
		)
	}
	parts = append(parts, "/usr/local/bin", "/usr/bin", "/bin", "/snap/bin", "/opt/homebrew/bin")
	return strings.Join(parts, ":")
}

func systemdUnitValue(value string) string {
	value = strings.TrimSpace(value)
	if needsSystemdQuoting(value) {
		return strconv.Quote(value)
	}
	return value
}

func needsSystemdQuoting(value string) bool {
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return value == ""
}

func parseSystemdShow(output []byte) (string, string, string) {
	lines := strings.Split(string(output), "\n")
	fields := map[string]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return fields["LoadState"], fields["ActiveState"], fields["UnitFileState"]
}
