package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/colorx"
)

type CursorShape int

const (
	CursorBlock CursorShape = iota
)

type TextInputStyleState struct {
	Text        lipgloss.Style
	Placeholder lipgloss.Style
	Prompt      lipgloss.Style
	Suggestion  lipgloss.Style
}

type TextInputCursorStyle struct {
	Color color.Color
	Shape CursorShape
	Blink bool
}

type TextInputStyles struct {
	Focused TextInputStyleState
	Blurred TextInputStyleState
	Cursor  TextInputCursorStyle
}

type TextAreaStyleState struct {
	Base             lipgloss.Style
	Text             lipgloss.Style
	LineNumber       lipgloss.Style
	CursorLine       lipgloss.Style
	CursorLineNumber lipgloss.Style
	Placeholder      lipgloss.Style
	Prompt           lipgloss.Style
}

type TextAreaCursorStyle struct {
	Color color.Color
	Shape CursorShape
	Blink bool
}

type TextAreaStyles struct {
	Focused TextAreaStyleState
	Blurred TextAreaStyleState
	Cursor  TextAreaCursorStyle
}

func new[T any](value T) *T {
	return &value
}

func tone(key charmtone.Key) color.Color {
	return key
}

func gloss(c color.Color) color.Color {
	return c
}

func hex(c color.Color) string {
	return colorx.Hex(c)
}
