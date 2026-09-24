package core

import (
	"fmt"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
)

// SessionBudget overrides the daemon run budget for one session. A nil field
// inherits the default; Tokens 0 means unlimited.
type SessionBudget struct {
	Steps         *int   `json:"steps,omitempty"`
	ActiveSeconds *int64 `json:"active_seconds,omitempty"`
	Tokens        *int64 `json:"tokens,omitempty"`
}

// IsZero reports whether the session overrides nothing.
func (b SessionBudget) IsZero() bool {
	return b.Steps == nil && b.ActiveSeconds == nil && b.Tokens == nil
}

func (b SessionBudget) apply(budget agent.Budget) agent.Budget {
	if b.Steps != nil {
		budget.Steps = *b.Steps
	}
	if b.ActiveSeconds != nil {
		budget.ActiveTime = time.Duration(*b.ActiveSeconds) * time.Second
	}
	if b.Tokens != nil {
		budget.Tokens = *b.Tokens
	}
	return budget
}

func (b SessionBudget) validate() error {
	switch {
	case b.Steps != nil && *b.Steps < 1:
		return fmt.Errorf("%w: budget steps must be at least 1", ErrInvalidInput)
	case b.ActiveSeconds != nil && *b.ActiveSeconds < 60:
		return fmt.Errorf("%w: budget active time must be at least one minute", ErrInvalidInput)
	case b.Tokens != nil && *b.Tokens < 0:
		return fmt.Errorf("%w: budget tokens must not be negative", ErrInvalidInput)
	default:
		return nil
	}
}

// SessionBudgetReport is a session's overrides and the effective budget: the
// default for the session's kind (a subagent session gets the subagent default,
// others the user default) with the overrides applied.
type SessionBudgetReport struct {
	SessionID     string        `json:"session_id"`
	Override      SessionBudget `json:"override"`
	Steps         int           `json:"steps"`
	ActiveSeconds int64         `json:"active_seconds"`
	Tokens        int64         `json:"tokens"`
}
