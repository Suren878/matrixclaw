package components

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type RowTone int

const (
	RowToneNormal RowTone = iota
	RowToneAccent
	RowToneWarning
)

type Row struct {
	Title    string
	Status   string
	Disabled bool
	Tone     RowTone
}

type row = Row

type RowStyles struct {
	Row            lipgloss.Style
	RowSelected    lipgloss.Style
	RowDisabled    lipgloss.Style
	Status         lipgloss.Style
	StatusAccent   lipgloss.Style
	StatusWarning  lipgloss.Style
	StatusSelected lipgloss.Style
}

func NewRowStyles(row, selected, disabled, status, accent, warning, selectedStatus lipgloss.Style) RowStyles {
	return RowStyles{
		Row:            row,
		RowSelected:    selected,
		RowDisabled:    disabled,
		Status:         status,
		StatusAccent:   accent,
		StatusWarning:  warning,
		StatusSelected: selectedStatus,
	}
}

func (styles RowStyles) WithWidth(width int) RowStyles {
	styles.Row = styles.Row.Width(width)
	styles.RowSelected = styles.RowSelected.Width(width)
	styles.RowDisabled = styles.RowDisabled.Width(width)
	return styles
}

func RowToneForStatus(status string) RowTone {
	status = strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.HasPrefix(status, "configured"), statusHasWord(status, "active"):
		return RowToneAccent
	default:
		return RowToneNormal
	}
}

func statusHasWord(status string, word string) bool {
	for _, field := range strings.FieldsFunc(status, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if field == word {
			return true
		}
	}
	return false
}

func renderRows(styles Styles, rows []row, selected int, width int) []string {
	normalized := make([]Row, 0, len(rows))
	for _, row := range rows {
		if row.Tone == RowToneNormal {
			row.Tone = RowToneForStatus(row.Status)
		}
		normalized = append(normalized, row)
	}
	return RenderRows(styles.RowStyles(width), normalized, selected, width)
}

func RenderRows(styles RowStyles, rows []Row, selected int, width int) []string {
	out := make([]string, 0, len(rows))
	for i, row := range rows {
		out = append(out, RenderRow(styles, row, i == selected, width))
	}
	return out
}

func RenderRow(styles RowStyles, row Row, selected bool, width int) string {
	rowStyle := styles.Row
	if selected {
		rowStyle = styles.RowSelected
	} else if row.Disabled {
		rowStyle = styles.RowDisabled
	}
	contentWidth := max(0, width-rowStyle.GetHorizontalFrameSize())
	rowStyle = rowStyle.Width(width)

	right := strings.TrimSpace(row.Status)
	right = ansi.Truncate(right, max(0, contentWidth/2), "…")

	statusStyle := styles.Status
	switch row.Tone {
	case RowToneAccent:
		statusStyle = styles.StatusAccent
	case RowToneWarning:
		statusStyle = styles.StatusWarning
	}
	if selected {
		statusStyle = styles.StatusSelected
	}

	infoText := ""
	infoWidth := 0
	if right != "" {
		infoText = " " + right + " "
		infoText = statusStyle.Render(infoText)
		infoWidth = lipgloss.Width(infoText)
	}
	if infoWidth > contentWidth {
		infoText = ""
		infoWidth = 0
	}

	left := ansi.Truncate(row.Title, max(0, contentWidth-infoWidth), "…")
	leftWidth := lipgloss.Width(left)
	gap := strings.Repeat(" ", max(0, contentWidth-leftWidth-infoWidth))
	return rowStyle.Render(left + gap + infoText)
}
