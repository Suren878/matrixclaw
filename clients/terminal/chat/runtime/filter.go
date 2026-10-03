package runtime

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
)

const mouseNoiseThreshold = 15 * time.Millisecond

type mouseEventFilter struct {
	lastMouseEvent time.Time
}

func newMouseEventFilter() *mouseEventFilter {
	return &mouseEventFilter{}
}

func (f *mouseEventFilter) Filter(_ tea.Model, msg tea.Msg) tea.Msg {
	mouse, ok := msg.(tea.MouseMsg)
	if !ok {
		return msg
	}
	_, isMotion := msg.(tea.MouseMotionMsg)
	if !common.IsWheelMouse(mouse) && !isMotion {
		return msg
	}
	now := time.Now()
	if now.Sub(f.lastMouseEvent) < mouseNoiseThreshold {
		return nil
	}
	f.lastMouseEvent = now
	return msg
}
