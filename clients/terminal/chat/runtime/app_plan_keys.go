package runtime

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *appModel) handlePlanKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	plan := m.currentSnapshot().Plan
	m.clampPlanSelection(plan)
	switch msg.String() {
	case "tab", "esc":
		return m, m.setFocus(appFocusEditor)
	case "up", "k":
		if m.planSelected > 0 {
			m.planSelected--
		}
		return m, nil
	case "down", "j":
		if m.planSelected < planSelectionCount(plan)-1 {
			m.planSelected++
		}
		return m, nil
	case "left", "h":
		if m.planSelected == planActionRowIndex(plan) && m.planActionSelected > 0 {
			m.planActionSelected--
		}
		return m, nil
	case "right", "l":
		if m.planSelected == planActionRowIndex(plan) && m.planActionSelected < planActionCount(plan)-1 {
			m.planActionSelected++
		}
		return m, nil
	case "g":
		return m, m.openPlanPrompt("Set Plan Goal", "Goal", "/plan goal ")
	case "a":
		return m, m.openPlanPrompt("New Task", "Task", "/plan add ")
	case "A":
		if planSelectedItemCanHaveSubtask(plan, m.planSelected) {
			return m, m.openPlanPrompt("Add Subtask", "Subtask", fmt.Sprintf("/plan subtask %d ", m.planSelected+1))
		}
		return m, m.openPlanPrompt("New Task", "Task", "/plan add ")
	case "c":
		return m, m.openCancelPlanDialog()
	case "enter":
		if m.planSelected == planActionRowIndex(plan) {
			return m, m.handlePlanActionButton(plan)
		}
		if _, ok := planSelectedItem(plan, m.planSelected); ok {
			return m, m.openPlanTaskMenu(plan, m.planSelected)
		}
		return m, nil
	case "d":
		if planSelectedItemClosed(plan, m.planSelected) {
			return m, nil
		}
		return m, m.planStatusCmd(plan, core.PlanItemDone)
	case "r":
		return m, m.startPlanRunCmd()
	case " ":
		if planSelectedItemClosed(plan, m.planSelected) {
			return m, nil
		}
		return m, m.planStatusCmd(plan, core.PlanItemActive)
	case "s":
		if planSelectedItemClosed(plan, m.planSelected) {
			return m, nil
		}
		return m, m.planStatusCmd(plan, core.PlanItemSkipped)
	default:
		return m, nil
	}
}

func (m *appModel) handlePlanActionButton(plan *core.SessionPlan) tea.Cmd {
	m.clampPlanSelection(plan)
	switch m.planActionSelected {
	case 0:
		return m.openPlanPrompt("New Task", "Task", "/plan add ")
	case 1:
		if planHasOpenWork(plan) {
			if m.busy {
				return m.openCancelRunDialog()
			}
			return m.startPlanRunCmd()
		}
	case 2:
		if planHasOpenWork(plan) {
			return m.openCancelPlanDialog()
		}
	}
	return nil
}

func (m *appModel) openCancelPlanDialog() tea.Cmd {
	m.dialog.OpenDialog(surfacedialog.NewConfirmCommand(m.com, surfacedialog.ConfirmCommandData{
		Message:        "Cancel current plan?",
		ConfirmLabel:   "Cancel plan",
		CancelLabel:    "Keep",
		ConfirmDanger:  true,
		ConfirmCommand: "/plan cancel",
		CancelCommand:  "",
	}))
	return nil
}

func (m *appModel) handlePlanPromptCommand(command string) (tea.Cmd, bool) {
	switch {
	case strings.HasPrefix(command, "/plan prompt edit "):
		index, ok := parsePlanPromptIndex(strings.TrimPrefix(command, "/plan prompt edit "))
		if !ok {
			return nil, true
		}
		item, ok := planSelectedItem(m.currentSnapshot().Plan, index-1)
		if !ok {
			return nil, true
		}
		return m.openPlanPromptValue("Edit Task", "Task", fmt.Sprintf("/plan edit %d ", index), item.Text), true
	case strings.HasPrefix(command, "/plan prompt subtask "):
		index, ok := parsePlanPromptIndex(strings.TrimPrefix(command, "/plan prompt subtask "))
		if !ok {
			return nil, true
		}
		if !planSelectedItemCanHaveSubtask(m.currentSnapshot().Plan, index-1) {
			return nil, true
		}
		return m.openPlanPrompt("New Subtask", "Subtask", fmt.Sprintf("/plan subtask %d ", index)), true
	default:
		return nil, false
	}
}

func parsePlanPromptIndex(value string) (int, bool) {
	index, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || index <= 0 {
		return 0, false
	}
	return index, true
}

func (m *appModel) openPlanTaskMenu(plan *core.SessionPlan, selected int) tea.Cmd {
	item, ok := planSelectedItem(plan, selected)
	if !ok {
		return nil
	}
	index := selected + 1
	title := strings.TrimSpace(item.Text)
	if title == "" {
		title = "Plan Task"
	}
	entries := []surfacedialog.CommandEntry{
		{
			ID:     "edit",
			Title:  "✎ Edit Task",
			Status: strconv.Itoa(index),
			Action: surfacedialog.ActionRunControlplaneCommand{Command: fmt.Sprintf("/plan prompt edit %d", index)},
		},
	}
	if planSelectedItemCanHaveSubtask(plan, selected) {
		entries = append(entries, surfacedialog.CommandEntry{
			ID:     "subtask",
			Title:  "+ New Subtask",
			Status: strconv.Itoa(index),
			Action: surfacedialog.ActionRunControlplaneCommand{Command: fmt.Sprintf("/plan prompt subtask %d", index)},
		})
	}
	if !planSelectedItemClosed(plan, selected) {
		entries = append(entries,
			surfacedialog.CommandEntry{
				ID:     "active",
				Title:  "• Mark Active",
				Status: string(core.PlanItemActive),
				Action: surfacedialog.ActionRunControlplaneCommand{Command: fmt.Sprintf("/plan active %d", index)},
			},
			surfacedialog.CommandEntry{
				ID:     "done",
				Title:  "✓ Mark Done",
				Status: string(core.PlanItemDone),
				Action: surfacedialog.ActionRunControlplaneCommand{Command: fmt.Sprintf("/plan done %d", index)},
			},
			surfacedialog.CommandEntry{
				ID:     "skip",
				Title:  "x Skip",
				Status: string(core.PlanItemSkipped),
				Action: surfacedialog.ActionRunControlplaneCommand{Command: fmt.Sprintf("/plan skip %d", index)},
			},
		)
	}
	m.dialog.OpenDialog(surfacedialog.NewCommands(m.com, surfacedialog.CommandsData{
		Title:   title,
		Legend:  "enter select · esc back",
		Entries: entries,
	}))
	return nil
}

func planSelectedItem(plan *core.SessionPlan, selected int) (core.PlanItem, bool) {
	if plan == nil || selected < 0 || selected >= len(plan.Items) {
		return core.PlanItem{}, false
	}
	return plan.Items[selected], true
}

func planSelectedItemClosed(plan *core.SessionPlan, selected int) bool {
	item, ok := planSelectedItem(plan, selected)
	if !ok {
		return false
	}
	switch item.Status {
	case core.PlanItemDone, core.PlanItemSkipped:
		return true
	default:
		return false
	}
}

func planSelectedItemCanHaveSubtask(plan *core.SessionPlan, selected int) bool {
	item, ok := planSelectedItem(plan, selected)
	if !ok {
		return false
	}
	if strings.TrimSpace(item.ParentID) != "" {
		return false
	}
	return !planSelectedItemClosed(plan, selected)
}

func (m *appModel) planStatusCmd(plan *core.SessionPlan, status core.PlanItemStatus) tea.Cmd {
	if plan == nil || len(plan.Items) == 0 {
		return nil
	}
	if m.planSelected >= len(plan.Items) {
		return nil
	}
	m.clampPlanSelection(plan)
	index := m.planSelected + 1
	command := fmt.Sprintf("/plan %s %d", planStatusCommand(status), index)
	return m.controlplaneCmd(command)
}

func planStatusCommand(status core.PlanItemStatus) string {
	switch status {
	case core.PlanItemActive:
		return "active"
	case core.PlanItemDone:
		return "done"
	case core.PlanItemSkipped:
		return "skip"
	default:
		return "active"
	}
}
