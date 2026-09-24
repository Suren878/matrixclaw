package agent

import (
	"time"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// Task is one native run to execute. Resume holds the counters the run had
// reached before it was parked or interrupted; a new run starts from zero.
type Task struct {
	RunID       string
	SessionID   string
	Client      string
	ExternalKey string
	WorkingDir  string
	Model       Model
	Budget      Budget
	Resume      Counters
}

// Budget limits one run; a zero field is unlimited.
type Budget struct {
	Steps      int
	ActiveTime time.Duration
	Tokens     int64
}

// Status is how a Run call ended.
type Status string

const (
	StatusCompleted       Status = "completed"
	StatusWaitingApproval Status = "waiting_approval"
	StatusInterrupted     Status = "interrupted"
	StatusCanceled        Status = "canceled"
	StatusFailed          Status = "failed"
)

// Outcome is applied by core. Assistant is the final reply (completed), the reply to
// seal (canceled, interrupted) or the errored reply (failed with MarkErrored).
// Reached is what the last step produced before an interruption; StopReason is set
// whenever the run completed or reached completion.
type Outcome struct {
	Status         Status
	StopReason     StopReason
	Assistant      *transcript.Message
	AssistantSaved bool
	Err            error
	MarkErrored    bool
	Reached        Status
}

// StopReason says why a completed run stopped.
type StopReason string

const (
	StopDone            StopReason = "done"
	StopBudgetExhausted StopReason = "budget_exhausted"
	StopLoopDetected    StopReason = "loop_detected"
)

// Continuable reports whether a run stopped before its work was done.
func (r StopReason) Continuable() bool {
	return r == StopBudgetExhausted || r == StopLoopDetected
}
