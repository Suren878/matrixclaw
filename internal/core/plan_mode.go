package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const exitPlanModeToolName = "exit_plan_mode"

// planModeBlocked is what the model reads for a call plan mode blocks.
const planModeBlocked = "Plan mode is on: calls that may change something are blocked until the user approves your plan. " +
	"Explore with read-only tools (read, grep, glob, ls, read-only bash such as git log or git diff, read-only subagents), " +
	"then call exit_plan_mode with the plan."

// planApprovedMode is the mode an approved plan switches the session to,
// whatever mode it had before plan mode: approving a plan approves its edits,
// never more (a member may decide approvals, only the owner switches modes).
const planApprovedMode = PermissionModeAcceptEdits

type exitPlanModeInput struct {
	Plan string `json:"plan"`
}

// PlanModeToolExecutors returns exit_plan_mode, offered only in plan mode.
func PlanModeToolExecutors(app *Core) []tools.Executor {
	return []tools.Executor{&exitPlanModeTool{app: app}}
}

type exitPlanModeTool struct {
	app *Core
}

// Spec marks exit_plan_mode read-only, so plan mode does not block it; it
// always asks, and its approval is the user's review of the plan.
func (t *exitPlanModeTool) Spec() tools.Spec {
	return tools.Spec{
		ID: exitPlanModeToolName,
		Description: "You are in plan mode: calls that change files or run mutating commands are blocked. " +
			"Explore with read-only tools until you know what to do, then call this with your plan to ask the user to approve it. " +
			"Write the plan in markdown: the steps, the files to change and how you will verify the result. " +
			"When the user approves, edits inside the working directory run without asking and you carry out the plan in this same turn; " +
			"when they reject it, their reason comes back and you stay in plan mode to revise it. Do not ask for approval in a reply instead.",
		Effect:          tools.EffectReadOnly,
		Asks:            true,
		Namespace:       "core.plan",
		Category:        tools.CategoryAutomation,
		InputJSONSchema: exitPlanModeSchema,
	}
}

// Preview is the plan as the user reviews it; a call outside plan mode is
// answered before anyone is asked.
func (t *exitPlanModeTool) Preview(ctx context.Context, call tools.Call) (tools.ApprovalRequest, error) {
	plan, err := t.plan(ctx, call)
	if err != nil {
		return tools.ApprovalRequest{}, err
	}
	return tools.ApprovalRequest{Description: fmt.Sprintf("Approve this plan? The session then switches to %s and the agent carries it out.\n\n%s", planApprovedMode, plan)}, nil
}

// Execute runs once the user approved the plan and switches the session out
// of plan mode.
func (t *exitPlanModeTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if _, err := t.plan(ctx, call); err != nil {
		var invalid notInPlanMode
		if errors.As(err, &invalid) {
			return tools.Result{Content: err.Error(), Status: tools.ResultStatusNeutral}, nil
		}
		if errors.Is(err, tools.ErrInvalidArgs) {
			return tools.Result{}, err
		}
		return tools.Result{Content: err.Error(), Status: tools.ResultStatusError}, nil
	}
	if _, err := t.app.UpdateSessionPermissionMode(ctx, UpdateSessionPermissionModeInput{SessionID: call.SessionID, PermissionMode: planApprovedMode}); err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Content: fmt.Sprintf("The user approved the plan. The permission mode is now %s: file edits inside the working directory run without asking, other changes may still ask. Carry out the plan now.", planApprovedMode)}, nil
}

// notInPlanMode answers a call made while the session is not in plan mode.
type notInPlanMode struct{ mode PermissionMode }

func (e notInPlanMode) Error() string {
	return fmt.Sprintf("Plan mode is off (the permission mode is %s); go on with the work without exit_plan_mode.", e.mode)
}

// plan reads the call's plan and checks that its session is in plan mode.
func (t *exitPlanModeTool) plan(ctx context.Context, call tools.Call) (string, error) {
	var input exitPlanModeInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return "", tools.InvalidArgs(exitPlanModeToolName, err)
	}
	plan := strings.TrimSpace(input.Plan)
	if plan == "" {
		return "", errors.New("plan is required: the steps, the files to change and how you will verify the result")
	}
	session, err := t.app.store.GetSession(ctx, call.SessionID)
	if err != nil {
		return "", err
	}
	if mode := NormalizePermissionMode(string(session.PermissionMode)); mode != PermissionModePlan {
		return "", notInPlanMode{mode: mode}
	}
	return plan, nil
}

var exitPlanModeSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "plan": {"type": "string", "description": "The plan in markdown: the steps, the files to change and how you will verify the result."}
  },
  "required": ["plan"],
  "additionalProperties": false
}`)
