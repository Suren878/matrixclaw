package clientcmd

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	appsetup "github.com/Suren878/matrixclaw/internal/setup"
)

func runStatusCommand(stdout io.Writer, stderr io.Writer, binaryName string, service *appsetup.Service) int {
	summary, err := service.Summary()
	if err != nil {
		return handleSetupReadError(stderr, binaryName, service, "status", err)
	}
	_, _ = fmt.Fprintf(stdout, "%s: setup path: %s\n", binaryName, service.Path())
	_, _ = fmt.Fprintf(stdout, "%s: provider: %s (%s) [%s]\n", binaryName, summary.Provider.Name, summary.Provider.Model, summary.Provider.Status)
	_, _ = fmt.Fprintf(stdout, "%s: api key: %s\n", binaryName, nonEmpty(summary.Provider.APIKeyPreview, "Not configured"))
	printServiceSummary(stdout, binaryName, summary.Daemon)
	_, _ = fmt.Fprintf(stdout, "%s: telegram: %s\n", binaryName, summary.Telegram.Status)
	if summary.Telegram.Username != "" {
		_, _ = fmt.Fprintf(stdout, "%s: telegram bot: @%s\n", binaryName, summary.Telegram.Username)
	}
	if summary.Telegram.Warning != "" {
		_, _ = fmt.Fprintf(stdout, "%s: telegram warning: %s\n", binaryName, summary.Telegram.Warning)
	}
	return 0
}

func runServiceCommand(stdout io.Writer, stderr io.Writer, binaryName string, service *appsetup.Service, args []string) int {
	subcommand := ""
	if len(args) > 0 {
		subcommand = strings.TrimSpace(args[0])
	}
	switch subcommand {
	case "status":
		summary, err := service.Summary()
		if err != nil {
			return handleSetupReadError(stderr, binaryName, service, "service status", err)
		}
		printServiceSummary(stdout, binaryName, summary.Daemon)
		return 0
	case "restart":
		_, _ = fmt.Fprintf(stdout, "%s: restarting matrixclaw service...\n", binaryName)
		summary, err := restartService(context.Background(), service)
		if err != nil {
			return handleSetupReadError(stderr, binaryName, service, "service restart", err)
		}
		_, _ = fmt.Fprintf(stdout, "%s: matrixclaw service restarted\n", binaryName)
		printServiceSummary(stdout, binaryName, summary)
		return 0
	case "stop":
		_, _ = fmt.Fprintf(stdout, "%s: stopping matrixclaw service...\n", binaryName)
		summary, err := stopService(context.Background(), service)
		if err != nil {
			return handleSetupReadError(stderr, binaryName, service, "service stop", err)
		}
		_, _ = fmt.Fprintf(stdout, "%s: matrixclaw service stopped\n", binaryName)
		printServiceSummary(stdout, binaryName, summary)
		return 0
	case "logs":
		return runServiceLogsCommand(stdout, stderr, binaryName, args[1:])
	case "help", "-h", "--help":
		printServiceUsage(stdout, binaryName)
		return 0
	default:
		printServiceUsage(stdout, binaryName)
		return 2
	}
}

func runServiceLogsCommand(stdout io.Writer, stderr io.Writer, binaryName string, args []string) int {
	lines := 80
	if len(args) > 0 {
		parsed, err := strconv.Atoi(strings.TrimSpace(args[0]))
		if err != nil || parsed <= 0 {
			_, _ = fmt.Fprintf(stderr, "%s: service logs: line count must be a positive number\n", binaryName)
			return 2
		}
		lines = parsed
	}
	logs, err := readServiceLogs(context.Background(), lines)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: service logs: %v\n", binaryName, err)
		return 1
	}
	_, _ = fmt.Fprint(stdout, logs)
	if !strings.HasSuffix(logs, "\n") {
		_, _ = fmt.Fprintln(stdout)
	}
	return 0
}

func printServiceUsage(w io.Writer, binaryName string) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintf(w, "  %s service status   Print matrixclaw service state\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s service restart  Restart matrixclaw service\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s service stop     Stop matrixclaw service\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s service logs     Print recent matrixclaw service logs\n", binaryName)
}

func printServiceSummary(w io.Writer, binaryName string, summary appsetup.DaemonSummary) {
	_, _ = fmt.Fprintf(w, "%s: service: %s\n", binaryName, summary.RuntimeStatus)
	_, _ = fmt.Fprintf(w, "%s: api: %s\n", binaryName, summary.HTTPAddr)
	_, _ = fmt.Fprintf(w, "%s: database: %s\n", binaryName, summary.DBPath)
	_, _ = fmt.Fprintf(w, "%s: autostart: %s\n", binaryName, yesNo(summary.Autostart))
	if summary.Warning != "" {
		_, _ = fmt.Fprintf(w, "%s: service warning: %s\n", binaryName, summary.Warning)
	}
}

func defaultReadServiceLogs(ctx context.Context, lines int) (string, error) {
	if lines <= 0 {
		lines = 80
	}
	cmd := exec.CommandContext(ctx, "journalctl", "--user", "-u", appsetup.DaemonUnitName, "-n", strconv.Itoa(lines), "--no-pager")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
