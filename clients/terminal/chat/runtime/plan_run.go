package runtime

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/runtime/planview"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/core"
)

const planSummaryMessageID = "local:session-plan-summary"

func (m *appModel) startPlanRunCmd() tea.Cmd {
	snapshot := m.currentSnapshot()
	if !planHasOpenWork(snapshot.Plan) {
		m.planAutoRun = false
		return nil
	}
	if m.busy {
		m.err = "agent is working, please wait"
		return nil
	}
	if strings.TrimSpace(m.session) == "" {
		m.err = "no active session"
		return nil
	}
	if m.rt == nil {
		m.err = "terminal runtime is not configured"
		return nil
	}
	m.err = ""
	m.planAutoRun = true
	m.planPanelOpen = true
	m.setBusy(true)
	sessionID := strings.TrimSpace(m.session)
	return func() tea.Msg {
		planRun, plan, err := m.rt.StartSessionPlanRun(m.ctx, sessionID, false)
		if err != nil {
			return sendMessageResultMsg{planRun: true, err: err}
		}
		if planRun.Status == core.PlanRunCompleted {
			snapshot, err := m.rt.loadOrInitSnapshot(m.ctx)
			return loadInitialMsg{snapshot: snapshot, err: err}
		}
		item, ok := planItemByID(plan, planRun.CurrentItemID)
		if !ok {
			return sendMessageResultMsg{planRun: true, err: fmt.Errorf("plan runner selected missing item %q", planRun.CurrentItemID)}
		}
		prompt := renderPlanRunPrompt(&plan, item)
		result, err := m.rt.sendMessage(m.ctx, sessionID, prompt)
		if err == nil {
			err = m.rt.BindSessionPlanRunStep(m.ctx, sessionID, result.Run.ID)
		}
		return sendMessageResultMsg{
			content: prompt,
			result:  result,
			planRun: true,
			err:     err,
		}
	}
}

func planItemByID(plan core.SessionPlan, itemID string) (core.PlanItem, bool) {
	itemID = strings.TrimSpace(itemID)
	for _, item := range plan.Items {
		if strings.TrimSpace(item.ID) == itemID {
			return item, true
		}
	}
	return core.PlanItem{}, false
}

func nextPlanExecutableItem(plan *core.SessionPlan) (core.PlanItem, bool) {
	if plan == nil {
		return core.PlanItem{}, false
	}
	return core.NextExecutablePlanItem(*plan)
}

func renderPlanRunPrompt(plan *core.SessionPlan, item core.PlanItem) string {
	return strings.TrimSpace(fmt.Sprintf(`Execute the next session plan item.

Plan item id: %s
Plan item text: %s

Work only on this item. If it has parent context, use it only for orientation. When the item is complete, give a concise result. The runtime will update the plan state after a successful run. If the item is blocked, say exactly why and do not claim the plan is complete.

Current plan:
%s`, item.ID, strings.TrimSpace(item.Text), renderPlanForPrompt(plan)))
}

func renderPlanForPrompt(plan *core.SessionPlan) string {
	if plan == nil {
		return ""
	}
	var b strings.Builder
	if goal := strings.TrimSpace(plan.Goal); goal != "" {
		fmt.Fprintf(&b, "Goal: %s\n", goal)
	}
	childCounts := planChildCounts(plan.Items)
	for i, item := range plan.Items {
		note := ""
		if childCounts[item.ID] > 0 {
			note = " (parent section; execute subtasks)"
		}
		fmt.Fprintf(&b, "%d. [%s] %s%s\n", i+1, item.Status, strings.TrimSpace(item.Text), note)
	}
	return strings.TrimSpace(b.String())
}

func planChildCounts(items []core.PlanItem) map[string]int {
	counts := make(map[string]int, len(items))
	for _, item := range items {
		parentID := strings.TrimSpace(item.ParentID)
		if parentID != "" {
			counts[parentID]++
		}
	}
	return counts
}

func (m *appModel) showPlanFinishedSummary(run core.Run) {
	plan := m.currentSnapshot().Plan
	if plan == nil || strings.TrimSpace(plan.Goal) == "" && len(plan.Items) == 0 {
		return
	}
	if run.Status == core.RunStatusCompleted && planHasOpenWork(plan) {
		return
	}
	title := "✅ Plan Finished"
	if run.Status != core.RunStatusCompleted {
		title = "Plan Stopped"
	}
	text := renderPlanSummaryText(title, plan)
	m.upsertTransientMessage(newPlanSummaryTransientMessage(text, run.Status))
}

func renderPlanSummaryText(title string, plan *core.SessionPlan) string {
	summary := strings.TrimSpace(renderPlanSummary(plan))
	if summary == "" {
		return strings.TrimSpace(title)
	}
	return strings.TrimSpace(title) + "\n" + summary
}

func renderPlanSummary(plan *core.SessionPlan) string {
	if plan == nil {
		return ""
	}
	lines := make([]string, 0, len(plan.Items)+1)
	if goal := strings.TrimSpace(plan.Goal); goal != "" {
		lines = append(lines, "Goal: "+goal, "")
	}
	guides := planview.TreeGuides(plan.Items)
	for _, item := range plan.Items {
		lines = append(lines, guides[item.ID]+planview.Marker(item.Status)+" "+strings.TrimSpace(item.Text))
	}
	return strings.Join(lines, "\n")
}

func newPlanSummaryTransientMessage(text string, status core.RunStatus) surfacemessage.Message {
	return newPlanSummaryTransientMessageAt(text, status, time.Now())
}

func newPlanSummaryTransientMessageAt(text string, status core.RunStatus, createdAt time.Time) surfacemessage.Message {
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	createdAt = createdAt.UTC()
	now := time.Now().Unix()
	created := createdAt.Unix()
	finish := surfacemessage.FinishReasonEndTurn
	if status != core.RunStatusCompleted {
		finish = surfacemessage.FinishReasonCanceled
	}
	return surfacemessage.Message{
		ID:               planSummaryMessageID,
		Role:             surfacemessage.System,
		Parts:            []surfacemessage.ContentPart{surfacemessage.TextContent{Text: text}, surfacemessage.Finish{Reason: finish, Time: now}},
		CreatedAt:        created,
		UpdatedAt:        now,
		IsSummaryMessage: true,
	}
}
