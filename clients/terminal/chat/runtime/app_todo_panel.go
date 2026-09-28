package runtime

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
)

// todoPanelChoice is what the user chose for the todo panel with ctrl+n.
type todoPanelChoice int

const (
	// todoPanelAuto shows the panel while the todo list has open items.
	todoPanelAuto todoPanelChoice = iota
	todoPanelShown
	todoPanelHidden
)

func (m *appModel) shouldShowTodoPanel() bool {
	switch m.todoPanel {
	case todoPanelShown:
		return true
	case todoPanelHidden:
		return false
	}
	list := m.currentSnapshot().Todo
	return list != nil && len(todo.Open(list.Items)) > 0
}

// toggleTodoPanel shows or hides the todo panel; a terminal too small for it
// shows the list in a dialog instead.
func (m *appModel) toggleTodoPanel() tea.Cmd {
	if m.availableTodoPanelWidth() <= 0 {
		return m.controlplaneCmd("/todo")
	}
	if m.shouldShowTodoPanel() {
		m.todoPanel = todoPanelHidden
	} else {
		m.todoPanel = todoPanelShown
	}
	m.resizeChat()
	return nil
}

func (m *appModel) todoPanelView(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	innerWidth := max(1, width-3)
	lines := []string{"", m.todoPanelTitle(innerWidth), ""}
	list := m.currentSnapshot().Todo
	if list == nil || len(list.Items) == 0 {
		lines = append(lines, m.styles.Muted.Render("No todo list yet"))
	} else {
		done := len(list.Items) - len(todo.Open(list.Items))
		lines = append(lines, m.styles.Muted.Render(fmt.Sprintf("%d/%d done", done, len(list.Items))))
		var items []string
		focus := -1
		for _, item := range list.Items {
			if focus < 0 && item.Status != todo.Completed {
				focus = len(items)
			}
			if item.Status == todo.InProgress {
				focus = len(items)
			}
			items = append(items, m.todoItemLines(item, innerWidth)...)
		}
		lines = append(lines, todoWindow(items, focus, height-len(lines))...)
	}
	return m.placeTodoPanel(width, height, lines, m.styles.HalfMuted.Render("ctrl+n todo"))
}

// todoWindow is the part of the item lines that fits in height, scrolled so the
// line at focus (the item in progress, else the first open one) stays in view.
func todoWindow(lines []string, focus int, height int) []string {
	if len(lines) <= height || focus < 0 {
		return lines
	}
	height = max(1, height)
	start := min(max(0, focus-1), len(lines)-height)
	return lines[start : start+height]
}

func (m *appModel) todoPanelTitle(width int) string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorToHex(m.styles.White))).
		Bold(true).
		Width(max(1, width)).
		Align(lipgloss.Center)
	return style.Render("Todo")
}

func (m *appModel) todoItemLines(item todo.Item, width int) []string {
	marker, markerStyle, textStyle := "[ ]", m.styles.Muted, lipgloss.NewStyle()
	switch item.Status {
	case todo.InProgress:
		marker, markerStyle = "[•]", m.styles.Files.Path
	case todo.Completed:
		marker, markerStyle, textStyle = "[✓]", m.styles.Files.Additions, m.styles.HalfMuted
	}
	indent := strings.Repeat(" ", ansi.StringWidth(marker)+1)
	wrapped := wrapTodoText(item.Label(), max(1, width-len(indent)))
	lines := make([]string, 0, len(wrapped))
	for i, line := range wrapped {
		prefix := indent
		if i == 0 {
			prefix = markerStyle.Render(marker) + " "
		}
		lines = append(lines, prefix+textStyle.Render(line))
	}
	return lines
}

func wrapTodoText(text string, width int) []string {
	wrapped := strings.TrimSpace(ansi.Wrap(text, max(1, width), " \t/\\._:=,;|-"))
	if wrapped == "" {
		return []string{""}
	}
	return strings.Split(wrapped, "\n")
}

func (m *appModel) placeTodoPanel(width int, height int, lines []string, footer string) string {
	contentWidth := max(1, width-2)
	if len(lines)+1 < height {
		for len(lines) < height-1 {
			lines = append(lines, "")
		}
		lines = append(lines, footer)
	}
	rendered := make([]string, 0, height)
	separator := m.styles.PanelMuted.Render("│")
	for i := 0; i < height; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		line = ansi.Truncate(line, contentWidth, "…")
		rendered = append(rendered, separator+" "+ansi.Truncate(line, contentWidth, " "))
	}
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, strings.Join(rendered, "\n"))
}
