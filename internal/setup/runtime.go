package setup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strings"
	"time"
)

const DaemonUnitName = "matrixclawd.service"

const daemonUnitName = DaemonUnitName

var stableBinaryPattern = regexp.MustCompile(`[\\/](go-build|_tmp)[\\/]|\.cache[\\/]+go-build`)

type daemonManager interface {
	Apply(ctx context.Context, setupPath string, cfg Config) (DaemonSummary, []string, error)
	Inspect(ctx context.Context, setupPath string, cfg Config) (DaemonSummary, error)
	Restart(ctx context.Context, setupPath string, cfg Config) (DaemonSummary, error)
}

type telegramValidator interface {
	Validate(ctx context.Context, token string) (TelegramSummary, error)
}

type commandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type systemUser struct {
	Username string
	HomeDir  string
}

type execRunner struct{}

type systemdUserDaemonManager struct {
	runner        commandRunner
	lookPath      func(string) (string, error)
	resolveDaemon func() (string, error)
	currentUser   func() (systemUser, error)
	checkLinger   func(context.Context, string) (bool, error)
	httpClient    *http.Client
}

type telegramHTTPValidator struct {
	client *http.Client
}

func newSystemdUserDaemonManager() *systemdUserDaemonManager {
	return &systemdUserDaemonManager{
		runner:        execRunner{},
		lookPath:      exec.LookPath,
		resolveDaemon: resolveDaemonBinary,
		currentUser: func() (systemUser, error) {
			usr, err := user.Current()
			if err != nil {
				return systemUser{}, err
			}
			return systemUser{
				Username: usr.Username,
				HomeDir:  firstNonEmptyTrimmed(strings.TrimSpace(usr.HomeDir), strings.TrimSpace(os.Getenv("HOME"))),
			}, nil
		},
		checkLinger: userLingerEnabled,
		httpClient:  &http.Client{Timeout: 2 * time.Second},
	}
}

func (m *systemdUserDaemonManager) Apply(ctx context.Context, setupPath string, cfg Config) (DaemonSummary, []string, error) {
	summary := daemonConfiguredSummary(cfg)
	warnings := []string{}

	if _, _, err := writeDaemonEnvironmentFile(setupPath, cfg); err != nil {
		return summary, warnings, err
	}

	daemonRunning := m.healthCheck(ctx, cfg.Daemon.HTTPAddr) == nil
	liveReloaded := false
	var liveReloadErr error
	if daemonRunning {
		liveReloadErr = m.reloadLiveDaemon(ctx, cfg)
		liveReloaded = liveReloadErr == nil
	}

	if _, err := m.lookPath("systemctl"); err == nil {
		daemonBin, err := m.resolveDaemon()
		if err != nil {
			return summary, warnings, fmt.Errorf("resolve matrixclawd binary: %w", err)
		}

		usr, err := m.currentUser()
		if err != nil {
			return summary, warnings, fmt.Errorf("resolve current user: %w", err)
		}
		if strings.TrimSpace(usr.HomeDir) == "" {
			return summary, warnings, errors.New("resolve HOME for systemd user service")
		}

		if err := m.applyWithSystemd(ctx, setupPath, daemonBin, usr, cfg, liveReloaded, &warnings); err != nil {
			if liveReloaded {
				return summary, warnings, err
			}
			if daemonRunning {
				if liveReloadErr != nil {
					return summary, warnings, fmt.Errorf("reload running daemon: %w", liveReloadErr)
				}
				return summary, warnings, err
			}
			if directErr := m.startDirect(ctx, daemonBin, setupPath, cfg); directErr != nil {
				return summary, warnings, fmt.Errorf("%v; direct launch fallback failed: %w", err, directErr)
			}
			warnings = append(warnings, "systemd unavailable; using direct daemon launch")
		}
	} else {
		switch {
		case liveReloaded:
			warnings = append(warnings, "systemd unavailable; live daemon reloaded")
		case daemonRunning && liveReloadErr != nil:
			return summary, warnings, fmt.Errorf("reload running daemon: %w", liveReloadErr)
		default:
			daemonBin, err := m.resolveDaemon()
			if err != nil {
				return summary, warnings, fmt.Errorf("resolve matrixclawd binary: %w", err)
			}
			if err := m.startDirect(ctx, daemonBin, setupPath, cfg); err != nil {
				return summary, warnings, err
			}
			warnings = append(warnings, "systemd unavailable; using direct daemon launch")
		}
	}

	inspected, err := m.Inspect(ctx, setupPath, cfg)
	if err != nil {
		return summary, warnings, err
	}
	if len(warnings) > 0 {
		inspected.Warning = strings.Join(warnings, " | ")
	}
	return inspected, warnings, nil
}

func (m *systemdUserDaemonManager) Inspect(ctx context.Context, _ string, cfg Config) (DaemonSummary, error) {
	summary := daemonConfiguredSummary(cfg)
	if _, err := m.lookPath("systemctl"); err != nil {
		return m.inspectViaHealth(ctx, cfg, "systemctl is not available")
	}

	output, err := m.runner.Output(
		ctx,
		"systemctl",
		"--user",
		"show",
		daemonUnitName,
		"--property=LoadState",
		"--property=ActiveState",
		"--property=UnitFileState",
	)
	if err != nil {
		return m.inspectViaHealth(ctx, cfg, strings.TrimSpace(string(output)))
	}

	loadState, activeState, unitFileState := parseSystemdShow(output)
	summary.Installed = loadState != "" && loadState != "not-found"
	summary.Running = activeState == "active"
	summary.Enabled = unitFileState == "enabled"

	switch {
	case !summary.Installed:
		summary.RuntimeStatus = "Not installed"
	case summary.Running:
		summary.RuntimeStatus = "Running"
	default:
		summary.RuntimeStatus = "Stopped"
	}

	return summary, nil
}

func daemonConfiguredSummary(cfg Config) DaemonSummary {
	return DaemonSummary{
		Status:        daemonStatus(cfg.Daemon.HTTPAddr, cfg.Daemon.DBPath),
		HTTPAddr:      cfg.Daemon.HTTPAddr,
		DBPath:        cfg.Daemon.DBPath,
		Autostart:     cfg.Daemon.AutostartOnBoot,
		RuntimeStatus: "Unknown",
	}
}
