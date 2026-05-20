package dialog

import (
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
)

const ConfirmRunCancelID = "confirm_run_cancel"

type ConfirmRunCancel struct {
	runID string
	state commandui.ConfirmState
}

func NewConfirmRunCancel(_ *surfacecommon.Common, runID string) *ConfirmRunCancel {
	return &ConfirmRunCancel{
		runID: runID,
	}
}

func (d *ConfirmRunCancel) ID() string {
	return ConfirmRunCancelID
}

func (d *ConfirmRunCancel) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch d.state.Update(keyMsg.String()).Kind {
	case commandui.EventCancel:
		return ActionConfirmRunCancel{RunID: d.runID, Confirmed: false}
	case commandui.EventSubmit:
		return ActionConfirmRunCancel{RunID: d.runID, Confirmed: true}
	}
	return nil
}

func (d *ConfirmRunCancel) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	view := commandui.RenderConfirmCard(commandui.NewFrame(area.Dx(), area.Dy()), commandui.ConfirmData{
		Message:       "The current response will stop and the run will be marked as canceled.",
		ConfirmLabel:  "Cancel request",
		CancelLabel:   "Keep waiting",
		Selected:      d.state.Selected,
		ConfirmDanger: true,
	})
	DrawCenter(scr, area, view)
	return nil
}
