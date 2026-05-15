package controlplane

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (d *Dispatcher) handlePlan(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.plan == nil {
		return unsupportedRuntime("plan"), nil
	}
	if d.sessions == nil {
		return unsupportedRuntime("sessions"), nil
	}
	_, session, err := d.currentSession(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}
	if !core.CapabilitiesForSession(*session).PlanningMode {
		return Result{Handled: true, Text: "Planning Mode is available for Matrixclaw sessions only."}, nil
	}

	args = strings.TrimSpace(args)
	lower := strings.ToLower(args)
	switch {
	case lower == "":
		plan, err := d.plan.SessionPlan(ctx, session.ID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Info: planInfoData(plan).Ptr()}, nil
	case strings.HasPrefix(lower, "goal "):
		plan, err := d.plan.SetSessionGoal(ctx, session.ID, strings.TrimSpace(args[len("goal "):]))
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Info: planInfoData(plan).Ptr(), ReloadSnapshot: true}, nil
	case strings.HasPrefix(lower, "add "):
		plan, err := d.plan.AddPlanItem(ctx, session.ID, strings.TrimSpace(args[len("add "):]), "")
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Info: planInfoData(plan).Ptr(), ReloadSnapshot: true}, nil
	case strings.HasPrefix(lower, "subtask "):
		plan, err := d.addPlanSubtaskByOrdinal(ctx, session.ID, args[len("subtask "):])
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Info: planInfoData(plan).Ptr(), ReloadSnapshot: true}, nil
	case strings.HasPrefix(lower, "edit "):
		return d.editPlanItemByOrdinal(ctx, session.ID, args[len("edit "):])
	case strings.HasPrefix(lower, "done "):
		return d.updatePlanItemByOrdinal(ctx, session.ID, args[len("done "):], core.PlanItemDone)
	case strings.HasPrefix(lower, "active "):
		return d.updatePlanItemByOrdinal(ctx, session.ID, args[len("active "):], core.PlanItemActive)
	case strings.HasPrefix(lower, "skip "):
		return d.updatePlanItemByOrdinal(ctx, session.ID, args[len("skip "):], core.PlanItemSkipped)
	case lower == "clear":
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        "Clear Planning Mode?",
			ConfirmLabel:   "Clear",
			CancelLabel:    "Cancel",
			ConfirmCommand: "/plan clear confirm",
			CancelCommand:  "/plan",
		}}, nil
	case lower == "clear confirm":
		plan, err := d.plan.ClearSessionPlan(ctx, session.ID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Info: planInfoData(plan).Ptr(), ReloadSnapshot: true}, nil
	default:
		return Result{Handled: true, Text: "Usage: /plan, /plan goal <text>, /plan add <text>, /plan subtask <number> <text>, /plan edit <number> <text>, /plan done <number>, /plan active <number>, /plan skip <number>, /plan clear"}, nil
	}
}

func (d *Dispatcher) addPlanSubtaskByOrdinal(ctx context.Context, sessionID string, value string) (core.SessionPlan, error) {
	plan, err := d.plan.SessionPlan(ctx, sessionID)
	if err != nil {
		return core.SessionPlan{}, err
	}
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) < 2 {
		return core.SessionPlan{}, fmt.Errorf("%w: usage /plan subtask <number> <text>", core.ErrInvalidInput)
	}
	index, err := strconv.Atoi(fields[0])
	if err != nil || index <= 0 || index > len(plan.Items) {
		return core.SessionPlan{}, fmt.Errorf("%w: plan item number is invalid", core.ErrInvalidInput)
	}
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), fields[0]))
	return d.plan.AddPlanItem(ctx, sessionID, text, plan.Items[index-1].ID)
}

func (d *Dispatcher) editPlanItemByOrdinal(ctx context.Context, sessionID string, value string) (Result, error) {
	plan, err := d.plan.SessionPlan(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) < 2 {
		return Result{}, fmt.Errorf("%w: usage /plan edit <number> <text>", core.ErrInvalidInput)
	}
	index, err := strconv.Atoi(fields[0])
	if err != nil || index <= 0 || index > len(plan.Items) {
		return Result{Handled: true, Text: "Plan item number is invalid."}, nil
	}
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), fields[0]))
	plan, err = d.plan.UpdatePlanItem(ctx, sessionID, plan.Items[index-1].ID, "", text)
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Info: planInfoData(plan).Ptr(), ReloadSnapshot: true}, nil
}

func (d *Dispatcher) updatePlanItemByOrdinal(ctx context.Context, sessionID string, value string, status core.PlanItemStatus) (Result, error) {
	plan, err := d.plan.SessionPlan(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	index, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || index <= 0 || index > len(plan.Items) {
		return Result{Handled: true, Text: "Plan item number is invalid."}, nil
	}
	plan, err = d.plan.UpdatePlanItem(ctx, sessionID, plan.Items[index-1].ID, status, "")
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Info: planInfoData(plan).Ptr(), ReloadSnapshot: true}, nil
}

func planInfoData(plan core.SessionPlan) InfoData {
	rows := []InfoRow{
		{Label: "Goal", Value: emptyFallback(plan.Goal, "not set")},
		{Label: "Items", Value: fmt.Sprintf("%d", len(plan.Items))},
	}
	depths := core.PlanItemDepths(plan.Items)
	for i, item := range plan.Items {
		indent := strings.Repeat("  ", min(depths[item.ID], 4))
		rows = append(rows, InfoRow{
			Label: fmt.Sprintf("%s%d. %s", indent, i+1, string(item.Status)),
			Value: item.Text,
		})
	}
	return InfoData{
		Title: "Planning Mode",
		Text:  planInfoText(plan),
		Rows:  rows,
	}
}

func planInfoText(plan core.SessionPlan) string {
	lines := []string{"Goal: " + emptyFallback(plan.Goal, "not set")}
	depths := core.PlanItemDepths(plan.Items)
	for i, item := range plan.Items {
		indent := strings.Repeat("  ", min(depths[item.ID], 4))
		lines = append(lines, fmt.Sprintf("%s%d. [%s] %s", indent, i+1, item.Status, item.Text))
	}
	return strings.Join(lines, "\n")
}

func emptyFallback(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func (data InfoData) Ptr() *InfoData {
	return &data
}
