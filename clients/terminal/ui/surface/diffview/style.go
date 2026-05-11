package diffview

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
)

// LineStyle defines the styles for a given line type in the diff view.
type LineStyle struct {
	LineNumber lipgloss.Style
	Symbol     lipgloss.Style
	Code       lipgloss.Style
}

// Style defines the overall style for the diff view, including styles for
// different line types such as divider, missing, equal, insert, and delete
// lines.
type Style struct {
	DividerLine LineStyle
	MissingLine LineStyle
	EqualLine   LineStyle
	InsertLine  LineStyle
	DeleteLine  LineStyle
}

// DefaultLightStyle provides a default light theme style for the diff view.
func DefaultLightStyle() Style {
	return Style{
		DividerLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(tone(charmtone.Iron)).
				Background(tone(charmtone.Thunder)),
			Code: lipgloss.NewStyle().
				Foreground(tone(charmtone.Oyster)).
				Background(tone(charmtone.Anchovy)),
		},
		MissingLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Background(tone(charmtone.Ash)),
			Code: lipgloss.NewStyle().
				Background(tone(charmtone.Ash)),
		},
		EqualLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(tone(charmtone.Charcoal)).
				Background(tone(charmtone.Ash)),
			Code: lipgloss.NewStyle().
				Foreground(tone(charmtone.Pepper)).
				Background(tone(charmtone.Salt)),
		},
		InsertLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(tone(charmtone.Turtle)).
				Background(lipgloss.Color("#c8e6c9")),
			Symbol: lipgloss.NewStyle().
				Foreground(tone(charmtone.Turtle)).
				Background(lipgloss.Color("#e8f5e9")),
			Code: lipgloss.NewStyle().
				Foreground(tone(charmtone.Pepper)).
				Background(lipgloss.Color("#e8f5e9")),
		},
		DeleteLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(tone(charmtone.Cherry)).
				Background(lipgloss.Color("#ffcdd2")),
			Symbol: lipgloss.NewStyle().
				Foreground(tone(charmtone.Cherry)).
				Background(lipgloss.Color("#ffebee")),
			Code: lipgloss.NewStyle().
				Foreground(tone(charmtone.Pepper)).
				Background(lipgloss.Color("#ffebee")),
		},
	}
}

// DefaultDarkStyle provides a default dark theme style for the diff view.
func DefaultDarkStyle() Style {
	return Style{
		DividerLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(tone(charmtone.Smoke)).
				Background(tone(charmtone.Sapphire)),
			Code: lipgloss.NewStyle().
				Foreground(tone(charmtone.Smoke)).
				Background(tone(charmtone.Ox)),
		},
		MissingLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Background(tone(charmtone.Charcoal)),
			Code: lipgloss.NewStyle().
				Background(tone(charmtone.Charcoal)),
		},
		EqualLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(tone(charmtone.Ash)).
				Background(tone(charmtone.Charcoal)),
			Code: lipgloss.NewStyle().
				Foreground(tone(charmtone.Salt)).
				Background(tone(charmtone.Pepper)),
		},
		InsertLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(tone(charmtone.Turtle)).
				Background(lipgloss.Color("#293229")),
			Symbol: lipgloss.NewStyle().
				Foreground(tone(charmtone.Turtle)).
				Background(lipgloss.Color("#303a30")),
			Code: lipgloss.NewStyle().
				Foreground(tone(charmtone.Salt)).
				Background(lipgloss.Color("#303a30")),
		},
		DeleteLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(tone(charmtone.Cherry)).
				Background(lipgloss.Color("#332929")),
			Symbol: lipgloss.NewStyle().
				Foreground(tone(charmtone.Cherry)).
				Background(lipgloss.Color("#3a3030")),
			Code: lipgloss.NewStyle().
				Foreground(tone(charmtone.Salt)).
				Background(lipgloss.Color("#3a3030")),
		},
	}
}
