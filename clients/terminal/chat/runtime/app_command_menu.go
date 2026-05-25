package runtime

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/commandmenu"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *appModel) openCommandsDialogCmd() tea.Cmd {
	dialog := surfacedialog.NewCommands(m.com, surfacedialog.CommandsData{
		Title:   "Commands",
		Legend:  "enter run · esc back",
		Entries: commandmenu.Entries(m.commandMenuState()),
	})
	if m.dialog.ContainsDialog(surfacedialog.CommandsID) {
		m.dialog.CloseDialog(surfacedialog.CommandsID)
		m.dialog.OpenDialog(dialog)
		m.dialog.BringToFront(surfacedialog.CommandsID)
		m.returnToCommands = false
		return nil
	}
	m.returnToCommands = false
	m.closeControlplaneDialogs()
	m.dialog.OpenDialog(dialog)
	return nil
}

func (m *appModel) commandMenuState() commandmenu.State {
	return commandmenu.State{
		SessionTitle:            m.currentSessionTitle(),
		ProviderID:              m.currentProviderID(),
		ModelID:                 m.currentModelLabel(),
		PermissionMode:          m.currentPermissionMode(),
		Capabilities:            m.currentSessionCapabilities(),
		ExternalEditorAvailable: strings.TrimSpace(os.Getenv("EDITOR")) != "",
	}
}

func (m *appModel) currentSessionCapabilities() core.SessionCapabilities {
	snapshot := m.currentSnapshot()
	if snapshot.Capabilities != nil {
		return *snapshot.Capabilities
	}
	if snapshot.Session != nil {
		return core.CapabilitiesForSession(*snapshot.Session)
	}
	return core.SessionCapabilities{
		ProviderSelection: true,
		PermissionMode:    true,
		PlanningMode:      true,
		NativeTools:       true,
	}
}

func (m *appModel) currentPermissionMode() core.PermissionMode {
	if session := m.currentSnapshot().Session; session != nil {
		return core.NormalizePermissionMode(string(session.PermissionMode))
	}
	return core.PermissionModeDefault
}

func (m *appModel) currentSessionTitle() string {
	if m.read == nil {
		return ""
	}
	snapshot := m.currentSnapshot()
	if session := snapshot.Session; session != nil {
		if title := strings.TrimSpace(session.Title); title != "" {
			return title
		}
		if id := strings.TrimSpace(session.ID); id != "" {
			return id
		}
	}
	return "matrixclaw"
}

func (m *appModel) currentProviderID() string {
	if providerID, _ := m.currentSessionLLM(); providerID != "" {
		return providerID
	}
	return strings.TrimSpace(m.providerName)
}

func resultTitle(text string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	first = strings.TrimSuffix(first, ":")
	if strings.TrimSpace(first) == "" {
		return "Result"
	}
	return first
}
