package dialog

import (
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
)

const ConfirmRunCancelID = "confirm_run_cancel"

type ConfirmRunCancel struct {
	runID string
	state components.ConfirmState
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
	case components.EventCancel:
		return ActionConfirmRunCancel{RunID: d.runID, Confirmed: false}
	case components.EventSubmit:
		return ActionConfirmRunCancel{RunID: d.runID, Confirmed: true}
	}
	return nil
}

func (d *ConfirmRunCancel) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	view := components.RenderConfirmCard(components.NewFrame(area.Dx(), area.Dy()), components.ConfirmData{
		Message:       "The current response will stop and the run will be marked as canceled.",
		ConfirmLabel:  "Cancel request",
		CancelLabel:   "Keep waiting",
		Selected:      d.state.Selected,
		ConfirmDanger: true,
	})
	DrawCenter(scr, area, view)
	return nil
}
