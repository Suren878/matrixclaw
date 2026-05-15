package dialog

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

const ConfirmCommandID = "confirm_command"

type ConfirmCommandData = controlplane.ConfirmData

type ConfirmCommand struct {
	data     ConfirmCommandData
	selected int
	keyMap   twoButtonConfirmKeyMap
}

func NewConfirmCommand(_ *surfacecommon.Common, data ConfirmCommandData) *ConfirmCommand {
	return &ConfirmCommand{
		data:   data,
		keyMap: defaultTwoButtonConfirmKeyMap(),
	}
}

func (*ConfirmCommand) ID() string { return ConfirmCommandID }

func (d *ConfirmCommand) HandleMsg(msg tea.Msg) Action {
	switch handleTwoButtonConfirmKey(msg, &d.selected, d.keyMap) {
	case twoButtonConfirmKeyClose:
		return d.cancelAction()
	case twoButtonConfirmKeySelect:
		if d.selected == 0 {
			return ActionRunControlplaneCommand{Command: d.data.ConfirmCommand}
		}
		return d.cancelAction()
	}
	return nil
}

func (d *ConfirmCommand) cancelAction() Action {
	return controlplaneCommandOrClose(d.data.CancelCommand)
}

func (d *ConfirmCommand) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	view := commandui.RenderConfirmCard(commandui.NewFrame(area.Dx(), area.Dy()), commandui.ConfirmData{
		Message:       strings.TrimSpace(d.data.Message),
		ConfirmLabel:  firstNonEmptyTrimmed(d.data.ConfirmLabel, "Confirm"),
		CancelLabel:   firstNonEmptyTrimmed(d.data.CancelLabel, "Cancel"),
		Selected:      d.selected,
		ConfirmDanger: d.data.ConfirmDanger,
		CancelDanger:  d.data.CancelDanger,
	})
	DrawCenter(scr, area, view)
	return nil
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
