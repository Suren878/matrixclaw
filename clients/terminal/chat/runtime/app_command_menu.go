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
	if m.dialog.ContainsDialog(surfacedialog.CommandsID) {
		m.dialog.BringToFront(surfacedialog.CommandsID)
		return nil
	}
	m.returnToCommands = false
	m.closeControlplaneDialogs()
	m.dialog.OpenDialog(surfacedialog.NewCommands(m.com, surfacedialog.CommandsData{
		Title:   "Commands",
		Legend:  "enter run · esc back",
		Entries: commandmenu.Entries(m.commandMenuState()),
	}))
	return nil
}

func (m *appModel) commandMenuState() commandmenu.State {
	return commandmenu.State{
		SessionTitle:            m.currentSessionTitle(),
		ProviderID:              m.currentProviderID(),
		ModelID:                 m.currentModelLabel(),
		PermissionMode:          m.currentPermissionMode(),
		ExternalEditorAvailable: strings.TrimSpace(os.Getenv("EDITOR")) != "",
	}
}

func (m *appModel) currentPermissionMode() core.PermissionMode {
	if m.read == nil {
		return core.PermissionModeDefault
	}
	if session := m.read.Snapshot().Session; session != nil {
		return core.NormalizePermissionMode(string(session.PermissionMode))
	}
	return core.PermissionModeDefault
}

func (m *appModel) currentSessionTitle() string {
	if m.read == nil {
		return ""
	}
	snapshot := m.read.Snapshot()
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
