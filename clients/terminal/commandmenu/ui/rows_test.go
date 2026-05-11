package commandui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderRowUsesSelectedAndStatusTone(t *testing.T) {
	styles := RowStyles{
		Row:            lipgloss.NewStyle(),
		RowSelected:    lipgloss.NewStyle().Bold(true),
		RowDisabled:    lipgloss.NewStyle().Faint(true),
		Status:         lipgloss.NewStyle(),
		StatusAccent:   lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		StatusWarning:  lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		StatusSelected: lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Bold(true),
	}

	rendered := RenderRow(styles, Row{
		Title:  "OpenAI",
		Status: "Configured",
		Tone:   RowToneAccent,
	}, false, 30)
	if !strings.Contains(rendered, "OpenAI") || !strings.Contains(rendered, "Configured") {
		t.Fatalf("rendered row = %q", rendered)
	}

	selected := RenderRow(styles, Row{
		Title:  "Anthropic",
		Status: "Active",
		Tone:   RowToneWarning,
	}, true, 30)
	if !strings.Contains(selected, "Anthropic") || !strings.Contains(selected, "Active") {
		t.Fatalf("selected row = %q", selected)
	}
}

func TestRenderRowWidthIncludesStylePadding(t *testing.T) {
	styles := RowStyles{
		Row:            lipgloss.NewStyle().Padding(0, 1),
		RowSelected:    lipgloss.NewStyle().Padding(0, 1).Bold(true),
		RowDisabled:    lipgloss.NewStyle().Padding(0, 1),
		Status:         lipgloss.NewStyle(),
		StatusAccent:   lipgloss.NewStyle(),
		StatusWarning:  lipgloss.NewStyle(),
		StatusSelected: lipgloss.NewStyle(),
	}

	rendered := RenderRow(styles, Row{
		Title:  "Name",
		Status: "matrixclaw Assistant Profile",
	}, false, 24)
	for _, line := range strings.Split(rendered, "\n") {
		if got := ansi.StringWidth(line); got > 24 {
			t.Fatalf("line width = %d, want <= 24: %q", got, line)
		}
	}
	if strings.Contains(rendered, "\n") {
		t.Fatalf("row wrapped unexpectedly: %q", rendered)
	}
}

func TestRowToneForStatus(t *testing.T) {
	cases := map[string]RowTone{
		"Configured": RowToneAccent,
		"enabled":    RowToneAccent,
		"Disabled":   RowToneWarning,
		"Active":     RowToneNormal,
	}
	for status, want := range cases {
		if got := RowToneForStatus(status); got != want {
			t.Fatalf("RowToneForStatus(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestRenderRowsSelectsExpectedRow(t *testing.T) {
	styles := NewRowStyles(
		lipgloss.NewStyle(),
		lipgloss.NewStyle().Bold(true),
		lipgloss.NewStyle(),
		lipgloss.NewStyle(),
		lipgloss.NewStyle(),
		lipgloss.NewStyle(),
		lipgloss.NewStyle(),
	)
	rendered := RenderRows(styles, []Row{{Title: "One"}, {Title: "Two"}}, 1, 20)
	if len(rendered) != 2 {
		t.Fatalf("len(rendered) = %d, want 2", len(rendered))
	}
	if !strings.Contains(rendered[1], "Two") {
		t.Fatalf("selected row = %q, want Two", rendered[1])
	}
}
