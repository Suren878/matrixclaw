package commandui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type TextFieldData struct {
	Value       string
	Placeholder string
	Inset       int
	Active      bool
}

func RenderTextField(frame Frame, data TextFieldData) string {
	frame = frame.WithInnerWidth(0)
	width := max(1, frame.InnerWidth()-max(0, data.Inset))
	style := frame.styles().Row
	if data.Active {
		style = frame.styles().StatusAccent.PaddingLeft(frame.styles().Row.GetHorizontalFrameSize())
	}
	value := data.Value
	if strings.TrimSpace(value) == "" && strings.TrimSpace(data.Placeholder) != "" {
		style = frame.styles().Muted.PaddingLeft(frame.styles().Row.GetHorizontalFrameSize())
		value = data.Placeholder
	}
	return renderInputLine(style, value, width)
}

func renderInputLine(style lipgloss.Style, value string, width int) string {
	if width <= 0 {
		return ""
	}
	contentWidth := max(0, width-style.GetHorizontalFrameSize())
	value = ansi.Truncate(strings.TrimRight(value, " "), contentWidth, "")
	return style.Width(width).Render(value)
}
