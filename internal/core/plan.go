package core

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func (c *Core) SessionPlan(ctx context.Context, sessionID string) (SessionPlan, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return SessionPlan{}, ErrSessionRequired
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return SessionPlan{}, err
	}
	return c.store.GetSessionPlan(ctx, sessionID)
}

func (c *Core) SetSessionGoal(ctx context.Context, sessionID string, goal string) (SessionPlan, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return SessionPlan{}, ErrSessionRequired
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return SessionPlan{}, err
	}
	if err := c.store.SetSessionGoal(ctx, sessionID, strings.TrimSpace(goal), c.now().UTC()); err != nil {
		return SessionPlan{}, err
	}
	return c.loadAndPublishSessionPlan(ctx, sessionID)
}

func (c *Core) ClearSessionPlan(ctx context.Context, sessionID string) (SessionPlan, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return SessionPlan{}, ErrSessionRequired
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return SessionPlan{}, err
	}
	if err := c.store.ClearSessionPlan(ctx, sessionID); err != nil {
		return SessionPlan{}, err
	}
	_ = c.store.ClearPlanRun(ctx, sessionID)
	return c.loadAndPublishSessionPlan(ctx, sessionID)
}

func (c *Core) AddPlanItem(ctx context.Context, sessionID string, text string, parentID string) (SessionPlan, error) {
	sessionID = normalizeText(sessionID)
	text = strings.TrimSpace(text)
	parentID = normalizeText(parentID)
	if sessionID == "" {
		return SessionPlan{}, ErrSessionRequired
	}
	if text == "" {
		return SessionPlan{}, fmt.Errorf("%w: plan item text is required", ErrInvalidInput)
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return SessionPlan{}, err
	}
	if parentID != "" {
		parent, err := c.store.GetPlanItem(ctx, parentID)
		if err != nil {
			return SessionPlan{}, err
		}
		if parent.SessionID != sessionID {
			return SessionPlan{}, ErrNotFound
		}
	}
	position, err := c.store.NextPlanItemPosition(ctx, sessionID, parentID)
	if err != nil {
		return SessionPlan{}, err
	}
	now := c.now().UTC()
	item := PlanItem{
		ID:        c.newID("plan"),
		SessionID: sessionID,
		ParentID:  parentID,
		Text:      text,
		Status:    PlanItemPending,
		Position:  position,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := c.store.AddPlanItem(ctx, item); err != nil {
		return SessionPlan{}, err
	}
	return c.loadAndPublishSessionPlan(ctx, sessionID)
}

func (c *Core) UpdatePlanItem(ctx context.Context, sessionID string, itemID string, status PlanItemStatus, text string) (SessionPlan, error) {
	sessionID = normalizeText(sessionID)
	itemID = normalizeText(itemID)
	if sessionID == "" {
		return SessionPlan{}, ErrSessionRequired
	}
	if itemID == "" {
		return SessionPlan{}, fmt.Errorf("%w: plan item id is required", ErrInvalidInput)
	}
	item, err := c.store.GetPlanItem(ctx, itemID)
	if err != nil {
		return SessionPlan{}, err
	}
	if item.SessionID != sessionID {
		return SessionPlan{}, ErrNotFound
	}
	if normalized := normalizePlanItemStatus(status); normalized != "" {
		item.Status = normalized
	}
	if trimmed := strings.TrimSpace(text); trimmed != "" {
		item.Text = trimmed
	}
	item.UpdatedAt = c.now().UTC()
	if err := c.store.UpdatePlanItem(ctx, item); err != nil {
		return SessionPlan{}, err
	}
	return c.loadAndPublishSessionPlan(ctx, item.SessionID)
}

func (c *Core) loadAndPublishSessionPlan(ctx context.Context, sessionID string) (SessionPlan, error) {
	plan, err := c.store.GetSessionPlan(ctx, sessionID)
	if err != nil {
		return SessionPlan{}, err
	}
	c.publishEvent(Event{
		Type:      EventPlanUpdated,
		SessionID: plan.SessionID,
		Payload:   plan,
	})
	return plan, nil
}

func normalizePlanItemStatus(status PlanItemStatus) PlanItemStatus {
	switch PlanItemStatus(strings.ToLower(strings.TrimSpace(string(status)))) {
	case PlanItemPending:
		return PlanItemPending
	case PlanItemActive:
		return PlanItemActive
	case PlanItemDone:
		return PlanItemDone
	case PlanItemSkipped:
		return PlanItemSkipped
	default:
		return ""
	}
}

func PlanItemDepths(items []PlanItem) map[string]int {
	depths := make(map[string]int, len(items))
	parents := make(map[string]string, len(items))
	ids := make(map[string]struct{}, len(items))
	for _, item := range items {
		ids[item.ID] = struct{}{}
		parents[item.ID] = strings.TrimSpace(item.ParentID)
	}
	var depthOf func(itemID string, seen map[string]struct{}) int
	depthOf = func(itemID string, seen map[string]struct{}) int {
		if depth, ok := depths[itemID]; ok {
			return depth
		}
		parentID := parents[itemID]
		if parentID == "" {
			depths[itemID] = 0
			return 0
		}
		if _, ok := ids[parentID]; !ok {
			depths[itemID] = 0
			return 0
		}
		if _, cycle := seen[itemID]; cycle {
			depths[itemID] = 0
			return 0
		}
		seen[itemID] = struct{}{}
		depths[itemID] = depthOf(parentID, seen) + 1
		return depths[itemID]
	}
	for _, item := range items {
		depthOf(item.ID, map[string]struct{}{})
	}
	return depths
}

func (c *Core) SessionPlanRun(ctx context.Context, sessionID string) (PlanRun, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return PlanRun{}, ErrSessionRequired
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return PlanRun{}, err
	}
	return c.store.GetPlanRun(ctx, sessionID)
}

func (c *Core) StartSessionPlanRun(ctx context.Context, sessionID string, reset bool) (PlanRun, SessionPlan, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return PlanRun{}, SessionPlan{}, ErrSessionRequired
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return PlanRun{}, SessionPlan{}, err
	}
	plan, err := c.store.GetSessionPlan(ctx, sessionID)
	if err != nil {
		return PlanRun{}, SessionPlan{}, err
	}
	item, ok := NextExecutablePlanItem(plan)
	now := c.now().UTC()
	if !ok {
		run := PlanRun{SessionID: sessionID, Status: PlanRunCompleted, UpdatedAt: now, CreatedAt: now}
		if existing, err := c.store.GetPlanRun(ctx, sessionID); err == nil && !existing.CreatedAt.IsZero() {
			run.CreatedAt = existing.CreatedAt
			run.StepNo = existing.StepNo
		}
		if err := c.store.SavePlanRun(ctx, run); err != nil {
			return PlanRun{}, SessionPlan{}, err
		}
		return run, plan, nil
	}
	run, err := c.store.GetPlanRun(ctx, sessionID)
	if err != nil {
		return PlanRun{}, SessionPlan{}, err
	}
	if reset || run.CreatedAt.IsZero() {
		run = PlanRun{SessionID: sessionID, CreatedAt: now}
	}
	run.Status = PlanRunRunning
	run.CurrentItemID = item.ID
	run.LastError = ""
	run.StepNo++
	run.Attempt++
	run.UpdatedAt = now
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if item.Status == PlanItemPending {
		if _, err := c.UpdatePlanItem(ctx, sessionID, item.ID, PlanItemActive, ""); err != nil {
			return PlanRun{}, SessionPlan{}, err
		}
		plan, err = c.store.GetSessionPlan(ctx, sessionID)
		if err != nil {
			return PlanRun{}, SessionPlan{}, err
		}
	}
	if err := c.store.SavePlanRun(ctx, run); err != nil {
		return PlanRun{}, SessionPlan{}, err
	}
	return run, plan, nil
}

// CompleteSessionPlanRunStep advances the running plan after the run's final reply
// assistant, which may not be stored yet.
func (c *Core) CompleteSessionPlanRunStep(ctx context.Context, run Run, assistant transcript.Message) error {
	planRun, err := c.store.GetPlanRun(ctx, run.SessionID)
	if err != nil || planRun.Status != PlanRunRunning {
		return nil
	}
	if strings.TrimSpace(planRun.LastRunID) != strings.TrimSpace(run.ID) {
		if strings.TrimSpace(planRun.LastRunID) != "" {
			return nil
		}
		userMessage, err := c.findRunUserMessage(ctx, run)
		if err != nil || planRunItemID(userMessage.Content) != strings.TrimSpace(planRun.CurrentItemID) {
			return nil
		}
		planRun.LastRunID = run.ID
	}
	assistantContent, err := c.runReply(ctx, run, assistant)
	if err != nil {
		return err
	}
	now := c.now().UTC()
	if planRunLooksBlocked(assistantContent) {
		planRun.Status = PlanRunBlocked
		planRun.LastError = strings.TrimSpace(assistantContent)
		planRun.UpdatedAt = now
		return c.store.SavePlanRun(ctx, planRun)
	}
	itemID := strings.TrimSpace(planRun.CurrentItemID)
	if itemID == "" {
		return nil
	}
	item, err := c.store.GetPlanItem(ctx, itemID)
	if err != nil || item.SessionID != run.SessionID {
		return nil
	}
	switch item.Status {
	case PlanItemDone, PlanItemSkipped:
	default:
		if _, err := c.UpdatePlanItem(ctx, run.SessionID, item.ID, PlanItemDone, ""); err != nil {
			return err
		}
	}
	if err := c.completeDonePlanParents(ctx, run.SessionID); err != nil {
		return err
	}
	plan, err := c.store.GetSessionPlan(ctx, run.SessionID)
	if err != nil {
		return err
	}
	if _, ok := NextExecutablePlanItem(plan); ok {
		planRun.Status = PlanRunIdle
	} else {
		planRun.Status = PlanRunCompleted
		planRun.CurrentItemID = ""
	}
	planRun.LastError = ""
	planRun.UpdatedAt = now
	return c.store.SavePlanRun(ctx, planRun)
}

func planRunItemID(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		left, right, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(left), "Plan item id") {
			continue
		}
		return strings.TrimSpace(right)
	}
	return ""
}

func (c *Core) BindSessionPlanRunStep(ctx context.Context, sessionID string, runID string) error {
	sessionID = normalizeText(sessionID)
	runID = normalizeText(runID)
	if sessionID == "" || runID == "" {
		return nil
	}
	planRun, err := c.store.GetPlanRun(ctx, sessionID)
	if err != nil || planRun.Status != PlanRunRunning {
		return nil
	}
	planRun.LastRunID = runID
	planRun.UpdatedAt = c.now().UTC()
	return c.store.SavePlanRun(ctx, planRun)
}

func NextExecutablePlanItem(plan SessionPlan) (PlanItem, bool) {
	children := make(map[string][]PlanItem, len(plan.Items))
	for _, item := range plan.Items {
		parentID := strings.TrimSpace(item.ParentID)
		if parentID != "" {
			children[parentID] = append(children[parentID], item)
		}
	}
	for _, status := range []PlanItemStatus{PlanItemActive, PlanItemPending} {
		for _, item := range plan.Items {
			if item.Status != status {
				continue
			}
			if hasOpenPlanChildren(item.ID, children) {
				continue
			}
			return item, true
		}
	}
	return PlanItem{}, false
}

func hasOpenPlanChildren(itemID string, children map[string][]PlanItem) bool {
	for _, child := range children[strings.TrimSpace(itemID)] {
		switch child.Status {
		case PlanItemDone, PlanItemSkipped:
			continue
		default:
			return true
		}
	}
	return false
}

func (c *Core) sessionPlanPrompt(ctx context.Context, sessionID string) string {
	if c == nil || c.store == nil {
		return ""
	}
	plan, err := c.store.GetSessionPlan(ctx, sessionID)
	if err != nil {
		return ""
	}
	lines := []string{
		"Session goal and plan:",
		"- Use plan tools for multi-step work: plan_get, plan_set_goal, plan_add_item, plan_update_item, plan_clear.",
		"- Skip plans for simple one-step requests; for larger tasks, keep top-level items current and mark completed work done before claiming completion.",
		"- Use subtasks only for genuinely large items; finish subtasks before marking the parent done.",
	}
	if strings.TrimSpace(plan.Goal) != "" {
		lines = append(lines, "- Current goal: "+strings.TrimSpace(plan.Goal))
	}
	depths := PlanItemDepths(plan.Items)
	for i, item := range plan.Items {
		indent := strings.Repeat("  ", min(depths[item.ID], 4))
		lines = append(lines, fmt.Sprintf("- %s%d. [%s] %s (id: %s)", indent, i+1, item.Status, item.Text, item.ID))
	}
	return strings.Join(lines, "\n")
}

func planRunLooksBlocked(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "plan_blocked" || strings.HasPrefix(line, "plan_blocked:") {
			return true
		}
	}
	return false
}

func (c *Core) completeDonePlanParents(ctx context.Context, sessionID string) error {
	for {
		plan, err := c.store.GetSessionPlan(ctx, sessionID)
		if err != nil {
			return nil
		}
		children := make(map[string][]PlanItem, len(plan.Items))
		for _, item := range plan.Items {
			parentID := strings.TrimSpace(item.ParentID)
			if parentID != "" {
				children[parentID] = append(children[parentID], item)
			}
		}
		changed := false
		for _, item := range plan.Items {
			if item.Status == PlanItemDone || item.Status == PlanItemSkipped || len(children[item.ID]) == 0 {
				continue
			}
			if !allPlanChildrenTerminal(children[item.ID]) {
				continue
			}
			if _, err := c.UpdatePlanItem(ctx, sessionID, item.ID, PlanItemDone, ""); err != nil {
				return err
			}
			changed = true
			break
		}
		if !changed {
			return nil
		}
	}
}

func allPlanChildrenTerminal(items []PlanItem) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		switch item.Status {
		case PlanItemDone, PlanItemSkipped:
			continue
		default:
			return false
		}
	}
	return true
}

func (c *Core) completeActivePlanItemsIfRunFinished(ctx context.Context, sessionID string) error {
	if planRun, err := c.store.GetPlanRun(ctx, sessionID); err == nil && planRun.Status == PlanRunBlocked {
		return nil
	}
	plan, err := c.store.GetSessionPlan(ctx, sessionID)
	if err != nil {
		return err
	}
	var active []PlanItem
	for _, item := range plan.Items {
		switch item.Status {
		case PlanItemPending:
			return nil
		case PlanItemActive:
			active = append(active, item)
		}
	}
	for _, item := range active {
		if _, err := c.UpdatePlanItem(ctx, sessionID, item.ID, PlanItemDone, ""); err != nil {
			return err
		}
	}
	return nil
}

// runReply is the text of the run's last reply ending with assistant, joined with
// the replies the output limit cut before it.
func (c *Core) runReply(ctx context.Context, run Run, assistant transcript.Message) (string, error) {
	messages, err := c.store.ListMessages(ctx, run.SessionID, 0)
	if err != nil {
		return "", err
	}
	assistant.RunID = run.ID
	if i := slices.IndexFunc(messages, func(message transcript.Message) bool { return message.ID == assistant.ID }); i >= 0 {
		messages[i] = assistant
	} else {
		messages = append(messages, assistant)
	}
	return transcript.RunReply(messages, run.ID), nil
}
