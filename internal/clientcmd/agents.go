package clientcmd

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	appsetup "github.com/Suren878/matrixclaw/internal/setup"
)

func runAgentsCommand(stdout io.Writer, stderr io.Writer, binaryName string, service *appsetup.Service, args []string) int {
	if len(args) > 0 && isHelpArg(args[0]) {
		printAgentsUsage(stdout, binaryName)
		return 0
	}
	if len(args) > 0 && strings.TrimSpace(args[0]) == "start" {
		return runAgentStartCommand(stdout, stderr, binaryName, service, args[1:])
	}
	if len(args) > 0 {
		printAgentsUsage(stdout, binaryName)
		return 2
	}
	cfg, err := service.Load()
	if err != nil {
		return handleSetupReadError(stderr, binaryName, service, "agents", err)
	}
	agents, err := configuredDaemonClient(cfg).ListExternalAgents(context.Background())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: agents: %v\n", binaryName, err)
		return 1
	}
	if len(agents) == 0 {
		_, _ = fmt.Fprintf(stdout, "%s: agents: none\n", binaryName)
		return 0
	}
	for _, agent := range agents {
		state := "disabled"
		switch {
		case !agent.Installed:
			state = "not installed"
		case agent.Enabled:
			state = "enabled"
		}
		if agent.Mode != "" {
			_, _ = fmt.Fprintf(stdout, "%s: %s [%s] %s\n", binaryName, agent.DisplayName, state, agent.Mode)
			continue
		}
		_, _ = fmt.Fprintf(stdout, "%s: %s [%s]\n", binaryName, agent.DisplayName, state)
	}
	return 0
}

func runAgentStartCommand(stdout io.Writer, stderr io.Writer, binaryName string, service *appsetup.Service, args []string) int {
	if len(args) > 0 && isHelpArg(args[0]) {
		printAgentsUsage(stdout, binaryName)
		return 0
	}
	if len(args) < 1 || len(args) > 2 {
		printAgentsUsage(stdout, binaryName)
		return 2
	}
	agentID := strings.ToLower(strings.TrimSpace(args[0]))
	workingDir, err := resolveWorkingDir(strings.Join(args[1:], ""))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: agents start: %v\n", binaryName, err)
		return 2
	}

	cfg, err := service.Load()
	if err != nil {
		return handleSetupReadError(stderr, binaryName, service, "agents start", err)
	}
	client := configuredDaemonClient(cfg)
	agents, err := client.ListExternalAgents(context.Background())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: agents start: %v\n", binaryName, err)
		return 1
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok || !agent.Installed || !agent.Enabled {
		_, _ = fmt.Fprintf(stderr, "%s: agents start: external agent %q is not enabled\n", binaryName, agentID)
		return 1
	}

	session, err := client.CreateSessionWithRequest(context.Background(), core.CreateSessionRequest{
		Title:           cmp.Or(strings.TrimSpace(agent.DisplayName), agentID),
		Kind:            string(core.SessionKindExternalAgent),
		RuntimeID:       string(core.SessionRuntimeExternalAgent),
		WorkingDir:      workingDir,
		PermissionMode:  string(core.PermissionModeFullAuto),
		ExternalAgentID: agentID,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: agents start: %v\n", binaryName, err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "%s: external session: %s (%s)\n", binaryName, session.ID, session.Title)
	return 0
}

func findExternalAgent(agents []core.ExternalAgentDescriptor, agentID string) (core.ExternalAgentDescriptor, bool) {
	for _, agent := range agents {
		if strings.EqualFold(agent.ID, agentID) || slices.ContainsFunc(agent.Aliases, func(alias string) bool {
			return strings.EqualFold(alias, agentID)
		}) {
			return agent, true
		}
	}
	return core.ExternalAgentDescriptor{}, false
}

func printAgentsUsage(w io.Writer, binaryName string) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintf(w, "  %s agents                    List external agent runtimes\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s agents start AGENT [DIR]  Create an external-agent session\n", binaryName)
}

func isHelpArg(value string) bool {
	value = strings.TrimSpace(value)
	return value == "help" || value == "-h" || value == "--help"
}
