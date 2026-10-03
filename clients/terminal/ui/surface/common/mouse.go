package common

import tea "charm.land/bubbletea/v2"

// IsWheelMouse reports whether msg is a wheel scroll in any direction.
func IsWheelMouse(msg tea.MouseMsg) bool {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp, tea.MouseWheelDown, tea.MouseWheelLeft, tea.MouseWheelRight:
		return true
	default:
		return false
	}
}

// IsMouseRelease reports whether msg releases a mouse button.
func IsMouseRelease(msg tea.MouseMsg) bool {
	_, ok := msg.(tea.MouseReleaseMsg)
	return ok
}

// IsMouseMotion reports whether msg moves the mouse.
func IsMouseMotion(msg tea.MouseMsg) bool {
	_, ok := msg.(tea.MouseMotionMsg)
	return ok
}
