package commandui

import "strings"

type FormData struct {
	Title      string
	Fields     []Item
	Focus      FormFocus
	Buttons    []ButtonSpec
	Button     int
	Help       string
	Error      string
	ExtraLines []string
}

func RenderFormCard(frame Frame, data FormData) string {
	help := firstNonEmpty(data.Help, "enter select · ↑/↓ move · ←/→ action · esc back")
	frame = frame.WithInnerWidth(0)
	styles := frame.styles()
	fieldFocus := -1
	if data.Focus.Kind == FormFocusField {
		fieldFocus = data.Focus.Index
	}
	body := renderRows(styles, itemRows(data.Fields), fieldFocus, frame.InnerWidth())
	actionFocused := data.Focus.Kind == FormFocusButton
	body = append(body, "")
	body = append(body, renderFormButtons(styles, frame.InnerWidth(), formButtonsOrDefault(data.Buttons), data.Button, actionFocused))
	for _, line := range data.ExtraLines {
		if strings.TrimSpace(line) != "" {
			body = append(body, "", renderTruncated(styles.Footer, line, frame.InnerWidth()))
		}
	}
	return frame.RenderCard(FrameData{
		Title: data.Title,
		Body:  body,
		Help:  help,
		Error: data.Error,
	})
}

func formButtonsOrDefault(buttons []ButtonSpec) []ButtonSpec {
	if len(buttons) > 0 {
		return buttons
	}
	return []ButtonSpec{
		{Label: "Save", Role: RoleSubmit},
		{Label: "Back", Role: RoleBack},
	}
}

func renderFormButtons(styles Styles, width int, specs []ButtonSpec, selected int, focused bool) string {
	return renderButtonSpecs(styles, width, specs, selected, focused, true)
}
