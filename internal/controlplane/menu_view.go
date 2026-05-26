package controlplane

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

type MenuState struct {
	SessionTitle   string
	ProviderID     string
	ModelID        string
	PermissionMode core.PermissionMode
	Capabilities   core.SessionCapabilities
}

type CommandView struct {
	ID       string
	Command  string
	Title    string
	Status   string
	Group    MenuItemGroup
	Public   bool
	Menu     bool
	Disabled bool
}

func BuildCommandView(state MenuState) []CommandView {
	items := make([]CommandView, 0, len(Catalog()))
	for _, spec := range Catalog() {
		title := spec.Description
		status := ""
		group := MenuItemGroupPrimary
		disabled := false
		switch spec.ID {
		case CommandNewSession:
			title = "New Session"
		case CommandSessions:
			title = "Sessions"
			if value := strings.TrimSpace(state.SessionTitle); value != "" {
				status = value
			}
		case CommandProvider:
			title = "Provider"
			if !state.Capabilities.ProviderSelection && hasSessionCapabilities(state.Capabilities) {
				status = "Matrixclaw only"
				disabled = true
			} else if value := strings.TrimSpace(state.ProviderID); value != "" {
				status = value
			}
		case CommandPermissions:
			title = "Permission Mode"
			if !state.Capabilities.PermissionMode && hasSessionCapabilities(state.Capabilities) {
				status = "Matrixclaw only"
				disabled = true
			} else {
				status = permissionModeStatus(state.PermissionMode)
			}
		case CommandContext:
			title = "Context"
			group = MenuItemGroupSecondary
		case CommandPlan:
			title = "Planning Mode"
			if !state.Capabilities.PlanningMode && hasSessionCapabilities(state.Capabilities) {
				status = "Matrixclaw only"
				disabled = true
			}
		case CommandMemory:
			title = "Memory"
			group = MenuItemGroupSecondary
		case CommandModules:
			title = "Modules"
			group = MenuItemGroupSecondary
		case CommandTasks:
			title = "Tasks"
		case CommandServer:
			title = "Server"
			group = MenuItemGroupSecondary
		case CommandHelp:
			title = "Help"
		case CommandRemind:
			title = "Reminder"
		case CommandStatus:
			title = "Server Status"
		case CommandRestart:
			title = "Restart Daemon"
		case CommandStop:
			title = "Stop Daemon"
		}
		items = append(items, CommandView{
			ID:       string(spec.ID),
			Command:  spec.Command,
			Title:    title,
			Status:   status,
			Group:    group,
			Public:   spec.Public,
			Menu:     spec.Menu,
			Disabled: disabled,
		})
	}
	return items
}

func hasSessionCapabilities(capabilities core.SessionCapabilities) bool {
	return capabilities.ProviderSelection ||
		capabilities.PermissionMode ||
		capabilities.PlanningMode ||
		capabilities.NativeTools ||
		capabilities.ExternalAgent
}

func PublicCommandView() []CommandView {
	views := BuildCommandView(MenuState{})
	out := make([]CommandView, 0, len(views))
	for _, view := range views {
		if view.Public && (view.Menu || view.ID == string(CommandHelp)) {
			out = append(out, view)
		}
	}
	return out
}

func permissionModeStatus(mode core.PermissionMode) string {
	switch core.NormalizePermissionMode(string(mode)) {
	case core.PermissionModeAcceptEdits:
		return "Edits only"
	case core.PermissionModeFullAuto:
		return "Full auto"
	default:
		return "Ask first"
	}
}
