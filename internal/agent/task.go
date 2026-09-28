package agent

import (
	"errors"
	"time"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// Task is one native run to execute. Resume holds the counters the run had
// reached before it was parked or interrupted; a new run starts from what the
// session's previous run carried over.
type Task struct {
	RunID       string
	SessionID   string
	Client      string
	ExternalKey string
	WorkingDir  string
	Model       Model
	// WindowTokens is the model's context window; 0 means unknown.
	WindowTokens int
	// CompactModel writes the run's summaries instead of Model when set;
	// CompactWindowTokens is its context window, 0 when unknown.
	CompactModel        Model
	CompactWindowTokens int
	Budget              Budget
	Resume              Counters
	// Continues lists the runs this one continues, nearest first; what they keep
	// verbatim past a summary stays kept.
	Continues []string
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
	StatusWaitingEvents   Status = "waiting_events"
	StatusInterrupted     Status = "interrupted"
	StatusCanceled        Status = "canceled"
	StatusFailed          Status = "failed"
)

// Outcome is applied by core. Assistant is the final reply (completed), the reply to
// seal (canceled, interrupted) or the errored reply (failed with MarkErrored).
// Reached is what the last step produced before an interruption; StopReason is set
// whenever the run completed or reached completion, and for a context-exhausted failure.
// Counters are the run's counters when it stopped.
type Outcome struct {
	Status         Status
	StopReason     StopReason
	Assistant      *transcript.Message
	AssistantSaved bool
	Err            error
	MarkErrored    bool
	Reached        Status
	Counters       Counters
}

// StopReason says why a completed run stopped.
type StopReason string

const (
	StopDone             StopReason = "done"
	StopBudgetExhausted  StopReason = "budget_exhausted"
	StopLoopDetected     StopReason = "loop_detected"
	StopContextExhausted StopReason = "context_exhausted"
)

// ErrContextExhausted fails a run whose conversation no longer fits the model's
// window even after summarising it.
var ErrContextExhausted = errors.New("context_exhausted: the conversation no longer fits the model's context window")

// Continuable reports whether a run stopped before its work was done.
func (r StopReason) Continuable() bool {
	return r == StopBudgetExhausted || r == StopLoopDetected
}
