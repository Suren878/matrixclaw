package controlplane

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

var modulesRoot = valueVerb(CommandModules, "", 0)

// handleModules serves the module list, the screens of the modules that
// have their own (named by a verb's first word) and the settings screens.
func (d *Dispatcher) handleModules(ctx context.Context, r Route) (Result, error) {
	screen, _, _ := strings.Cut(r.Verb.words, " ")
	if !d.owner() && slices.Contains(ownerScreens, screen) {
		return textResult(ownerOnlySettings), nil
	}
	switch screen {
	case "agents":
		return d.handleExternalAgents(ctx, r)
	case "storage":
		return d.handleStorage(ctx, r)
	case "skills":
		return d.handleSkillsForExternal(ctx, r)
	case "mcp":
		return d.handleMCP(ctx, r)
	case "":
		if r.Value == "" {
			return d.modulesPicker(ctx)
		}
		return d.moduleSettingsPage(ctx, r.Value, nil, "")
	default:
		return d.handleModuleSettings(ctx, r)
	}
}

// clientScreens are the modules this client draws itself, collections and
// editors rather than settings, by the verb of their first screen.
var clientScreens = map[string]Verb{"storage": storageRootVerb, "skills": skillsRoot, "mcp": mcpRoot}

// ownerScreens are the client's screens that only edit the owner's settings.
var ownerScreens = []string{"agents", "skills", "mcp"}

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
		Command(modulesRoot.command()).
		Item(d.moduleRow("agents", "External Agents", agentsInfo, agentsRoot.command()))
	for _, status := range statuses {
		if screen, ok := clientScreens[status.ID]; ok {
			picker.Item(d.moduleRow(status.ID, status.Title, status.State, screen.command()))
		} else if status.Settings {
			picker.Item(d.moduleRow(status.ID, status.Title, status.State, settingsPageVerb.command(status.ID)))
		}
	}
	return Result{Screen: picker.Build()}, nil
}

// moduleRow opens a module; non-owners see the owner's screens as status only.
func (d *Dispatcher) moduleRow(id string, title string, info string, command string) PickerItem {
	row := PickerItem{ID: id, Title: title, Info: info, Command: command}
	if !d.owner() && slices.Contains(ownerScreens, id) {
		row.Command, row.Disabled = "", true
	}
	return row
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
