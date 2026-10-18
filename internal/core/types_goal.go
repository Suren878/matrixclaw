package core

import (
	"context"
	"time"
)

// GoalStatus is where a session goal stands.
type GoalStatus string

const (
	// GoalActive keeps the session working on the goal run after run.
	GoalActive GoalStatus = "active"
	// GoalPaused waits for /goal resume: the user stopped it, a run failed or
	// its allowance of runs or tokens is used up.
	GoalPaused GoalStatus = "paused"
	// GoalBlocked cannot go on without the user.
	GoalBlocked GoalStatus = "blocked"
	// GoalComplete is done, with evidence and a passing check.
	GoalComplete GoalStatus = "complete"
)

const (
	// MaxGoalObjective bounds the objective, in bytes.
	MaxGoalObjective = 2000
	// DefaultGoalRuns and MaxGoalRuns are the runs a goal may start on its own.
	DefaultGoalRuns = 10
	MaxGoalRuns     = 50
)

// SessionGoal is a long objective the session works on across runs until it
// is verifiably done. Reason says why it is paused or blocked, or holds the
// evidence it was completed with. RunsUsed and TokensUsed count the runs the
// goal started; LastRunID and LastStop name the newest run that ended since
// the goal was set and how it ended. CheckFailure and CheckFailures identify
// the newest failed check output and how often in a row it repeated;
// LastBlocker, BlockerRepeats and BlockerRunID the blocker the model reported.
type SessionGoal struct {
	SessionID      string     `json:"session_id"`
	Objective      string     `json:"objective"`
	CheckCommand   string     `json:"check_command,omitempty"`
	MaxRuns        int        `json:"max_runs"`
	TokenBudget    int64      `json:"token_budget,omitempty"`
	Status         GoalStatus `json:"status"`
	Reason         string     `json:"reason,omitempty"`
	RunsUsed       int        `json:"runs_used"`
	TokensUsed     int64      `json:"tokens_used,omitempty"`
	LastRunID      string     `json:"last_run_id,omitempty"`
	LastStop       string     `json:"last_stop,omitempty"`
	LastCheck      string     `json:"last_check,omitempty"`
	CheckFailure   string     `json:"-"`
	CheckFailures  int        `json:"check_failures,omitempty"`
	LastBlocker    string     `json:"last_blocker,omitempty"`
	BlockerRepeats int        `json:"blocker_repeats,omitempty"`
	BlockerRunID   string     `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// GoalInput sets a session goal; zero MaxRuns takes DefaultGoalRuns and zero
// TokenBudget leaves tokens unlimited. Client, ExternalKey and
// ClientCapabilities name who sets it: a client that receives deliveries gets
// the goal's first run.
type GoalInput struct {
	Objective          string             `json:"objective"`
	CheckCommand       string             `json:"check_command,omitempty"`
	MaxRuns            int                `json:"max_runs,omitempty"`
	TokenBudget        int64              `json:"token_budget,omitempty"`
	Client             string             `json:"client,omitempty"`
	ExternalKey        string             `json:"external_key,omitempty"`
	ClientCapabilities ClientCapabilities `json:"client_capabilities,omitempty"`
}

// GoalResponse is a session's goal; nil when it has none.
type GoalResponse struct {
	Goal *SessionGoal `json:"goal"`
}

// GoalStore keeps each session's goal, one at most.
type GoalStore interface {
	GetSessionGoal(ctx context.Context, sessionID string) (SessionGoal, error)
	SaveSessionGoal(ctx context.Context, goal SessionGoal) error
	DeleteSessionGoal(ctx context.Context, sessionID string) error
	ListActiveSessionGoals(ctx context.Context) ([]SessionGoal, error)
}
