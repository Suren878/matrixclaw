package clientcmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		fmt.Fprintf(stderr, "%s: agents: %v\n", binaryName, err)
		return 1
	}
	if len(agents) == 0 {
		fmt.Fprintf(stdout, "%s: agents: none\n", binaryName)
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
			fmt.Fprintf(stdout, "%s: %s [%s] %s\n", binaryName, agent.DisplayName, state, agent.Mode)
			continue
		}
		fmt.Fprintf(stdout, "%s: %s [%s]\n", binaryName, agent.DisplayName, state)
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
	agentID := normalizeAgentID(args[0])
	workingDir, err := resolveAgentWorkingDir(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "%s: agents start: %v\n", binaryName, err)
		return 2
	}

	cfg, err := service.Load()
	if err != nil {
		return handleSetupReadError(stderr, binaryName, service, "agents start", err)
	}
	client := configuredDaemonClient(cfg)
	agents, err := client.ListExternalAgents(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "%s: agents start: %v\n", binaryName, err)
		return 1
	}
	if !externalAgentReady(agents, agentID) {
		fmt.Fprintf(stderr, "%s: agents start: external agent %q is not enabled\n", binaryName, agentID)
		return 1
	}

	session, err := client.CreateSessionWithRequest(context.Background(), core.CreateSessionRequest{
		Title:           externalAgentSessionTitle(agents, agentID),
		Kind:            string(core.SessionKindExternalAgent),
		RuntimeID:       string(core.SessionRuntimeExternalAgent),
		WorkingDir:      workingDir,
		PermissionMode:  string(core.PermissionModeFullAuto),
		ExternalAgentID: agentID,
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s: agents start: %v\n", binaryName, err)
		return 1
	}
	fmt.Fprintf(stdout, "%s: external session: %s (%s)\n", binaryName, session.ID, session.Title)
	return 0
}

func resolveAgentWorkingDir(args []string) (string, error) {
	value := ""
	if len(args) > 0 {
		value = strings.TrimSpace(args[0])
	}
	if value == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return wd, nil
	}
	abs, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("WORKDIR is not a directory: %s", abs)
	}
	return abs, nil
}

func normalizeAgentID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func externalAgentReady(agents []core.ExternalAgentDescriptor, agentID string) bool {
	for _, agent := range agents {
		if externalAgentMatches(agent, agentID) {
			return agent.Installed && agent.Enabled
		}
	}
	return false
}

func externalAgentSessionTitle(agents []core.ExternalAgentDescriptor, agentID string) string {
	for _, agent := range agents {
		if externalAgentMatches(agent, agentID) && strings.TrimSpace(agent.DisplayName) != "" {
			return strings.TrimSpace(agent.DisplayName)
		}
	}
	return agentID
}

func externalAgentMatches(agent core.ExternalAgentDescriptor, agentID string) bool {
	agentID = strings.ToLower(strings.TrimSpace(agentID))
	if strings.EqualFold(strings.TrimSpace(agent.ID), agentID) {
		return true
	}
	for _, alias := range agent.Aliases {
		if strings.EqualFold(strings.TrimSpace(alias), agentID) {
			return true
		}
	}
	return false
}

func printAgentsUsage(w io.Writer, binaryName string) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s agents                    List external agent runtimes\n", binaryName)
	fmt.Fprintf(w, "  %s agents start AGENT [DIR]  Create an external-agent session\n", binaryName)
}

func isHelpArg(value string) bool {
	value = strings.TrimSpace(value)
	return value == "help" || value == "-h" || value == "--help"
}
