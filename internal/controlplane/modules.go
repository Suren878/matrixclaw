package controlplane

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

func (d *Dispatcher) handleModules(ctx context.Context, args string) (Result, error) {
	step, rest := firstCommandStep(args)
	switch step {
	case "":
		return d.modulesPicker(ctx)
	case "agents":
		return d.handleExternalAgents(ctx, rest)
	case "storage":
		return d.handleStorage(ctx, rest)
	case realtime.ModuleID:
		return d.handleRealtimeVoiceModule(ctx, rest)
	case "skills":
		return d.handleSkillsForExternal(ctx, rest)
	case "mcp":
		return d.handleMCP(ctx, rest)
	default:
		return d.handleModuleSettings(ctx, step, rest)
	}
}

// clientScreens are the modules this client draws itself: collections and
// editors rather than settings.
var clientScreens = []string{"storage", realtime.ModuleID, "skills", "mcp"}

// modulesPicker lists the external agents and every module with a screen,
// with the daemon's status line.
func (d *Dispatcher) modulesPicker(ctx context.Context) (Result, error) {
	statuses, err := d.daemon.Modules(ctx)
	if err != nil {
		return Result{}, err
	}
	agentsInfo := ""
	if agents, err := d.daemon.ListExternalAgents(ctx); err == nil {
		agentsInfo = externalAgentsModuleInfo(agents)
	}
	picker := NewPickerData(PickerModules, "Modules").
		Command(modulesCommand()).
		Row("agents", "External Agents", agentsInfo, externalAgentsCommand())
	for _, status := range statuses {
		if status.Settings || slices.Contains(clientScreens, status.ID) {
			picker.Row(status.ID, status.Title, status.State, modulesCommand(status.ID))
		}
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func externalAgentsModuleInfo(agents []core.ExternalAgentDescriptor) string {
	if len(agents) == 0 {
		return ""
	}
	enabled := make([]string, 0, len(agents))
	installed := make([]string, 0, len(agents))
	for _, agent := range agents {
		title := externalAgentTitle(agent)
		if title == "" {
			continue
		}
		if agent.Enabled {
			enabled = append(enabled, title)
		}
		if agent.Installed {
			installed = append(installed, title)
		}
	}
	switch len(enabled) {
	case 1:
		return enabled[0]
	case 2:
		return strings.Join(enabled, ", ")
	default:
		if len(enabled) > 2 {
			return strconv.Itoa(len(enabled)) + " enabled"
		}
	}
	if len(installed) == 1 {
		return "Disabled · " + installed[0]
	}
	if len(installed) > 1 {
		return strconv.Itoa(len(installed)) + " installed · disabled"
	}
	return "Not installed"
}
