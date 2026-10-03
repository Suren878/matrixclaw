package runtime

import surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"

// menuState is how the open dialogs relate to the commands menu.
type menuState int

const (
	menuNone menuState = iota
	// menuRoot: the commands menu is open as the root dialog.
	menuRoot
	// menuReturn: a command started in the menu runs; its results lead back.
	menuReturn
)

// dialogs is the dialog stack and what the terminal tracks about it.
type dialogs struct {
	*surfacedialog.Overlay
	menu menuState
	// seq numbers controlplane requests; only the latest one's result shows.
	seq uint64
	// suppressed are approvals answered here that the daemon has not yet resolved.
	suppressed map[string]struct{}
}

// commandStarted moves a command run from the open menu into its flow, and any
// other command out of it.
func (d *dialogs) commandStarted() {
	inMenu := d.menu == menuRoot && d.ContainsDialog(surfacedialog.CommandsID)
	if inMenu || (d.menu == menuReturn && d.HasDialogs()) {
		d.menu = menuReturn
		return
	}
	d.menu = menuNone
}

// menuClosed records that the root commands menu is gone.
func (d *dialogs) menuClosed() {
	if d.menu == menuRoot {
		d.menu = menuNone
	}
}

// fromMenu reports whether results lead back to the commands menu.
func (d *dialogs) fromMenu() bool { return d.menu == menuReturn }
