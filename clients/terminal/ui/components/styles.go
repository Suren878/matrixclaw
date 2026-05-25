package components

import (
	"charm.land/lipgloss/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/theme"
)

type Styles struct {
	Card           lipgloss.Style
	Title          lipgloss.Style
	Subtitle       lipgloss.Style
	Muted          lipgloss.Style
	Divider        lipgloss.Style
	Footer         lipgloss.Style
	Error          lipgloss.Style
	Row            lipgloss.Style
	RowSelected    lipgloss.Style
	RowDisabled    lipgloss.Style
	Status         lipgloss.Style
	StatusAccent   lipgloss.Style
	StatusWarning  lipgloss.Style
	StatusSelected lipgloss.Style
	Button         lipgloss.Style
	ButtonSelected lipgloss.Style
	Danger         lipgloss.Style
	DangerSelected lipgloss.Style
}

func DefaultStyles() Styles {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Text))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Muted))
	selected := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.SelectedFg)).
		Background(lipgloss.Color(theme.Bright)).
		Bold(true)

	return Styles{
		Card: lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color(theme.Border)),
		Title:          base.Foreground(lipgloss.Color(theme.Bright)).Bold(true).PaddingLeft(1),
		Subtitle:       base.PaddingLeft(1),
		Muted:          muted,
		Divider:        muted.Foreground(lipgloss.Color(theme.Border)),
		Footer:         muted.PaddingLeft(1),
		Error:          base.Foreground(lipgloss.Color(theme.Danger)).Bold(true).PaddingLeft(1),
		Row:            base.PaddingLeft(1),
		RowSelected:    selected.PaddingLeft(1),
		RowDisabled:    muted.PaddingLeft(1),
		Status:         muted,
		StatusAccent:   base.Foreground(lipgloss.Color(theme.Bright)).Bold(true),
		StatusWarning:  base.Foreground(lipgloss.Color(theme.Warning)).Bold(true),
		StatusSelected: lipgloss.NewStyle().Foreground(lipgloss.Color(theme.SelectedFg)).Bold(true),
		Button:         base.Foreground(lipgloss.Color(theme.Bright)).Padding(0, 1),
		ButtonSelected: selected.Padding(0, 1),
		Danger:         base.Foreground(lipgloss.Color(theme.Danger)).Padding(0, 1),
		DangerSelected: base.Foreground(lipgloss.Color(theme.SelectedFg)).Background(lipgloss.Color(theme.Danger)).Bold(true).Padding(0, 1),
	}
}

func (s Styles) RowStyles(width int) RowStyles {
	return NewRowStyles(
		s.Row,
		s.RowSelected,
		s.RowDisabled,
		s.Status,
		s.StatusAccent,
		s.StatusWarning,
		s.StatusSelected,
	).WithWidth(width)
}
