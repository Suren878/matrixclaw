package dialog

import surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"

// CommandsID is the identifier for the commands popup.
const CommandsID = "commands"

type CommandEntry = PickerEntry

// CommandsData is the minimal runtime state needed to render the commands popup.
type CommandsData struct {
	Title   string
	Legend  string
	Entries []CommandEntry
}

// NewCommands creates the commands popup using the generic picker dialog.
func NewCommands(com *surfacecommon.Common, data CommandsData) *Picker {
	return NewPicker(com, PickerData{
		ID:      CommandsID,
		Title:   data.Title,
		Legend:  data.Legend,
		Filter:  false,
		Entries: data.Entries,
	})
}
