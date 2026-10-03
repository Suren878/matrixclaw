package runtime

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

func menuApp(t *testing.T) *appModel {
	t.Helper()
	m, _ := renderApp(t, 110, 34, core.ClientSnapshot{SessionID: "session_1", Session: renderSession(), Context: renderContext()})
	return m
}

// runCommand runs command as the commands menu or the editor would and
// delivers result for it.
func runCommand(m *appModel, command string, result controlplane.Result) {
	m.Update(surfacedialog.ActionRunControlplaneCommand{Command: command})
	m.Update(controlplaneResultMsg{command: command, seq: m.dialog.seq, result: result})
}

func escape(m *appModel) {
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
}

func TestResultsOfAMenuCommandLeadBackToTheMenu(t *testing.T) {
	m := menuApp(t)
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})

	runCommand(m, "/todo", controlplane.Result{Text: "Todo\nnothing yet"})
	if top := m.dialog.DialogLast(); top == nil || top.ID() != surfacedialog.InfoID {
		t.Fatalf("result dialog = %v", top)
	}
	escape(m)

	if top := m.dialog.DialogLast(); top == nil || top.ID() != surfacedialog.CommandsID {
		t.Fatalf("closing the result did not return to the menu: %v", top)
	}
	escape(m)
	if m.dialog.HasDialogs() {
		t.Fatal("closing the menu left dialogs open")
	}
}

func TestResultsOfATypedCommandCloseToTheChat(t *testing.T) {
	m := menuApp(t)

	m.Update(controlplaneResultMsg{command: "/todo", seq: m.dialog.seq, result: controlplane.Result{Text: "Todo\nnothing yet"}})
	if top := m.dialog.DialogLast(); top == nil || top.ID() != surfacedialog.InfoID {
		t.Fatalf("result dialog = %v", top)
	}
	escape(m)

	if m.dialog.HasDialogs() {
		t.Fatalf("closing a typed command's result opened %v", m.dialog.DialogLast().ID())
	}
}

func TestAPickerOpenedFromTheMenuOffersTheWayBack(t *testing.T) {
	m := menuApp(t)
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})

	runCommand(m, "/sessions", controlplane.Result{Picker: &controlplane.PickerData{Title: "Sessions", Items: []controlplane.PickerItem{{ID: "s1", Title: "Main", Command: "/sessions use s1"}}}})
	if !strings.Contains(ansi.Strip(m.viewContent()), "Back") {
		t.Fatalf("picker lacks Back:\n%s", ansi.Strip(m.viewContent()))
	}
	escape(m)

	if top := m.dialog.DialogLast(); top == nil || top.ID() != surfacedialog.CommandsID || !strings.Contains(ansi.Strip(m.viewContent()), "Providers") {
		t.Fatalf("closing the picker did not return to the menu:\n%s", ansi.Strip(m.viewContent()))
	}
}

func TestACommandsResultTextIsNotShownAsAnError(t *testing.T) {
	m := menuApp(t)

	m.Update(controlplaneResultMsg{command: "/new", seq: m.dialog.seq, result: controlplane.Result{Text: "Started a new session.", ReloadSnapshot: true}})

	screen := ansi.Strip(m.viewContent())
	if !strings.Contains(screen, "Started a new session.") || strings.Contains(screen, "ERROR") {
		t.Fatalf("status line:\n%s", screen)
	}
}
