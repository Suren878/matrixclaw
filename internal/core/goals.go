package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/events"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// RunTriggerGoal is a run the session started on its own to work on its goal.
const RunTriggerGoal RunTrigger = "goal"

// EventGoalUpdated carries a GoalResponse whenever a session goal changes.
const EventGoalUpdated events.Type = "goal.updated"

// SessionGoal returns the session's goal, nil when it has none.
func (c *Core) SessionGoal(ctx context.Context, sessionID string) (GoalResponse, error) {
	session, err := c.store.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return GoalResponse{}, err
	}
	goal, err := c.store.GetSessionGoal(ctx, session.ID)
	if errors.Is(err, ErrNotFound) {
		return GoalResponse{}, nil
	}
	if err != nil {
		return GoalResponse{}, err
	}
	return GoalResponse{Goal: &goal}, nil
}

// SetSessionGoal gives the session a new goal in place of the one it had, and
// starts working on it when the session is idle.
func (c *Core) SetSessionGoal(ctx context.Context, sessionID string, input GoalInput) (GoalResponse, error) {
	input.Objective, input.CheckCommand = strings.TrimSpace(input.Objective), strings.TrimSpace(input.CheckCommand)
	if input.MaxRuns == 0 {
		input.MaxRuns = DefaultGoalRuns
	}
	switch {
	case input.Objective == "":
		return GoalResponse{}, fmt.Errorf("%w: a goal needs an objective", ErrInvalidInput)
	case len(input.Objective) > MaxGoalObjective:
		return GoalResponse{}, fmt.Errorf("%w: the objective is longer than %d bytes; put the details in a file and name it", ErrInvalidInput, MaxGoalObjective)
	case input.MaxRuns < 1 || input.MaxRuns > MaxGoalRuns:
		return GoalResponse{}, fmt.Errorf("%w: a goal may start 1 to %d runs", ErrInvalidInput, MaxGoalRuns)
	case input.TokenBudget < 0:
		return GoalResponse{}, fmt.Errorf("%w: the token budget cannot be negative", ErrInvalidInput)
	}
	session, err := c.store.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return GoalResponse{}, err
	}
	if isSubagentSession(session) || CapabilitiesForSession(session).ExternalAgent {
		return GoalResponse{}, fmt.Errorf("%w: goals are kept by native top-level sessions only", ErrInvalidInput)
	}
	now := c.now().UTC()
	goal := SessionGoal{
		SessionID: session.ID, Objective: input.Objective, CheckCommand: input.CheckCommand,
		MaxRuns: input.MaxRuns, TokenBudget: input.TokenBudget, Status: GoalActive,
		CreatedAt: now, UpdatedAt: now,
	}
	gate := c.sessionGate(session.ID)
	gate.Lock()
	err = c.saveGoal(ctx, goal)
	gate.Unlock()
	if err != nil {
		return GoalResponse{}, err
	}
	var setter wakeTarget
	if input.ClientCapabilities.ReceivesDeliveries && input.Client != "" && input.ExternalKey != "" {
		setter = wakeTarget{client: input.Client, externalKey: input.ExternalKey, capabilities: input.ClientCapabilities}
	}
	return GoalResponse{Goal: &goal}, c.continueGoalTo(ctx, session.ID, setter)
}

// GoalAction is a change of a goal's status by the user.
type GoalAction string

const (
	GoalPause  GoalAction = "pause"
	GoalResume GoalAction = "resume"
)

// ChangeSessionGoal pauses an active goal, or resumes a paused or blocked one
// with a fresh allowance of runs and tokens when it had used them up.
func (c *Core) ChangeSessionGoal(ctx context.Context, sessionID string, action GoalAction) (GoalResponse, error) {
	sessionID = strings.TrimSpace(sessionID)
	goal, err := c.updateGoal(ctx, sessionID, func(goal *SessionGoal) error {
		switch {
		case action == GoalPause && goal.Status == GoalActive:
			goal.Status, goal.Reason = GoalPaused, "paused by the user"
		case action == GoalResume && (goal.Status == GoalPaused || goal.Status == GoalBlocked):
			goal.Status, goal.Reason = GoalActive, ""
			goal.CheckFailure, goal.CheckFailures, goal.BlockerRepeats = "", 0, 0
			if goal.allowanceUsed() {
				goal.RunsUsed, goal.TokensUsed = 0, 0
			}
		case action != GoalPause && action != GoalResume:
			return fmt.Errorf("%w: unknown goal action %q", ErrInvalidInput, action)
		default:
			return fmt.Errorf("%w: cannot %s a goal that is %s", ErrInvalidInput, action, goal.Status)
		}
		return nil
	})
	if err != nil {
		return GoalResponse{}, err
	}
	if action == GoalResume {
		err = c.continueGoal(ctx, sessionID)
	}
	return GoalResponse{Goal: &goal}, err
}

// ClearSessionGoal removes the session's goal; a run working on it finishes,
// and no other starts for it.
func (c *Core) ClearSessionGoal(ctx context.Context, sessionID string) (GoalResponse, error) {
	session, err := c.store.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return GoalResponse{}, err
	}
	gate := c.sessionGate(session.ID)
	gate.Lock()
	defer gate.Unlock()
	if err := c.store.DeleteSessionGoal(ctx, session.ID); err != nil {
		return GoalResponse{}, err
	}
	c.publishEvent(events.Event{Type: EventGoalUpdated, SessionID: session.ID, Payload: GoalResponse{}})
	return GoalResponse{}, nil
}

// allowanceUsed reports whether the goal started all the runs or used all the
// tokens it may.
func (g SessionGoal) allowanceUsed() bool {
	return g.RunsUsed >= g.MaxRuns || g.TokenBudget > 0 && g.TokensUsed >= g.TokenBudget
}

// updateGoal changes the session's goal under the session gate and announces it.
func (c *Core) updateGoal(ctx context.Context, sessionID string, change func(*SessionGoal) error) (SessionGoal, error) {
	gate := c.sessionGate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	goal, err := c.store.GetSessionGoal(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		return SessionGoal{}, fmt.Errorf("%w: the session has no goal", ErrNotFound)
	}
	if err != nil {
		return SessionGoal{}, err
	}
	if err := change(&goal); err != nil {
		return SessionGoal{}, err
	}
	goal.UpdatedAt = c.now().UTC()
	return goal, c.saveGoal(ctx, goal)
}

func (c *Core) saveGoal(ctx context.Context, goal SessionGoal) error {
	if err := c.store.SaveSessionGoal(ctx, goal); err != nil {
		return err
	}
	c.publishEvent(events.Event{Type: EventGoalUpdated, SessionID: goal.SessionID, Payload: GoalResponse{Goal: &goal}})
	return nil
}

// settleGoalRun counts an ended run against the session's active goal: a run
// the goal started uses one of its runs and its tokens; a canceled or failed
// run, or a used-up allowance, pauses the goal and tells the user. A run that
// started before the goal, or was counted already, changes nothing.
func (c *Core) settleGoalRun(ctx context.Context, run Run) error {
	gate := c.sessionGate(run.SessionID)
	gate.Lock()
	notice, err := c.settleGoalLocked(ctx, run)
	gate.Unlock()
	if err != nil || notice == nil {
		return err
	}
	return c.sendWakeNotice(ctx, *notice)
}

func (c *Core) settleGoalLocked(ctx context.Context, run Run) (*wakeNotice, error) {
	goal, err := c.store.GetSessionGoal(ctx, run.SessionID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil || !run.Status.Terminal() || run.ID == goal.LastRunID || run.StartedAt.Before(goal.CreatedAt) {
		return nil, err
	}
	// A run that completed or blocked the goal itself still counts against it.
	active := goal.Status == GoalActive
	if !active && run.Trigger != RunTriggerGoal {
		return nil, nil
	}
	goal.LastRunID, goal.LastStop = run.ID, runEndText(run)
	if run.Trigger == RunTriggerGoal {
		tokens, err := c.runTokens(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		goal.RunsUsed++
		goal.TokensUsed += tokens
	}
	switch {
	case !active:
	case run.Status == RunStatusCanceled:
		goal.Status, goal.Reason = GoalPaused, "a run was canceled"
	case run.Status == RunStatusFailed:
		goal.Status, goal.Reason = GoalPaused, "a run failed: "+run.Error
	case goal.RunsUsed >= goal.MaxRuns:
		goal.Status, goal.Reason = GoalPaused, fmt.Sprintf("it used all %s it may start", quantityText(goal.MaxRuns, "run"))
	case goal.TokenBudget > 0 && goal.TokensUsed >= goal.TokenBudget:
		goal.Status, goal.Reason = GoalPaused, fmt.Sprintf("it used its budget of %d tokens", goal.TokenBudget)
	}
	goal.UpdatedAt = c.now().UTC()
	if err := c.saveGoal(ctx, goal); err != nil || !active || goal.Status == GoalActive {
		return nil, err
	}
	return c.goalNotice(ctx, goal, "The goal is paused: "+goal.Reason+". Send /goal resume to go on.")
}

// goalNotice is a notice about the goal for the session and the place its
// runs are delivered to.
func (c *Core) goalNotice(ctx context.Context, goal SessionGoal, text string) (*wakeNotice, error) {
	session, err := c.store.GetSession(ctx, goal.SessionID)
	if err != nil {
		return nil, err
	}
	runs, err := c.store.ListSessionRuns(ctx, goal.SessionID, wakeChainWindow)
	if err != nil {
		return nil, err
	}
	target, err := c.latestWakeTarget(ctx, runs)
	if err != nil {
		return nil, err
	}
	return &wakeNotice{session: session, target: target, text: text}, nil
}

// runTokens is what the run's model generations used, as its budget counts them.
func (c *Core) runTokens(ctx context.Context, runID string) (int64, error) {
	steps, err := c.store.ListRunSteps(ctx, runID)
	if err != nil {
		return 0, err
	}
	var tokens int64
	for _, step := range steps {
		if step.StopReason != agent.CompactStopReason {
			tokens += step.PromptTokens + step.OutputTokens
		}
	}
	return tokens, nil
}

// runEndText says how an ended run ended, for the goal's context note.
func runEndText(run Run) string {
	switch {
	case run.Status == RunStatusCompleted && run.StopReason != "" && run.StopReason != agent.StopDone:
		return "stopped (" + string(run.StopReason) + ")"
	case run.Status == RunStatusCompleted:
		return "replied and stopped"
	default:
		return string(run.Status)
	}
}

// goalRunText is the user message of a run the goal started.
const goalRunText = "Continue working on the session goal described in the context update."

// continueGoal starts the next run of the session's active goal when the
// session is idle: no run, no queued message (the user's input goes first)
// and nothing that pauses the goal. The run is delivered where the session's
// latest delivered run went.
func (c *Core) continueGoal(ctx context.Context, sessionID string) error {
	return c.continueGoalTo(ctx, sessionID, wakeTarget{})
}

// continueGoalTo is continueGoal delivering the run to target, when it names one.
func (c *Core) continueGoalTo(ctx context.Context, sessionID string, target wakeTarget) error {
	gate := c.sessionGate(sessionID)
	gate.Lock()
	result, notice, err := c.prepareGoalRun(ctx, sessionID, target)
	gate.Unlock()
	switch {
	case err != nil:
		return err
	case notice != nil:
		return c.sendWakeNotice(ctx, *notice)
	case result == nil:
		return nil
	}
	if err := c.startRun(ctx, result.Run.ID); err != nil {
		_, err = c.failAcceptedRun(ctx, *result, err)
		return err
	}
	return nil
}

func (c *Core) prepareGoalRun(ctx context.Context, sessionID string, target wakeTarget) (*AcceptRunResult, *wakeNotice, error) {
	goal, err := c.store.GetSessionGoal(ctx, sessionID)
	if errors.Is(err, ErrNotFound) || err == nil && goal.Status != GoalActive {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if _, err := c.store.GetActiveRunBySession(ctx, sessionID); !errors.Is(err, ErrNotFound) {
		return nil, nil, err
	}
	inputs, err := c.store.ListPendingSessionInputs(ctx, sessionID)
	if err != nil || len(inputs) > 0 {
		return nil, nil, err
	}
	runs, err := c.store.ListSessionRuns(ctx, sessionID, wakeChainWindow)
	if err != nil {
		return nil, nil, err
	}
	continues := ""
	if len(runs) > 0 {
		// The newest run may have ended before a restart counted it.
		if notice, err := c.settleGoalLocked(ctx, runs[0]); err != nil || notice != nil {
			return nil, notice, err
		}
		continues = runs[0].ID
	}
	if target.client == "" {
		if target, err = c.latestWakeTarget(ctx, runs); err != nil {
			return nil, nil, err
		}
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	run := clientRun(goalRunText, transcript.NormalizeMessageParts(goalRunText, nil), target.client, target.externalKey, target.capabilities, deliveryTo{Address: target.address})
	run.Trigger, run.ContinuesRunID = RunTriggerGoal, continues
	result, err := c.createAcceptedRun(ctx, session, run)
	if err != nil {
		return nil, nil, err
	}
	return &result, nil, nil
}

// activeGoal is the session's goal while it is active.
func (c *Core) activeGoal(ctx context.Context, sessionID string) (SessionGoal, bool, error) {
	goal, err := c.store.GetSessionGoal(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		return SessionGoal{}, false, nil
	}
	return goal, err == nil && goal.Status == GoalActive, err
}

// goalBudget caps the token budget of a run the goal started at what the
// goal has left.
func (c *Core) goalBudget(ctx context.Context, run Run, budget agent.Budget) (agent.Budget, error) {
	if run.Trigger != RunTriggerGoal {
		return budget, nil
	}
	goal, active, err := c.activeGoal(ctx, run.SessionID)
	if err != nil || !active || goal.TokenBudget == 0 {
		return budget, err
	}
	left := max(goal.TokenBudget-goal.TokensUsed, 1)
	if budget.Tokens == 0 || left < budget.Tokens {
		budget.Tokens = left
	}
	return budget, nil
}

func quantityText(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
