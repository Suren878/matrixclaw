package runtime

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestMouseEventFilterDropsRapidMouseNoise(t *testing.T) {
	filter := newMouseEventFilter()
	msg := tea.MouseWheelMsg{Button: tea.MouseWheelDown}

	if got := filter.Filter(nil, msg); got == nil {
		t.Fatal("expected first mouse event to pass")
	}
	if got := filter.Filter(nil, msg); got != nil {
		t.Fatal("expected second rapid mouse event to be dropped")
	}

	time.Sleep(mouseNoiseThreshold)

	if got := filter.Filter(nil, msg); got == nil {
		t.Fatal("expected mouse event after threshold to pass")
	}
}

func TestMousePressInEditorFocusesEditor(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, nil)
	model.width = 100
	model.height = 30
	model.read = viewmodel.NewReadModel(snapshotWithTexts(now, "first"))
	model.rebuildChat()
	model.focus = appFocusChat
	model.input.Blur()

	editorTop, _ := model.editorBounds()
	next, cmd := model.handleMouse(tea.MouseClickMsg{X: 2, Y: editorTop, Button: tea.MouseLeft})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected focus command")
	}
	if model.focus != appFocusEditor {
		t.Fatalf("focus = %v, want editor", model.focus)
	}
}

func TestCtrlGTogglesExpandedHelp(t *testing.T) {
	model := newApp(nil, nil)
	if model.help.ShowAll {
		t.Fatal("expected help to start collapsed")
	}
	next, cmd := model.handleKey(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no command")
	}
	if !model.help.ShowAll {
		t.Fatal("expected help to expand")
	}
}

func TestEscWhileBusyOpensCancelRunDialog(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, nil)
	model.width = 100
	model.height = 30
	model.focus = appFocusEditor
	model.read = viewmodel.NewReadModel(core.ClientSnapshot{
		SessionID: "session-1",
		Run: &core.Run{
			ID:        "run-1",
			SessionID: "session-1",
			Status:    core.RunStatusRunning,
			StartedAt: now,
			UpdatedAt: now,
		},
	})
	model.setBusy(true)

	next, cmd := model.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no async command when opening confirm dialog")
	}
	if !model.dialog.ContainsDialog(surfacedialog.ConfirmRunCancelID) {
		t.Fatal("expected cancel run dialog to open")
	}
}

func TestCtrlSOpensSessionsDialogCommand(t *testing.T) {
	model := newApp(nil, &Runtime{})
	next, cmd := model.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected sessions dialog command")
	}
}

func TestCtrlPOpensCommandsDialog(t *testing.T) {
	model := newApp(nil, nil)
	next, cmd := model.handleKey(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no follow-up command")
	}
	if !model.dialog.ContainsDialog(surfacedialog.CommandsID) {
		t.Fatal("expected commands dialog to open")
	}
}
