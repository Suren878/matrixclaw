package runtime

import (
	"os"
	"strings"

	"github.com/Suren878/matrixclaw/clients/terminal/commandmenu"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *appModel) openCommandsDialog() {
	dialog := surfacedialog.NewCommands(m.com, surfacedialog.CommandsData{
		Title:   "Commands",
		Legend:  "enter run · esc back",
		Entries: commandmenu.Entries(m.commandMenuState()),
	})
	m.returnToCommands = false
	m.closeControlplaneDialogs()
	m.dialog.OpenDialog(dialog)
	m.commandsDialogRoot = true
}

func (m *appModel) commandMenuState() commandmenu.State {
	provider, model := m.currentSessionLLM()
	return commandmenu.State{
		SessionTitle:            m.currentSessionTitle(),
		ProviderID:              provider,
		ModelID:                 model,
		PermissionMode:          m.currentPermissionMode(),
		Capabilities:            m.state().Capabilities(),
		ExternalEditorAvailable: strings.TrimSpace(os.Getenv("EDITOR")) != "",
	}
}

func (m *appModel) currentPermissionMode() core.PermissionMode {
	if session := m.state().Session(); session != nil {
		return core.NormalizePermissionMode(string(session.PermissionMode))
	}
	return core.PermissionModeDefault
}

func (m *appModel) currentSessionTitle() string {
	if m.read == nil {
		return ""
	}
	if session := m.read.Session(); session != nil {
		if title := strings.TrimSpace(session.Title); title != "" {
			return title
		}
		if id := strings.TrimSpace(session.ID); id != "" {
			return id
		}
	}
	return "matrixclaw"
}

func resultTitle(text string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	first = strings.TrimSuffix(first, ":")
	if strings.TrimSpace(first) == "" {
		return "Result"
	}
	return first
}
