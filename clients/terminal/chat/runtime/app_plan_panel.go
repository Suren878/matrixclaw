package runtime

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/runtime/planview"
	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *appModel) planPanelVisible() bool {
	return m.visiblePlanPanelWidth() > 0
}

func (m *appModel) shouldShowPlanPanel() bool {
	if m.focus == appFocusPlan || m.planPanelOpen {
		return true
	}
	return m.busy && planHasOpenWork(m.currentSnapshot().Plan)
}

func (m *appModel) openPlanPanel() tea.Cmd {
	if !m.currentSessionCapabilities().PlanningMode {
		m.dialog.CloseAll()
		m.dialog.OpenDialog(surfacedialog.NewInfo(m.com, surfacedialog.InfoData{
			Title: "Planning Mode",
			Text:  "Planning Mode is available for Matrixclaw sessions only.",
		}))
		return nil
	}
	if m.planPanelOpen || m.focus == appFocusPlan {
		m.planPanelOpen = false
		return m.setFocus(appFocusEditor)
	}
	m.planPanelOpen = true
	if m.availablePlanPanelWidth() <= 0 {
		return m.controlplaneCmd("/plan")
	}
	return m.setFocus(appFocusPlan)
}

func (m *appModel) planPanelView(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	innerWidth := max(1, width-3)
	snapshot := m.currentSnapshot()
	plan := snapshot.Plan
	m.clampPlanSelection(plan)

	lines := []string{
		"",
		m.planPanelTitle(innerWidth),
	}
	footer := m.planPanelFooter()

	if plan == nil || (strings.TrimSpace(plan.Goal) == "" && len(plan.Items) == 0) {
		lines = append(lines,
			"",
			m.styles.Muted.Render("No goal yet"),
			"",
			m.planActionDivider(innerWidth),
			m.planActionControlsLine(plan, innerWidth),
		)
		return m.placePlanPanelWithFooter(width, height, lines, footer)
	}

	if goal := strings.TrimSpace(plan.Goal); goal != "" {
		lines = append(lines, "", m.styles.Muted.Render("Goal"))
		lines = append(lines, planview.WrapLine(goal, innerWidth)...)
	}

	if len(plan.Items) > 0 {
		done := planview.CompletedCount(plan.Items)
		lines = append(lines, "", m.styles.Muted.Render(fmt.Sprintf("Items %d/%d", done, len(plan.Items))))
		guides := planview.TreeGuides(plan.Items)
		for i, item := range plan.Items {
			lines = append(lines, m.planItemLines(i, item, innerWidth, guides[item.ID])...)
		}
	}

	lines = append(lines, "", m.planActionDivider(innerWidth), m.planActionControlsLine(plan, innerWidth))

	return m.placePlanPanelWithFooter(width, height, lines, footer)
}

func (m *appModel) planPanelTitle(width int) string {
	title := "Planning Mode"
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorToHex(m.styles.White))).
		Bold(true).
		Width(max(1, width)).
		Align(lipgloss.Center)
	return style.Render(title)
}

func (m *appModel) planCursor() string {
	if m.focus != appFocusPlan {
		return "  "
	}
	if m.now.IsZero() || (m.now.UnixMilli()/480)%2 == 0 {
		return m.styles.Files.Path.Render(">")
	}
	return " "
}

func (m *appModel) planPanelFooter() []string {
	if m.focus == appFocusPlan {
		return []string{m.styles.HalfMuted.Render("↑↓ move · ←→ buttons · enter actions · d done")}
	}
	return []string{m.styles.HalfMuted.Render("ctrl+n plan")}
}

func (m *appModel) planActionDivider(width int) string {
	return m.styles.PanelMuted.Render(strings.Repeat("─", max(8, width)))
}

func (m *appModel) planActionControlsLine(plan *core.SessionPlan, width int) string {
	styles := commandui.DefaultStyles()
	actionFocused := m.focus == appFocusPlan && m.planSelected == planActionRowIndex(plan)
	buttons := []commandui.Button{
		{
			Label:   "+ New Task",
			Focused: actionFocused && m.planActionSelected == 0,
		},
	}
	if planHasOpenWork(plan) {
		buttons = append(buttons,
			commandui.Button{
				Label:   planActionLabel(plan, m.busy),
				Focused: actionFocused && m.planActionSelected == 1,
			},
			commandui.Button{
				Label:   "✕ Cancel Plan",
				Focused: actionFocused && m.planActionSelected == 2,
			},
		)
	}
	return commandui.RenderButtons(styles, width, buttons...)
}

func (m *appModel) planItemLines(index int, item core.PlanItem, width int, guide string) []string {
	prefix := "  "
	if m.focus == appFocusPlan && index == m.planSelected {
		prefix = m.planCursor() + " "
	}
	marker := planview.Marker(item.Status)
	markerStyle := m.planItemMarkerStyle(item.Status)
	text := strings.TrimSpace(item.Text)
	if text == "" {
		text = "Untitled"
	}
	head := fmt.Sprintf("%s%s%s ", prefix, guide, markerStyle.Render(marker))
	plainHead := fmt.Sprintf("%s%s%s ", prefix, guide, marker)
	firstWidth := max(1, width-ansi.StringWidth(plainHead))
	wrapped := planview.WrapLine(text, firstWidth)
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	lines := make([]string, 0, len(wrapped))
	lines = append(lines, head+wrapped[0])
	if len(wrapped) == 1 {
		return lines
	}
	continuation := planview.ContinuationPrefix(prefix, guide, marker)
	continuationWidth := max(1, width-ansi.StringWidth(continuation))
	for _, line := range wrapped[1:] {
		for _, part := range planview.WrapLine(line, continuationWidth) {
			lines = append(lines, continuation+part)
		}
	}
	return lines
}

func (m *appModel) planItemMarkerStyle(status core.PlanItemStatus) lipgloss.Style {
	switch status {
	case core.PlanItemActive:
		return m.styles.Files.Path
	case core.PlanItemDone:
		return m.styles.Files.Additions
	case core.PlanItemSkipped:
		return m.styles.HalfMuted
	default:
		return m.styles.Muted
	}
}

func (m *appModel) placePlanPanel(width int, height int, lines []string) string {
	return m.placePlanPanelWithFooter(width, height, lines, nil)
}

func (m *appModel) placePlanPanelWithFooter(width int, height int, lines []string, footer []string) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	contentWidth := max(1, width-2)
	if len(footer) > 0 && len(lines)+len(footer) < height {
		padding := height - len(lines) - len(footer)
		for range padding {
			lines = append(lines, "")
		}
		lines = append(lines, footer...)
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

func (m *appModel) clampPlanSelection(plan *core.SessionPlan) {
	count := planSelectionCount(plan)
	if count == 0 {
		m.planSelected = 0
		m.planActionSelected = 0
		return
	}
	if m.planSelected < 0 {
		m.planSelected = 0
	}
	if m.planSelected >= count {
		m.planSelected = count - 1
	}
	actionCount := planActionCount(plan)
	if m.planActionSelected < 0 {
		m.planActionSelected = 0
	}
	if m.planActionSelected >= actionCount {
		m.planActionSelected = actionCount - 1
	}
}

func planSelectionCount(plan *core.SessionPlan) int {
	if plan == nil {
		return 1
	}
	return len(plan.Items) + 1
}

func planActionRowIndex(plan *core.SessionPlan) int {
	if plan == nil {
		return 0
	}
	return len(plan.Items)
}

func planActionCount(plan *core.SessionPlan) int {
	if planHasOpenWork(plan) {
		return 3
	}
	return 1
}

func planActionLabel(plan *core.SessionPlan, busy bool) string {
	if busy {
		return "■ Stop"
	}
	if planHasActiveItem(plan) {
		return "▶ Continue"
	}
	return "▶ Run"
}

func planHasActiveItem(plan *core.SessionPlan) bool {
	if plan == nil {
		return false
	}
	for _, item := range plan.Items {
		if item.Status == core.PlanItemActive {
			return true
		}
	}
	return false
}

func planHasOpenWork(plan *core.SessionPlan) bool {
	if plan == nil {
		return false
	}
	for _, item := range plan.Items {
		switch item.Status {
		case core.PlanItemDone, core.PlanItemSkipped:
			continue
		default:
			return true
		}
	}
	return false
}

func (m *appModel) openPlanPrompt(title string, placeholder string, prefix string) tea.Cmd {
	return m.openPlanPromptValue(title, placeholder, prefix, "")
}

func (m *appModel) openPlanPromptValue(title string, placeholder string, prefix string, value string) tea.Cmd {
	m.dialog.OpenDialog(surfacedialog.NewPromptCommand(m.com, controlplane.PromptData{
		Title:               title,
		Placeholder:         placeholder,
		Value:               value,
		SubmitCommandPrefix: prefix,
		CancelCommand:       "",
	}))
	return nil
}
