package setup

import (
	"charm.land/lipgloss/v2"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
)

func (m *model) commandFrame() commandui.Frame {
	return commandui.NewFrame(m.width, m.frameRows()).WithInnerWidth(0)
}

func (m *model) renderCommandCard(card string) string {
	width := m.width
	if width == 0 {
		width = 100
	}
	rows := m.frameRows()
	if m.height == 0 {
		rows = lipgloss.Height(card) + 2
	}
	box := centerRect(width, rows, lipgloss.Width(card), lipgloss.Height(card))
	background := m.renderRain(width, rows, box)
	bodyText := overlayAtRect(background, card, width, rows, box)
	return bgStyle.Render(bodyText)
}

func (m *model) frameRows() int {
	height := m.height
	if height == 0 {
		return 24
	}
	return max(1, height-2)
}
