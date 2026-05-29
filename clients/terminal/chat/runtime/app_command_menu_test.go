package runtime

import (
	"context"
	"testing"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func TestActionOpenCommandsClosesControlplaneResultDialogs(t *testing.T) {
	m := newApp(context.Background(), nil)
	m.openCommandsDialogCmd()
	m.returnToCommands = true

	if !m.showControlplaneTextResult(controlplane.Result{Text: "Memory:\n- project: note"}) {
		t.Fatalf("showControlplaneTextResult returned false, want text result dialog")
	}
	if !m.dialog.ContainsDialog(surfacedialog.CommandsID) || !m.dialog.ContainsDialog(surfacedialog.InfoID) {
		t.Fatalf("expected command and info dialogs before returning to commands")
	}

	m.Update(surfacedialog.ActionOpenCommands{})

	if !m.dialog.ContainsDialog(surfacedialog.CommandsID) {
		t.Fatalf("commands dialog was not reopened")
	}
	if m.dialog.ContainsDialog(surfacedialog.InfoID) {
		t.Fatalf("info dialog remained below commands after returning to command menu")
	}
	if top := m.dialog.DialogLast(); top == nil || top.ID() != surfacedialog.CommandsID {
		t.Fatalf("top dialog = %#v, want commands", top)
	}
}
