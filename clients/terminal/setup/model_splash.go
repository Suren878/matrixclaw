package setup

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func (m *model) renderSplash() string {
	width := m.width
	if width == 0 {
		width = 100
	}
	height := m.height
	if height == 0 {
		height = 32
	}
	subtitle := "press enter to begin setup"
	if m.hasExisting {
		subtitle = "press enter to reopen setup"
	}
	popup := splashPopupStyle.Render(strings.Join([]string{
		centerBlock(60, logoWordmark(60)),
		"",
		centerBlock(60, setupSubtitleStyle.Render("One daemon. Thin clients. Real provider profiles.")),
		"",
		centerBlock(60, setupFooterStyle.Render(subtitle)),
		"",
		centerBlock(60, splashEnterStyle.Render("ENTER")),
	}, "\n"))
	box := centerRect(width, height, lipgloss.Width(popup), lipgloss.Height(popup))
	background := m.renderRain(width, height, box)
	return bgStyle.Render(overlayAtRect(background, popup, width, height, box))
}
