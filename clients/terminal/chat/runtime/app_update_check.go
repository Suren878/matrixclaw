package runtime

import (
	"bytes"
	"context"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/updater"
)

const updateInfoDialogID = "update_info"

func (m *appModel) checkUpdateCmd() tea.Cmd {
	if skipUpdateCheck() {
		return nil
	}
	current := strings.TrimSpace(m.version)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		update, ok, err := updater.Checker{}.Check(ctx, current)
		return updateCheckMsg{update: update, ok: ok, err: err}
	}
}

func (m *appModel) handleUpdateCheck(msg updateCheckMsg) {
	if msg.err != nil || !msg.ok || m.updates.prompted {
		return
	}
	m.updates.prompted = true
	m.dialog.openUnasked(surfacedialog.NewConfirmCommand(m.com, controlplane.ConfirmData{
		Message:        "A new matrixclaw release is available: " + msg.update.Current + " -> " + msg.update.Latest + "\n\nUpdate now?",
		ConfirmLabel:   "Yes",
		CancelLabel:    "No",
		ConfirmCommand: "/update install " + msg.update.Latest,
	}))
}

// handleUpdateCommand runs the terminal-local "/update install <version>" and
// "/update restart" (restart the daemon, then reopen the terminal) commands.
func (m *appModel) handleUpdateCommand(command string) tea.Cmd {
	fields := strings.Fields(command)
	if len(fields) == 2 && fields[0] == "/update" && fields[1] == "restart" {
		return m.openServerRestartDialog(true)
	}
	if len(fields) < 3 || fields[0] != "/update" || fields[1] != "install" {
		return nil
	}
	version := strings.TrimSpace(fields[2])
	if version == "" || m.updates.installing {
		return nil
	}
	m.updates.installing = true
	m.dialog.CloseDialog(surfacedialog.ConfirmCommandID)
	m.dialog.OpenDialog(surfacedialog.NewInfo(m.com, surfacedialog.InfoData{
		ID:    updateInfoDialogID,
		Title: "Update",
		Text:  "Installing " + version + "...",
	}))
	return m.installUpdateCmd(version)
}

func (m *appModel) installUpdateCmd(version string) tea.Cmd {
	return func() tea.Msg {
		var out bytes.Buffer
		var errOut bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := updater.Installer{Stdout: &out, Stderr: &errOut}.Install(ctx, version)
		output := strings.TrimSpace(out.String())
		if errOut.Len() > 0 {
			if output != "" {
				output += "\n"
			}
			output += strings.TrimSpace(errOut.String())
		}
		return updateInstallMsg{version: version, output: output, err: err}
	}
}

func (m *appModel) handleUpdateInstall(msg updateInstallMsg) {
	m.updates.installing = false
	if msg.err != nil {
		if info, ok := m.dialog.Dialog(updateInfoDialogID).(*surfacedialog.Info); ok {
			info.SetText("Update failed: " + msg.err.Error() + updateOutputSuffix(msg.output))
			return
		}
		m.dialog.OpenDialog(surfacedialog.NewInfo(m.com, surfacedialog.InfoData{
			ID:    updateInfoDialogID,
			Title: "Update",
			Text:  "Update failed: " + msg.err.Error() + updateOutputSuffix(msg.output),
		}))
		return
	}
	m.dialog.CloseDialog(updateInfoDialogID)
	m.dialog.openUnasked(surfacedialog.NewConfirmCommand(m.com, controlplane.ConfirmData{
		Message:        "Updated to " + strings.TrimSpace(msg.version) + ".\n\nRestart daemon now? Terminal will reopen after restart.",
		ConfirmLabel:   "Yes",
		CancelLabel:    "No",
		ConfirmCommand: "/update restart",
	}))
}

func updateOutputSuffix(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return "\n\n" + output
}

func skipUpdateCheck() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MATRIXCLAW_SKIP_UPDATE_CHECK"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
