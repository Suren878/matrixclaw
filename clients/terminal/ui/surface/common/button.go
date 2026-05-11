package common

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

// ButtonOpts defines the configuration for a single button.
type ButtonOpts struct {
	Text           string
	UnderlineIndex int
	Selected       bool
	Padding        int
}

// Button creates a button with an underlined character and selection state.
func Button(t *styles.Styles, opts ButtonOpts) string {
	style := t.ButtonBlur
	if opts.Selected {
		style = t.ButtonFocus
	}

	text := opts.Text
	if opts.Padding == 0 {
		opts.Padding = 2
	}
	if opts.UnderlineIndex > -1 && opts.UnderlineIndex > len(text)-1 {
		opts.UnderlineIndex = -1
	}

	text = style.Padding(0, opts.Padding).Render(text)
	if opts.UnderlineIndex != -1 {
		text = lipgloss.StyleRanges(
			text,
			lipgloss.NewRange(
				opts.Padding+opts.UnderlineIndex,
				opts.Padding+opts.UnderlineIndex+1,
				style.Underline(true),
			),
		)
	}

	return text
}

// ButtonGroup creates a row of selectable buttons.
func ButtonGroup(t *styles.Styles, buttons []ButtonOpts, spacing string) string {
	if len(buttons) == 0 {
		return ""
	}
	if spacing == "" {
		spacing = "  "
	}

	parts := make([]string, len(buttons))
	for i, button := range buttons {
		parts[i] = Button(t, button)
	}
	return strings.Join(parts, spacing)
}
