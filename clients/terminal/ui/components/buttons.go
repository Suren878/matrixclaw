package components

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
	Label  string
	Role   Role
	Danger bool
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

func renderButtonSpecs(styles Styles, width int, specs []ButtonSpec, selected int, focused bool, dangerByRole bool) string {
	buttons := make([]Button, 0, len(specs))
	for i, spec := range specs {
		buttons = append(buttons, buttonFromSpec(spec, focused && i == selected, dangerByRole))
	}
	return RenderButtons(styles, width, buttons...)
}

func buttonFromSpec(spec ButtonSpec, focused bool, dangerByRole bool) Button {
	return Button{
		Label:   firstNonEmpty(spec.Label, string(spec.Role)),
		Danger:  spec.Danger || dangerByRole && roleIsDestructive(spec.Role),
		Focused: focused,
	}
}

func roleIsDestructive(role Role) bool {
	return role == RoleBack || role == RoleCancel || role == RoleExit
}
