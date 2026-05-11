package dialog

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func (p *Permissions) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, p.keyMap.Close):
			return p.respond(PermissionDeny)
		case key.Matches(msg, p.keyMap.Right), key.Matches(msg, p.keyMap.Tab):
			p.selectedOption = (p.selectedOption + 1) % len(p.permissionOptions())
		case key.Matches(msg, p.keyMap.Left):
			p.selectedOption = (p.selectedOption + len(p.permissionOptions()) - 1) % len(p.permissionOptions())
		case key.Matches(msg, p.keyMap.Select):
			return p.selectCurrentOption()
		case key.Matches(msg, p.keyMap.Allow):
			return p.respond(PermissionAllow)
		case key.Matches(msg, p.keyMap.AllowSession):
			if p.canAllowSession() {
				return p.respond(PermissionAllowSession)
			}
		case key.Matches(msg, p.keyMap.Deny):
			return p.respond(PermissionDeny)
		case key.Matches(msg, p.keyMap.ToggleDiffMode):
			if p.hasDiffView() {
				newMode := !p.isSplitMode()
				p.diffSplitMode = &newMode
				p.viewportDirty = true
			}
		case key.Matches(msg, p.keyMap.ToggleFullscreen):
			if p.hasDiffView() {
				p.fullscreen = !p.fullscreen
			}
		case key.Matches(msg, p.keyMap.ScrollDown), key.Matches(msg, p.keyMap.ScrollUp):
			p.viewport, _ = p.viewport.Update(msg)
		case key.Matches(msg, p.keyMap.ScrollLeft):
			if p.hasDiffView() {
				p.scrollLeft()
			} else {
				p.viewport, _ = p.viewport.Update(msg)
			}
		case key.Matches(msg, p.keyMap.ScrollRight):
			if p.hasDiffView() {
				p.scrollRight()
			} else {
				p.viewport, _ = p.viewport.Update(msg)
			}
		default:
			if !p.hasDiffView() {
				p.viewport, _ = p.viewport.Update(msg)
				p.viewportDirty = true
			}
		}
	case tea.MouseMsg:
		mouse := msg.Mouse()
		if _, ok := msg.(tea.MouseClickMsg); ok && mouse.Button == tea.MouseLeft {
			if action, ok := p.handleMouseClick(msg); ok {
				return p.respond(action)
			}
		}
		if isWheelMouse(msg) {
			if p.hasDiffView() {
				switch mouse.Button {
				case tea.MouseWheelLeft:
					p.scrollLeft()
				case tea.MouseWheelRight:
					p.scrollRight()
				default:
					p.viewport, _ = p.viewport.Update(msg)
				}
			} else {
				p.viewport, _ = p.viewport.Update(msg)
			}
		}
	}

	return nil
}

func (p *Permissions) handleMouseClick(msg tea.MouseMsg) (PermissionAction, bool) {
	if p.lastView == "" || p.lastViewRect.Empty() {
		return "", false
	}

	mouse := msg.Mouse()
	x := mouse.X - p.lastViewRect.Min.X
	y := mouse.Y - p.lastViewRect.Min.Y
	if x < 0 || y < 0 {
		return "", false
	}

	lines := strings.Split(p.lastView, "\n")
	if y >= len(lines) {
		return "", false
	}

	type buttonHit struct {
		action PermissionAction
		line   int
		start  int
		end    int
	}

	findHits := func() []buttonHit {
		buttons := p.permissionOptions()

		hits := make([]buttonHit, 0, len(buttons))
		for i, line := range lines {
			plain := ansi.Strip(line)
			for _, btn := range buttons {
				idx := strings.Index(plain, btn.label)
				if idx < 0 {
					continue
				}
				start := max(0, idx-2)
				end := idx + len(btn.label) + 2
				hits = append(hits, buttonHit{
					action: btn.action,
					line:   i,
					start:  start,
					end:    end,
				})
			}
		}
		return hits
	}

	for _, hit := range findHits() {
		if hit.line == y && x >= hit.start && x <= hit.end {
			return hit.action, true
		}
	}

	return "", false
}

func (p *Permissions) selectCurrentOption() tea.Msg {
	options := p.permissionOptions()
	if len(options) == 0 {
		return p.respond(PermissionDeny)
	}
	if p.selectedOption < 0 || p.selectedOption >= len(options) {
		p.selectedOption = 0
	}
	return p.respond(options[p.selectedOption].action)
}

func (p *Permissions) respond(action PermissionAction) tea.Msg {
	return ActionPermissionResponse{
		Permission: p.permission,
		Action:     action,
	}
}

func (p *Permissions) permissionOptions() []permissionOption {
	options := []permissionOption{
		{label: "Allow", action: PermissionAllow, underlineIndex: 0},
	}
	if p.canAllowSession() {
		options = append(options, permissionOption{label: "Allow Session", action: PermissionAllowSession, underlineIndex: 6})
	}
	options = append(options, permissionOption{label: "Deny", action: PermissionDeny, underlineIndex: 0})
	return options
}

func (p *Permissions) canAllowSession() bool {
	switch p.permission.ToolName {
	case toolNameEdit, toolNameWrite, toolNameMultiEdit:
		return true
	default:
		return false
	}
}

func (p *Permissions) hasDiffView() bool {
	switch p.permission.ToolName {
	case toolNameEdit, toolNameWrite, toolNameMultiEdit:
		return true
	}
	return false
}

func (p *Permissions) isSplitMode() bool {
	if p.diffSplitMode != nil {
		return *p.diffSplitMode
	}
	return p.defaultDiffSplitMode
}

func (p *Permissions) scrollLeft() {
	p.diffXOffset = max(0, p.diffXOffset-horizontalScrollStep)
	p.viewportDirty = true
}

func (p *Permissions) scrollRight() {
	p.diffXOffset += horizontalScrollStep
	p.viewportDirty = true
}

func isWheelMouse(msg tea.MouseMsg) bool {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp, tea.MouseWheelDown, tea.MouseWheelLeft, tea.MouseWheelRight:
		return true
	default:
		return false
	}
}
