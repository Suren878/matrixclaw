package dialog

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
)

// ActionClose closes the current dialog.
type ActionClose struct{}

// ActionQuit quits the TUI.
type ActionQuit = tea.QuitMsg

// ActionExternalEditor opens the external editor with the current draft.
type ActionExternalEditor struct{}

// ActionOpenCommands returns to the command menu.
type ActionOpenCommands struct{}

// ActionRunControlplaneCommand executes a shared controlplane command.
type ActionRunControlplaneCommand struct {
	Command string
}

// ActionPermissionResponse is emitted when the user responds to a permission dialog.
type ActionPermissionResponse struct {
	Permission surfacepermission.PermissionRequest
	Action     PermissionAction
}

// ActionConfirmRunCancel is emitted when the user confirms or declines run cancelation.
type ActionConfirmRunCancel struct {
	RunID     string
	Confirmed bool
}

// ActionCmd carries a tea command back to the outer runtime.
type ActionCmd struct {
	Cmd tea.Cmd
}

func controlplaneCommandOrClose(command string) Action {
	if command := strings.TrimSpace(command); command != "" {
		return ActionRunControlplaneCommand{Command: command}
	}
	return ActionClose{}
}
