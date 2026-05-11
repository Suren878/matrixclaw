package runtime

import (
	"time"

	tea "charm.land/bubbletea/v2"
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
	if !isWheelMouse(mouse) && !isMotion {
		return msg
	}
	now := time.Now()
	if now.Sub(f.lastMouseEvent) < mouseNoiseThreshold {
		return nil
	}
	f.lastMouseEvent = now
	return msg
}

func isWheelMouse(msg tea.MouseMsg) bool {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp, tea.MouseWheelDown, tea.MouseWheelLeft, tea.MouseWheelRight:
		return true
	default:
		return false
	}
}
