package dialog

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

const ConfirmCommandID = "confirm_command"

type ConfirmCommandData = controlplane.ConfirmData

type ConfirmCommand struct {
	data    ConfirmCommandData
	state   components.ConfirmState
	loading bool
	frame   int
}

func NewConfirmCommand(_ *surfacecommon.Common, data ConfirmCommandData) *ConfirmCommand {
	return &ConfirmCommand{
		data: data,
	}
}

func (*ConfirmCommand) ID() string { return ConfirmCommandID }

func (d *ConfirmCommand) HandleMsg(msg tea.Msg) Action {
	if _, ok := msg.(loadingTickMsg); ok {
		if !d.loading {
			return nil
		}
		d.frame = (d.frame + 1) % len(loadingFrames)
		return ActionCmd{Cmd: loadingTickCmd()}
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if d.loading {
		return nil
	}
	switch d.state.Update(keyMsg.String()).Kind {
	case components.EventCancel:
		return d.cancelAction()
	case components.EventSubmit:
		return ActionRunControlplaneCommand{Command: d.data.ConfirmCommand}
	}
	return nil
}

func (d *ConfirmCommand) cancelAction() Action {
	return controlplaneCommandOrClose(d.data.CancelCommand)
}

func (d *ConfirmCommand) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	message := strings.TrimSpace(d.data.Message)
	if d.loading {
		message = strings.TrimSpace(message + " " + loadingFrame(d.frame))
	}
	view := components.RenderConfirmCard(components.NewFrame(area.Dx(), area.Dy()), components.ConfirmData{
		Message:       message,
		ConfirmLabel:  firstNonEmptyTrimmed(d.data.ConfirmLabel, "Confirm"),
		CancelLabel:   firstNonEmptyTrimmed(d.data.CancelLabel, "Cancel"),
		Selected:      d.state.Selected,
		ConfirmDanger: d.data.ConfirmDanger,
		CancelDanger:  d.data.CancelDanger,
		OnlyCancel:    d.loading,
	})
	DrawCenter(scr, area, view)
	return nil
}

func (d *ConfirmCommand) StartLoading() tea.Cmd {
	d.loading = true
	d.frame = 0
	return loadingTickCmd()
}

func (d *ConfirmCommand) StopLoading() {
	d.loading = false
	d.frame = 0
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
