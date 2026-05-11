package commandui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type Button struct {
	Label   string
	Danger  bool
	Focused bool
}

type ButtonSpec struct {
	Label string
	Role  Role
}

func RenderButtons(styles Styles, width int, buttons ...Button) string {
	parts := make([]string, 0, len(buttons))
	for _, button := range buttons {
		label := strings.TrimSpace(button.Label)
		if label == "" {
			continue
		}
		style := styles.Button
		if button.Danger {
			style = styles.Danger
		}
		if button.Focused {
			style = styles.ButtonSelected
			if button.Danger {
				style = styles.DangerSelected
			}
		}
		parts = append(parts, style.Render(label))
	}
	line := strings.Join(parts, "  ")
	if width <= 0 {
		return line
	}
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(line)
}
