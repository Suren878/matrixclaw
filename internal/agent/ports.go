// Package agent runs the native agent loop over narrow ports implemented by core.
package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// Model generates one assistant turn; providers.Runtime satisfies it.
type Model interface {
	Generate(ctx context.Context, req providers.Request) (providers.Response, error)
}

// Window is the transcript a run starts from: the session's newest context
// boundary, if any, and the messages after what it covers, ordered by seq.
type Window struct {
	Boundary *transcript.Message
	Messages []transcript.Message
}

// Compaction is the boundary's compaction; nil without a boundary.
func (w Window) Compaction() *transcript.Compaction {
	if w.Boundary == nil {
		return nil
	}
	return w.Boundary.Compaction
}

// Journal persists one session's transcript; during a run the engine is its only writer.
// BeginStreaming/Stream use the fast progress path; FinishStreaming is the durable
// update of an in-flight assistant message and the only update of a written message.
type Journal interface {
	Load(ctx context.Context, sessionID string) (Window, error)
	Append(ctx context.Context, msg transcript.Message) (seq int64, err error)
	BeginStreaming(ctx context.Context, msg transcript.Message) (seq int64, err error)
	Stream(ctx context.Context, msg transcript.Message) error
	FinishStreaming(ctx context.Context, msg transcript.Message) error
	Checkpoint(ctx context.Context, state State) error
	RecordStep(ctx context.Context, step Step) error
}

// Phase is the durable execution boundary a run last reached.
type Phase string

const (
	PhaseModel     Phase = "model"
	PhaseToolBatch Phase = "tool_batch"
)

// State is the checkpoint the engine writes before each model call, when it
// starts calls of a tool batch and when the run parks; its Counters let a parked
// or restarted run keep its budget.
type State struct {
	RunID    string
	Phase    Phase
	Batch    *ToolBatch
	Counters Counters
}

// ToolBatch names the calls of the batch being run, in call order, and those
// of them held back (deferred) when the checkpoint was written.
type ToolBatch struct {
	CallIDs     []string `json:"call_ids"`
	DeferredIDs []string `json:"deferred_ids,omitempty"`
}

// Step is one successful model generation (a run_steps row). StopReason is a
// provider stop reason or "compact" for a summary generation.
type Step struct {
	RunID      string
	Model      string
	Provider   string
	Usage      providers.Usage
	StopReason string
	Latency    time.Duration
	ToolCalls  int
}

// Decision says whether a requested tool call may run; Reason goes back to the model.
// A Barrier call that waits for approval holds back the calls after it in its batch.
// Calls sharing a non-empty Key run one at a time, across runs too.
type Decision struct {
	Allowed bool
	Reason  string
	Barrier bool
	Key     string
}

// Tools lists, authorizes, executes and finalizes the tools of one run. Execute
// errors are fatal; tool failures come back as IsError results. Execute runs on
// a goroutine of its own, concurrently with other calls of the batch; the other
// methods run on the engine goroutine. Finish runs after the result is written.
type Tools interface {
	Specs(ctx context.Context) []tools.Spec
	Authorize(ctx context.Context, name string, call tools.Call) (Decision, error)
	Execute(ctx context.Context, name string, call tools.Call) (tools.Result, error)
	Finish(ctx context.Context, name string, call tools.Call, result tools.Result, message transcript.Message) error
}

// Pending is a tool call that waits for a user decision.
type Pending struct {
	RunID      string
	SessionID  string
	ToolCallID string
	ToolName   string
	Request    tools.ApprovalRequest
}

// Approvals records approval requests and reports whether any are still open.
type Approvals interface {
	Request(ctx context.Context, p Pending) error
	Pending(ctx context.Context, runID string) (bool, error)
}

// InputKind selects what Inbox.Peek returns.
type InputKind string

const (
	InputSteer   InputKind = "steer"
	InputDecided InputKind = "decided"
)

// Input arrives from outside the engine: steer Text, or a decided approval;
// Denied approvals carry the user's Reason.
type Input struct {
	Kind       InputKind
	ID         string
	Text       string
	ToolCallID string
	ToolName   string
	WorkingDir string
	Args       json.RawMessage
	Denied     bool
	Reason     string
}

// Inbox delivers outside input. Peek never consumes: steers stay pending until the
// engine consumes their IDs, and InputDecided returns decided approvals whose call
// has no result yet.
type Inbox interface {
	Peek(ctx context.Context, runID string, kind InputKind) ([]Input, error)
	Consume(ctx context.Context, runID string, ids []string) error
	Canceled(ctx context.Context, runID string) (bool, error)
}

// EventKind names what Sink receives.
type EventKind string

const (
	EventMessageCreated EventKind = "message.created"
	EventMessageUpdated EventKind = "message.updated"
	EventToolRequested  EventKind = "tool.requested"
	EventToolFinished   EventKind = "tool.finished"
)

// Event is live progress for clients.
type Event struct {
	Kind            EventKind
	SessionID       string
	RunID           string
	Message         transcript.Message
	ToolCallID      string
	ToolName        string
	ResultMessageID string
	Result          tools.Result
}

// Sink fans engine events out to clients.
type Sink interface {
	Emit(event Event)
}

type Todos interface {
	// Open lists the unfinished items of the session's todo list when a run of
	// chain (a run and the runs it continues) wrote it, and none otherwise.
	Open(ctx context.Context, sessionID string, chain []string) ([]todo.Item, error)
}

// Prompts supplies the core-owned parts of every request: the system prompt and
// custom instructions, fixed for a run, and the changing state the engine sends
// as a context note whenever it changes.
type Prompts interface {
	System(ctx context.Context, history []transcript.Message) (system, custom string)
	Context(ctx context.Context) string
}
