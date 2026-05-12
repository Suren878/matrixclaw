package clientcmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	tuiruntime "github.com/Suren878/matrixclaw/clients/terminal/chat/runtime"
	terminalsetup "github.com/Suren878/matrixclaw/clients/terminal/setup"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	appsetup "github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/terminalrender"
)

type IO struct {
	Stdout io.Writer
	Stderr io.Writer
}

var (
	newSetupService = appsetup.NewDefaultService
	openSetupUI     = terminalsetup.Run
	openTUI         = tuiruntime.Run
	ensureDaemon    = func(ctx context.Context, service *appsetup.Service) (appsetup.DaemonSummary, error) {
		return service.EnsureDaemonContext(ctx)
	}
	restartService = func(ctx context.Context, service *appsetup.Service) (appsetup.DaemonSummary, error) {
		return service.RestartDaemonContext(ctx)
	}
	readServiceLogs = defaultReadServiceLogs
	newDaemonClient = func(baseURL string) *daemonclient.Client {
		return daemonclient.New(baseURL, "doctor", "local")
	}
)

func configuredDaemonClient(cfg appsetup.Config) *daemonclient.Client {
	return newDaemonClient(daemonBaseURL(cfg.Daemon.HTTPAddr)).WithAPIToken(cfg.Daemon.APIToken)
}

func Run(io IO, binaryName string, args []string) int {
	terminalrender.Configure()

	stdout := io.Stdout
	stderr := io.Stderr

	service, err := newSetupService()
	if err != nil {
		fmt.Fprintf(stderr, "%s: setup service: %v\n", binaryName, err)
		return 1
	}

	command := ""
	if len(args) > 0 {
		command = args[0]
	}

	switch command {
	case "":
		return runDefaultCommand(stdout, stderr, binaryName, service)
	case "setup":
		return runSetupCommand(stdout, stderr, binaryName, service)
	case "status":
		return runStatusCommand(stdout, stderr, binaryName, service)
	case "version":
		return runVersionCommand(stdout, binaryName, service)
	case "doctor":
		return runDoctorCommand(stdout, stderr, binaryName, service)
	case "service":
		return runServiceCommand(stdout, stderr, binaryName, service, args[1:])
	case "providers":
		return runProvidersCommand(stdout, stderr, binaryName, service, args[1:])
	case "tui":
		return runTUICommand(stderr, binaryName, service)
	case "help", "-h", "--help":
		printUsage(stdout, binaryName)
		return 0
	default:
		printUsage(stdout, binaryName)
		return 2
	}
}

func runDefaultCommand(stdout io.Writer, stderr io.Writer, binaryName string, service *appsetup.Service) int {
	configured, err := service.IsConfigured()
	if err != nil {
		fmt.Fprintf(stderr, "%s: setup: %v\n", binaryName, err)
		return 1
	}
	if !configured {
		return runSetupCommand(stdout, stderr, binaryName, service)
	}
	return runTUICommand(stderr, binaryName, service)
}

func runSetupCommand(stdout io.Writer, stderr io.Writer, binaryName string, service *appsetup.Service) int {
	result, err := openSetupUI(context.Background(), service)
	if errors.Is(err, terminalsetup.ErrAborted) {
		return 130
	}
	if err != nil {
		fmt.Fprintf(stderr, "%s: setup: %v\n", binaryName, err)
		return 1
	}
	fmt.Fprintf(stdout, "%s: wrote setup to %s\n", binaryName, result.Path)
	return 0
}

func printUsage(w io.Writer, binaryName string) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s                  Open TUI when configured, otherwise setup\n", binaryName)
	fmt.Fprintf(w, "  %s setup            Open the setup UI\n", binaryName)
	fmt.Fprintf(w, "  %s status           Print setup and matrixclaw service state\n", binaryName)
	fmt.Fprintf(w, "  %s doctor           Diagnose setup, daemon, and provider registry\n", binaryName)
	fmt.Fprintf(w, "  %s version          Print client and daemon version\n", binaryName)
	fmt.Fprintf(w, "  %s service status   Print matrixclaw service state\n", binaryName)
	fmt.Fprintf(w, "  %s service restart  Restart matrixclaw service\n", binaryName)
	fmt.Fprintf(w, "  %s service logs     Print recent matrixclaw service logs\n", binaryName)
	fmt.Fprintf(w, "  %s providers        List setup provider catalog\n", binaryName)
	fmt.Fprintf(w, "  %s providers verify Verify configured provider model access\n", binaryName)
	fmt.Fprintf(w, "  %s tui              Open the terminal chat surface\n", binaryName)
}
