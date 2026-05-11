package clientcmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	appsetup "github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/version"
)

func runVersionCommand(stdout io.Writer, binaryName string, service *appsetup.Service) int {
	fmt.Fprintf(stdout, "%s: client: %s\n", binaryName, version.String())

	cfg, err := service.Load()
	if err != nil {
		return 0
	}
	health, err := configuredDaemonClient(cfg).Health(context.Background())
	if err != nil {
		fmt.Fprintf(stdout, "%s: daemon: unavailable (%v)\n", binaryName, err)
		return 0
	}
	if !health.OK {
		fmt.Fprintf(stdout, "%s: daemon: unhealthy\n", binaryName)
		return 0
	}
	daemonVersion := strings.TrimSpace(health.Version.Version)
	if daemonVersion == "" {
		daemonVersion = "unknown"
	}
	if strings.TrimSpace(health.Version.Commit) != "" {
		daemonVersion += " (" + strings.TrimSpace(health.Version.Commit) + ")"
	}
	fmt.Fprintf(stdout, "%s: daemon: %s\n", binaryName, daemonVersion)
	return 0
}
