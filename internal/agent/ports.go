// Package agent runs the native agent loop over narrow ports implemented by core.
package agent

import (
	"context"
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
// RecordStep is usage accounting: the adapter logs a failed write, the run goes on.
type Journal interface {
	Load(ctx context.Context, sessionID string) (Window, error)
	Append(ctx context.Context, msg transcript.Message) (seq int64, err error)
	BeginStreaming(ctx context.Context, msg transcript.Message) (seq int64, err error)
	Stream(ctx context.Context, msg transcript.Message) error
	FinishStreaming(ctx context.Context, msg transcript.Message) error
	Checkpoint(ctx context.Context, state State) error
	RecordStep(ctx context.Context, step Step)
}

// Phase is the durable execution boundary a run last reached.
type Phase string

const (
	PhaseModel     Phase = "model"
	PhaseToolBatch Phase = "tool_batch"
)

// State is the checkpoint the engine writes before each model call, when it
// starts calls of a tool batch and when the run parks; its RunState lets a parked
// or restarted run keep its budget.
type State struct {
	RunID    string
	Phase    Phase
	Batch    *ToolBatch
	RunState RunState
}

// ToolBatch names the calls of the batch being run, in call order, and those
// of them held back (deferred) when the checkpoint was written; crash recovery
// defers those again.
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

// Admission is Authorize's answer for one call; Reason goes back to the model
// when it may not run. A Barrier call that waits for approval holds back the
// calls after it in its batch. Calls sharing a non-empty Key run one at a time,
// across runs too. A Delegated call runs another agent, whose time is not this
// run's. Grant is opaque to the engine, which hands it to Execute unchanged.
type Admission struct {
	Allowed   bool
	Reason    string
	Barrier   bool
	Key       string
	Delegated bool
	Grant     Grant
}

// Grant is the Tools port's own record of a call it authorized.
type Grant any

// Tools lists, authorizes and executes the tools of one run. Execute runs a
// call Authorize allowed and returns either a result or, for a call that waits
// for the user, its approval request. Execute errors are fatal; tool failures
// come back as error results. Execute runs on a goroutine of its own,
// concurrently with other calls of the batch; the other methods run on the
// engine goroutine. Recovery says how a resumed run settles a call its
// interrupted execution left without a result.
type Tools interface {
	Specs(ctx context.Context) []tools.Spec
	Authorize(ctx context.Context, name string, call tools.Call) (Admission, error)
	Execute(ctx context.Context, grant Grant) (tools.Result, *tools.ApprovalRequest, error)
	Recovery(ctx context.Context, name string, call tools.Call) (Recovery, error)
}

// Pending is a tool call that waits for a user decision.
type Pending struct {
	RunID      string
	SessionID  string
	ToolCallID string
	ToolName   string
	Request    tools.ApprovalRequest
}

// Approvals records approval requests.
type Approvals interface {
	Request(ctx context.Context, p Pending) error
}

// Steer is input the user sent the running run: Text with the Images sent with it.
type Steer struct {
	ID     string
	Text   string
	Images []transcript.MessagePart
}

// TaskEvent says a background task ended; the model reads Text.
type TaskEvent struct {
	TaskID string
	Text   string
}

// Decision is where the newest approval of a call stands: Pending until the
// user answers, then granted or Denied with the user's Reason.
type Decision struct {
	ToolCallID string
	Pending    bool
	Denied     bool
	Reason     string
}

// Inbox delivers outside input to one run. Steers and events stay pending
// until the engine consumes them; Decisions lists the newest approval of each
// call that asked, oldest first. Finished reports whether any of the background
// tasks ended, even if its event went elsewhere.
type Inbox interface {
	Steers(ctx context.Context) ([]Steer, error)
	ConsumeSteers(ctx context.Context, ids []string) error
	Events(ctx context.Context) ([]TaskEvent, error)
	ConsumeEvents(ctx context.Context, taskIDs []string) error
	Decisions(ctx context.Context) ([]Decision, error)
	Finished(ctx context.Context, taskIDs []string) (bool, error)
}

// EventKind names what Sink receives.
type EventKind string

const (
	EventMessageCreated EventKind = "message.created"
	EventMessageUpdated EventKind = "message.updated"
	EventToolRequested  EventKind = "tool.requested"
	EventToolFinished   EventKind = "tool.finished"
	// EventContextMeasured carries ContextTokens and WindowTokens after a step.
	EventContextMeasured EventKind = "context.measured"
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
	ContextTokens   int
	WindowTokens    int
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

// Hooks runs the user's hooks at the engine's seams. SessionStart returns
// context for the model, Stop why a run that would end must go on ("" lets it
// end; active says a Stop hook already kept it going), PreCompact only observes.
type Hooks interface {
	SessionStart(ctx context.Context, source string) string
	Stop(ctx context.Context, reply string, active bool) string
	PreCompact(ctx context.Context, trigger string)
}

// Verifier checks the session's goal when a run would end with a reply.
// Hold keeps the run going with Note (a failed check); otherwise a Note (a
// passed check, a goal marked blocked) is journaled for the user and the run
// ends. A session without an active goal or check gets the zero Verdict.
type Verifier interface {
	Verify(ctx context.Context, sessionID, runID string) (Verdict, error)
}

// Verdict is what a goal check found.
type Verdict struct {
	Hold bool
	Note string
}
