package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent/prompt"
	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const (
	goalGetToolName    = "goal_get"
	goalUpdateToolName = "goal_update"
	// goalBlockerRepeats is how many consecutive goal runs must report the
	// same blocker before the goal counts as blocked.
	goalBlockerRepeats = 3
)

// goalTools are offered only to runs of a session with an active goal.
var goalTools = map[string]bool{goalGetToolName: true, goalUpdateToolName: true}

// GoalToolExecutors exposes the session goal to models. A goal is set by the
// user only, so there is no tool that creates one.
func GoalToolExecutors(app *Core) []tools.Executor {
	if app == nil {
		return nil
	}
	return []tools.Executor{&goalGetTool{app: app}, &goalUpdateTool{app: app}}
}

type goalGetTool struct {
	app *Core
}

func (t *goalGetTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              goalGetToolName,
		Description:     "Show the session goal: objective, status, check command, runs and tokens used, and the last check and blocker.",
		Effect:          tools.EffectReadOnly,
		Namespace:       "core.goal",
		Category:        tools.CategoryAutomation,
		InputJSONSchema: json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
	}
}

func (t *goalGetTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	response, err := t.app.SessionGoal(ctx, call.SessionID)
	if err != nil {
		return tools.Result{}, err
	}
	if response.Goal == nil {
		return tools.Result{Content: "The session has no goal.", Status: tools.ResultStatusNeutral}, nil
	}
	return tools.Result{Content: goalText(*response.Goal), Metadata: response.Goal}, nil
}

type goalUpdateTool struct {
	app *Core
}

// Spec marks goal_update read-only: it changes only the goal's own record, and
// the check it may run is the one the user set to run without asking.
func (t *goalUpdateTool) Spec() tools.Spec {
	return tools.Spec{
		ID: goalUpdateToolName,
		Description: "Report the session goal complete or blocked. " +
			"complete: only when every requirement of the objective is met; give the evidence (commands that passed, files, tests). The goal's check command runs first and must pass, and no todo item may be open. " +
			"blocked: only when you cannot go on without the user; give the blocker. It is accepted after the same blocker is reported in 3 consecutive goal runs, so repeat it word for word; until then try another approach.",
		Effect:          tools.EffectReadOnly,
		Namespace:       "core.goal",
		Category:        tools.CategoryAutomation,
		InputJSONSchema: goalUpdateToolSchema,
	}
}

// ConcurrencyKey serialises one session's goal updates.
func (t *goalUpdateTool) ConcurrencyKey(call tools.Call) string {
	return "goal:" + call.SessionID
}

var goalUpdateToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "status": {"type": "string", "enum": ["complete", "blocked"]},
    "evidence": {"type": "string", "description": "For complete: each requirement of the objective and the evidence that it is met."},
    "blocker": {"type": "string", "description": "For blocked: what stops you and what you need from the user."}
  },
  "required": ["status"],
  "additionalProperties": false
}`)

func (t *goalUpdateTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input struct {
		Status   GoalStatus `json:"status"`
		Evidence string     `json:"evidence"`
		Blocker  string     `json:"blocker"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{}, tools.InvalidArgs(goalUpdateToolName, err)
	}
	goal, active, err := t.app.activeGoal(ctx, call.SessionID)
	if err != nil {
		return tools.Result{}, err
	}
	if !active {
		return goalError("The session has no active goal."), nil
	}
	switch input.Status {
	case GoalComplete:
		return t.app.completeGoal(ctx, goal, call, strings.TrimSpace(input.Evidence))
	case GoalBlocked:
		return t.app.reportGoalBlocker(ctx, call, strings.TrimSpace(input.Blocker))
	default:
		return goalError("status must be complete or blocked."), nil
	}
}

func goalError(text string) tools.Result {
	return tools.Result{Content: text, Status: tools.ResultStatusError}
}

// completeGoal marks the goal complete when evidence is given, no todo item of
// the run's chain is open and the check, if any, passes.
func (c *Core) completeGoal(ctx context.Context, goal SessionGoal, call tools.Call, evidence string) (tools.Result, error) {
	if evidence == "" {
		return goalError("Give the evidence: map each requirement of the objective to what shows it is met."), nil
	}
	chain := []string{call.RunID}
	if run, err := c.store.GetRun(ctx, call.RunID); err == nil {
		continues, err := c.continuedRuns(ctx, run)
		if err != nil {
			return tools.Result{}, err
		}
		chain = append(chain, continues...)
	}
	open, err := coreTodos{c: c}.Open(ctx, call.SessionID, chain)
	if err != nil {
		return tools.Result{}, err
	}
	if len(open) > 0 {
		return goalError(fmt.Sprintf("The goal is not complete: your todo list has %d open items:\n%s\nFinish them, or update the list if they no longer apply.", len(open), todo.Text(open))), nil
	}
	if goal.CheckCommand != "" {
		check, recorded, err := c.checkGoal(ctx, goal)
		if err != nil {
			return tools.Result{}, err
		}
		if !check.passed() {
			text := check.failure() + "\nThe goal is not complete."
			if recorded.Status == GoalBlocked {
				text += " It is now blocked: " + recorded.Reason + "."
			}
			return goalError(text), nil
		}
	}
	completed, err := c.updateGoal(ctx, call.SessionID, func(current *SessionGoal) error {
		if current.Status != GoalActive {
			return fmt.Errorf("%w: the goal is %s", ErrInvalidInput, current.Status)
		}
		current.Status, current.Reason = GoalComplete, evidence
		return nil
	})
	if err != nil {
		return tools.Result{}, err
	}
	text := "The goal is complete."
	if completed.CheckCommand != "" {
		text = fmt.Sprintf("The goal check `%s` passed; the goal is complete.", completed.CheckCommand)
	}
	return tools.Result{Content: text + " Summarise the evidence in your reply."}, nil
}

// reportGoalBlocker records a blocker. Reported by goalBlockerRepeats
// consecutive runs (each continuing the one before), it blocks the goal.
func (c *Core) reportGoalBlocker(ctx context.Context, call tools.Call, blocker string) (tools.Result, error) {
	if blocker == "" {
		return goalError("Describe the blocker: what stops you and what you need from the user."), nil
	}
	run, err := c.store.GetRun(ctx, call.RunID)
	if err != nil {
		return tools.Result{}, err
	}
	same := strings.Join(strings.Fields(strings.ToLower(blocker)), " ")
	goal, err := c.updateGoal(ctx, call.SessionID, func(current *SessionGoal) error {
		if current.Status != GoalActive {
			return fmt.Errorf("%w: the goal is %s", ErrInvalidInput, current.Status)
		}
		switch {
		case strings.Join(strings.Fields(strings.ToLower(current.LastBlocker)), " ") != same:
			current.BlockerRepeats = 1
		case current.BlockerRunID == run.ID:
		case current.BlockerRunID == run.ContinuesRunID:
			current.BlockerRepeats++
		default:
			current.BlockerRepeats = 1
		}
		current.LastBlocker, current.BlockerRunID = blocker, run.ID
		if current.BlockerRepeats >= goalBlockerRepeats {
			current.Status, current.Reason = GoalBlocked, blocker
		}
		return nil
	})
	if err != nil {
		return tools.Result{}, err
	}
	if goal.Status == GoalBlocked {
		return tools.Result{Content: "The goal is blocked and waits for the user. Explain the blocker and what you need in your reply."}, nil
	}
	return tools.Result{
		Content: fmt.Sprintf("Blocker recorded (%d of %d consecutive goal runs). The goal stays active: try another approach to get past it; report it again in the next goal run only if it still stops you.", goal.BlockerRepeats, goalBlockerRepeats),
		Status:  tools.ResultStatusNeutral,
	}, nil
}

// goalText describes a goal for goal_get.
func goalText(goal SessionGoal) string {
	lines := []string{"Objective: " + goal.Objective, "Status: " + string(goal.Status)}
	if goal.Reason != "" {
		lines = append(lines, "Reason: "+goal.Reason)
	}
	lines = append(lines, goalFacts(goal)...)
	return strings.Join(lines, "\n")
}

// goalFacts are the goal's check, use and last results.
func goalFacts(goal SessionGoal) []string {
	var lines []string
	if goal.CheckCommand != "" {
		check := "Check command: `" + goal.CheckCommand + "`"
		if goal.LastCheck != "" {
			check += ", last " + goal.LastCheck
		}
		lines = append(lines, check)
	}
	use := fmt.Sprintf("Goal runs used: %d of %d", goal.RunsUsed, goal.MaxRuns)
	if goal.TokenBudget > 0 {
		use += fmt.Sprintf("; tokens used: %d of %d", goal.TokensUsed, goal.TokenBudget)
	}
	lines = append(lines, use)
	if goal.LastStop != "" {
		lines = append(lines, "Last run: "+goal.LastStop)
	}
	if goal.LastBlocker != "" && goal.Status == GoalActive {
		lines = append(lines, fmt.Sprintf("Last blocker reported (%d of %d): %s", goal.BlockerRepeats, goalBlockerRepeats, goal.LastBlocker))
	}
	return lines
}

// goalPrompt is the context note section of the session's active goal, for
// a run that started with it and so has the goal tools.
func (c *Core) goalPrompt(ctx context.Context, turn nativeTurn) string {
	if !turn.Goal {
		return ""
	}
	goal, active, err := c.activeGoal(ctx, turn.SessionID)
	if err != nil || !active {
		return ""
	}
	return prompt.JoinSections(
		"Session goal (you work on it run after run on your own until it is verifiably done):\n"+goal.Objective,
		strings.Join(goalFacts(goal), "\n"),
		goalGuidance,
	)
}

const goalGuidance = `Working on the goal:
- No-progress check: first classify the previous run as progress (it moved the goal forward), verified wait (it waited on something real, such as a background task) or no progress. After no progress, change your approach instead of repeating it.
- Fidelity: work toward the whole objective as written. Do not narrow it, reinterpret it or declare parts out of scope.
- Keep the todo list current; the goal cannot be complete while items are open.
- Completion audit: before reporting the goal complete, map every requirement of the objective to authoritative evidence (a passing command, a test, the file that does it). Weak or indirect evidence means not done.
- When it is done, call goal_update with status complete and that evidence. Call it with status blocked only when you cannot go on without the user.
- Do not stop to ask questions the objective already answers; nobody watches each run.`
