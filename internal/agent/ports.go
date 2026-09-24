// Package agent runs the native agent loop over narrow ports implemented by core.
package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// Model generates one assistant turn; providers.Runtime satisfies it.
type Model interface {
	Generate(ctx context.Context, req providers.Request) (providers.Response, error)
}

// Window is the transcript a run starts from, ordered by seq.
type Window struct {
	Messages []transcript.Message
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

// Phase is the durable execution boundary crash recovery resumes from.
type Phase string

const (
	PhaseModel Phase = "model"
	PhaseTool  Phase = "tool"
)

// State is the checkpoint written before each model call and tool execution.
type State struct {
	RunID      string
	Phase      Phase
	ToolCallID string
	ToolName   string
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
type Decision struct {
	Allowed bool
	Reason  string
}

// Tools lists, authorizes, executes and finalizes the tools of one run. Execute
// errors are fatal; tool failures come back as IsError results. Finish runs after
// the result message is written.
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
	InputSteer    InputKind = "steer"
	InputApproved InputKind = "approved"
)

// Input arrives from outside the engine: steer Text, or a granted approval.
type Input struct {
	Kind       InputKind
	ID         string
	Text       string
	ToolCallID string
	ToolName   string
	WorkingDir string
	Args       json.RawMessage
}

// Inbox delivers outside input. Peek never consumes: steers stay pending until the
// engine consumes their IDs, and InputApproved returns granted approvals whose call
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

// Prompts supplies the core-owned parts of every request: the system prompt and
// custom instructions, the plan snapshot for summaries, and the context budget.
type Prompts interface {
	System(ctx context.Context, summary string, history []transcript.Message) (system, custom string)
	PlanSnapshot(ctx context.Context) string
	Budget(ctx context.Context) (baseTokens, windowTokens int, err error)
}
