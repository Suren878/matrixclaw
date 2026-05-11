package dialog

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

// TextInputCursor returns a best-effort terminal cursor for a text input.
func TextInputCursor(input textinput.Model, styles surfacestyles.TextInputStyles) *uv.Cursor {
	if !input.Focused() {
		return nil
	}

	x := lipgloss.Width(input.Prompt) + textInputCursorX(input)
	cur := uv.NewCursor(x, 0)
	cur.Color = styles.Cursor.Color
	cur.Shape = uv.CursorBlock
	cur.Blink = styles.Cursor.Blink
	return cur
}

func applyTextInputStyles(input *textinput.Model, styles surfacestyles.TextInputStyles) {
	next := textinput.DefaultDarkStyles()
	next.Focused.Text = styles.Focused.Text
	next.Focused.Placeholder = styles.Focused.Placeholder
	next.Focused.Prompt = styles.Focused.Prompt
	next.Focused.Suggestion = styles.Focused.Suggestion
	next.Blurred.Text = styles.Blurred.Text
	next.Blurred.Placeholder = styles.Blurred.Placeholder
	next.Blurred.Prompt = styles.Blurred.Prompt
	next.Blurred.Suggestion = styles.Blurred.Suggestion
	next.Cursor.Color = styles.Cursor.Color
	next.Cursor.Blink = styles.Cursor.Blink
	input.SetStyles(next)
}

func textInputCursorX(input textinput.Model) int {
	pos := input.Position()
	runes := []rune(input.Value())
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}

	prefix := string(runes[:pos])
	inputWidth := input.Width()
	if inputWidth <= 0 {
		return ansi.StringWidth(prefix)
	}

	width := ansi.StringWidth(prefix)
	if width <= inputWidth {
		return width
	}

	visibleWidth := 0
	for i := len(runes[:pos]) - 1; i >= 0; i-- {
		runeWidth := ansi.StringWidth(string(runes[i]))
		if visibleWidth+runeWidth > inputWidth {
			break
		}
		visibleWidth += runeWidth
	}
	return visibleWidth
}
