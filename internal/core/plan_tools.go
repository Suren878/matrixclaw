package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const (
	planGetToolName    = "plan_get"
	planGoalToolName   = "plan_set_goal"
	planAddToolName    = "plan_add_item"
	planUpdateToolName = "plan_update_item"
	planClearToolName  = "plan_clear"
)

// PlanToolExecutors exposes session plan operations to models through the normal tool registry.
func PlanToolExecutors(app *Core) []tools.Executor {
	if app == nil {
		return nil
	}
	return []tools.Executor{
		&planToolExecutor{app: app, spec: planToolSpec(planGetToolName, "PlanGet", "Read the current session goal and plan", tools.EffectReadOnly, planEmptySchema)},
		&planToolExecutor{app: app, spec: planToolSpec(planGoalToolName, "PlanSetGoal", "Set or replace the current session goal", tools.EffectMutation, planGoalSchema)},
		&planToolExecutor{app: app, spec: planToolSpec(planAddToolName, "PlanAddItem", "Add a task to the current session plan", tools.EffectMutation, planAddSchema)},
		&planToolExecutor{app: app, spec: planToolSpec(planUpdateToolName, "PlanUpdateItem", "Update a plan item status or text", tools.EffectMutation, planUpdateSchema)},
		&planToolExecutor{app: app, spec: planToolSpec(planClearToolName, "PlanClear", "Clear the current session goal and plan", tools.EffectMutation, planEmptySchema)},
	}
}

type planToolExecutor struct {
	app  *Core
	spec tools.Spec
}

func (e *planToolExecutor) Spec() tools.Spec { return e.spec }

func (e *planToolExecutor) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	sessionID := strings.TrimSpace(call.SessionID)
	if sessionID == "" {
		return tools.Result{Content: "session_id is required", IsError: true, Status: tools.ResultStatusError}, nil
	}
	var (
		plan SessionPlan
		err  error
	)
	switch e.spec.ID {
	case planGetToolName:
		plan, err = e.app.SessionPlan(ctx, sessionID)
	case planGoalToolName:
		var args struct {
			Goal string `json:"goal"`
		}
		if err := decodePlanToolArgs(call.Args, &args); err != nil {
			return tools.Result{}, err
		}
		plan, err = e.app.SetSessionGoal(ctx, sessionID, args.Goal)
	case planAddToolName:
		var args struct {
			Text     string `json:"text"`
			ParentID string `json:"parent_id,omitempty"`
		}
		if err := decodePlanToolArgs(call.Args, &args); err != nil {
			return tools.Result{}, err
		}
		plan, err = e.app.AddPlanItem(ctx, sessionID, args.Text, args.ParentID)
	case planUpdateToolName:
		var args struct {
			ItemID string `json:"item_id"`
			Status string `json:"status,omitempty"`
			Text   string `json:"text,omitempty"`
		}
		if err := decodePlanToolArgs(call.Args, &args); err != nil {
			return tools.Result{}, err
		}
		plan, err = e.app.UpdatePlanItem(ctx, sessionID, args.ItemID, PlanItemStatus(args.Status), args.Text)
	case planClearToolName:
		plan, err = e.app.ClearSessionPlan(ctx, sessionID)
	default:
		err = fmt.Errorf("%w: unknown plan tool %q", ErrInvalidInput, e.spec.ID)
	}
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true, Status: tools.ResultStatusError}, nil
	}
	return tools.Result{
		Content: planToolResultContent(e.spec.ID, plan),
		Metadata: map[string]any{
			"plan": plan,
		},
		Status: tools.ResultStatusSuccess,
	}, nil
}

func planToolSpec(id string, name string, description string, effect tools.Effect, schema json.RawMessage) tools.Spec {
	return tools.Spec{
		ID:              id,
		Name:            name,
		Description:     description,
		Risk:            tools.RiskSafe,
		Effect:          effect,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "core.plan",
		Category:        tools.CategoryAutomation,
		Profiles:        []tools.Profile{tools.ProfileCoding, tools.ProfileAutomation},
		OutputKind:      tools.OutputText,
		InputJSONSchema: schema,
	}
}

func decodePlanToolArgs(args json.RawMessage, dest any) error {
	if len(args) == 0 {
		args = []byte(`{}`)
	}
	if err := json.Unmarshal(args, dest); err != nil {
		return fmt.Errorf("%w: %v", tools.ErrInvalidArgs, err)
	}
	return nil
}

func planToolResultContent(toolName string, plan SessionPlan) string {
	switch strings.TrimSpace(toolName) {
	case planGetToolName:
		return planToolSummary(plan)
	case planClearToolName:
		return "Plan cleared."
	default:
		return "Plan updated. Current plan is available in the session plan context."
	}
}

func planToolSummary(plan SessionPlan) string {
	lines := []string{"Goal: " + strings.TrimSpace(plan.Goal)}
	if strings.TrimSpace(plan.Goal) == "" {
		lines[0] = "Goal: not set"
	}
	depths := PlanItemDepths(plan.Items)
	for i, item := range plan.Items {
		indent := strings.Repeat("  ", min(depths[item.ID], 4))
		lines = append(lines, fmt.Sprintf("%s%d. [%s] %s (id: %s)", indent, i+1, item.Status, item.Text, item.ID))
	}
	return strings.Join(lines, "\n")
}

var (
	planEmptySchema  = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	planGoalSchema   = json.RawMessage(`{"type":"object","properties":{"goal":{"type":"string"}},"required":["goal"],"additionalProperties":false}`)
	planAddSchema    = json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"parent_id":{"type":"string","description":"Optional parent plan item id for a subtask."}},"required":["text"],"additionalProperties":false}`)
	planUpdateSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "item_id": {"type": "string"},
    "status": {"type": "string", "enum": ["pending", "active", "done", "skipped"]},
    "text": {"type": "string"}
  },
  "required": ["item_id"],
  "additionalProperties": false
}`)
)
