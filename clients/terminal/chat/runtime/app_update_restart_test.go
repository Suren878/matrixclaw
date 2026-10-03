package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/core"
)

func restartAfterUpdate(t *testing.T, answer string) string {
	t.Helper()
	m := newApp(context.Background(), nil)
	m.width, m.height = 120, 40
	m.handleUpdateInstall(updateInstallMsg{version: "v9.9.9"})
	m.handleDialogInput(tea.KeyPressMsg{Code: rune(answer[0]), Text: answer})
	if answer == "n" {
		if m.dialog.HasDialogs() {
			t.Fatal("declined restart dialog still open")
		}
		m.handleRunControlplaneCommand(surfacedialog.ActionRunControlplaneCommand{Command: "/restart confirm"}, false)
	}
	m.handleServerRestartPoll(serverRestartPollMsg{deliveries: []core.ClientDelivery{{
		ID: "d1", Type: core.ClientDeliveryTypeDaemonRestart, Status: core.ClientDeliveryStatusReady, CreatedAt: time.Now().UTC(),
	}}})
	return ansi.Strip(m.viewContent())
}

func TestDeclinedPostUpdateRestartDoesNotReopenTheTerminalLater(t *testing.T) {
	if view := restartAfterUpdate(t, "n"); strings.Contains(view, "Restarting terminal") {
		t.Fatalf("a later daemon restart reopened the terminal:\n%s", view)
	}
}

func TestAcceptedPostUpdateRestartReopensTheTerminal(t *testing.T) {
	if view := restartAfterUpdate(t, "y"); !strings.Contains(view, "Restarting terminal") {
		t.Fatalf("the terminal does not reopen after the update restart:\n%s", view)
	}
}
