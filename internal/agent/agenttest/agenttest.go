// Package agenttest provides a scripted model and in-memory fakes for the agent ports.
package agenttest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const (
	RunID     = "run_1"
	SessionID = "session_1"
)

// Turn is one scripted answer; Stream deltas are streamed before Response is returned.
type Turn struct {
	Stream   []string
	Response providers.Response
	Err      error
}

// ScriptedModel answers requests with its turns in order and records every request.
type ScriptedModel struct {
	turns    []Turn
	requests []providers.Request
}

// NewScriptedModel returns a model that plays turns in order.
func NewScriptedModel(turns ...Turn) *ScriptedModel {
	return &ScriptedModel{turns: turns}
}

func (m *ScriptedModel) Generate(ctx context.Context, req providers.Request) (providers.Response, error) {
	m.requests = append(m.requests, req)
	if len(m.turns) == 0 {
		return providers.Response{}, errors.New("agenttest: no scripted turn left")
	}
	turn := m.turns[0]
	m.turns = m.turns[1:]
	for _, delta := range turn.Stream {
		if err := providers.StreamText(ctx, delta); err != nil {
			return providers.Response{}, err
		}
	}
	return turn.Response, turn.Err
}

// Requests returns every request the model received.
func (m *ScriptedModel) Requests() []providers.Request {
	return m.requests
}

// ModelFunc adapts a function to agent.Model.
type ModelFunc func(ctx context.Context, req providers.Request) (providers.Response, error)

func (f ModelFunc) Generate(ctx context.Context, req providers.Request) (providers.Response, error) {
	return f(ctx, req)
}

// Journal keeps the transcript in memory, loads it like core — the newest boundary
// and the messages after it — and counts streaming writes. Like the store it rejects
// duplicate IDs and fails writes on a stopped context; RecordStep never fails, as core
// only logs a failed step write. OnAppend and OnCheckpoint run first and can fail a write.
type Journal struct {
	Messages     []transcript.Message
	States       []agent.State
	Steps        []agent.Step
	Begins       int
	Streams      int
	Finishes     int
	LoadErr      error
	OnAppend     func(transcript.Message) error
	OnCheckpoint func(agent.State) error
	seq          int64
}

// Seed stores messages as if they were written before the run.
func (j *Journal) Seed(messages ...transcript.Message) {
	for _, message := range messages {
		if _, err := j.insert(message); err != nil {
			panic(err)
		}
	}
}

// Message returns the stored message with the ID.
func (j *Journal) Message(id string) (transcript.Message, bool) {
	for _, message := range j.Messages {
		if message.ID == id {
			return message, true
		}
	}
	return transcript.Message{}, false
}

// Result returns the stored result message of a tool call.
func (j *Journal) Result(callID string) (transcript.Message, bool) {
	for _, message := range j.Messages {
		for _, part := range message.Parts {
			if part.ToolResult != nil && part.ToolResult.ToolCallID == callID {
				return message, true
			}
		}
	}
	return transcript.Message{}, false
}

func (j *Journal) Load(ctx context.Context, _ string) (agent.Window, error) {
	if err := ctx.Err(); err != nil {
		return agent.Window{}, err
	}
	if j.LoadErr != nil {
		return agent.Window{}, j.LoadErr
	}
	var window agent.Window
	for i := len(j.Messages) - 1; i >= 0; i-- {
		if j.Messages[i].Compaction != nil {
			boundary := j.Messages[i]
			window.Boundary = &boundary
			break
		}
	}
	var covers int64
	if compaction := window.Compaction(); compaction != nil {
		covers = compaction.CoversThroughSeq
	}
	for _, message := range j.Messages {
		if message.Compaction == nil && message.Seq > covers {
			window.Messages = append(window.Messages, message)
		}
	}
	return window, nil
}

func (j *Journal) Append(ctx context.Context, msg transcript.Message) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if j.OnAppend != nil {
		if err := j.OnAppend(msg); err != nil {
			return 0, err
		}
	}
	return j.insert(msg)
}

func (j *Journal) BeginStreaming(ctx context.Context, msg transcript.Message) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	j.Begins++
	return j.insert(msg)
}

func (j *Journal) Stream(ctx context.Context, msg transcript.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	j.Streams++
	return j.replace(msg)
}

func (j *Journal) FinishStreaming(ctx context.Context, msg transcript.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	j.Finishes++
	return j.replace(msg)
}

func (j *Journal) Checkpoint(ctx context.Context, state agent.State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if j.OnCheckpoint != nil {
		if err := j.OnCheckpoint(state); err != nil {
			return err
		}
	}
	j.States = append(j.States, state)
	return nil
}

func (j *Journal) RecordStep(_ context.Context, step agent.Step) error {
	j.Steps = append(j.Steps, step)
	return nil
}

func (j *Journal) insert(msg transcript.Message) (int64, error) {
	if _, exists := j.Message(msg.ID); exists {
		return 0, fmt.Errorf("agenttest: message %q already exists", msg.ID)
	}
	j.seq++
	msg.Seq = j.seq
	j.Messages = append(j.Messages, msg)
	return j.seq, nil
}

func (j *Journal) replace(msg transcript.Message) error {
	for i := range j.Messages {
		if j.Messages[i].ID == msg.ID {
			msg.Seq = j.Messages[i].Seq
			j.Messages[i] = msg
			return nil
		}
	}
	return fmt.Errorf("agenttest: message %q not found", msg.ID)
}

// ToolFunc executes one fake tool call.
type ToolFunc func(call tools.Call) tools.Result

// Tools authorizes registered names only (Mutating ones are barriers until
// granted, Keys and Delegated fill the decision) and records executed calls in
// start order (read Calls after Run returns). A call of an Asks tool returns an
// approval request until Inbox holds its grant, as core does. OnExecute runs
// first, on the call's goroutine, and fails the call with its error.
type Tools struct {
	Funcs     map[string]ToolFunc
	Mutating  map[string]bool
	Asks      map[string]bool
	Keys      map[string]string
	Delegated map[string]bool
	Inbox     *Inbox
	Calls     []tools.Call
	OnExecute func(ctx context.Context, name string, call tools.Call) error
	// SpecReads counts tool listings.
	SpecReads int
	mu        sync.Mutex
}

func (t *Tools) Specs(context.Context) []tools.Spec {
	t.SpecReads++
	names := make([]string, 0, len(t.Funcs))
	for name := range t.Funcs {
		names = append(names, name)
	}
	sort.Strings(names)
	specs := make([]tools.Spec, 0, len(names))
	for _, name := range names {
		specs = append(specs, tools.Spec{ID: name, Description: name})
	}
	return specs
}

func (t *Tools) Authorize(_ context.Context, name string, call tools.Call) (agent.Decision, error) {
	if _, ok := t.Funcs[name]; !ok {
		return agent.Decision{Reason: fmt.Sprintf("invalid input: unknown tool %q", name)}, nil
	}
	return agent.Decision{Allowed: true, Barrier: t.Mutating[name] && !t.granted(call.ToolCallID), Key: t.Keys[name], Delegated: t.Delegated[name]}, nil
}

func (t *Tools) Execute(ctx context.Context, name string, call tools.Call) (tools.Result, *tools.ApprovalRequest, error) {
	if t.OnExecute != nil {
		if err := t.OnExecute(ctx, name, call); err != nil {
			return tools.Result{}, nil, err
		}
	}
	t.mu.Lock()
	t.Calls = append(t.Calls, call)
	t.mu.Unlock()
	if t.Asks[name] && !t.granted(call.ToolCallID) {
		return tools.Result{}, &tools.ApprovalRequest{Description: name}, nil
	}
	return t.Funcs[name](call), nil, nil
}

// granted reports whether Inbox holds a grant for the call.
func (t *Tools) granted(callID string) bool {
	if t.Inbox == nil {
		return false
	}
	return slices.ContainsFunc(t.Inbox.Decided, func(input agent.Input) bool { return input.ToolCallID == callID && !input.Denied })
}

// Approvals records requests; Grant, when set, resolves each request at once.
// OnRequest runs first and fails the request with its error. Like the store, both
// methods fail on a stopped context.
type Approvals struct {
	Requests  []agent.Pending
	Open      bool
	Grant     func(agent.Pending)
	OnRequest func(agent.Pending) error
}

func (a *Approvals) Request(ctx context.Context, p agent.Pending) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.OnRequest != nil {
		if err := a.OnRequest(p); err != nil {
			return err
		}
	}
	a.Requests = append(a.Requests, p)
	if a.Grant != nil {
		a.Grant(p)
		return nil
	}
	a.Open = true
	return nil
}

func (a *Approvals) Pending(ctx context.Context, _ string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return a.Open, nil
}

// Inbox hands out pending steers, whose IDs are their text, and events until
// they are consumed, and decided approvals on every peek; Ended lists tasks that
// finished without an event. Like the store, it fails on a stopped context.
type Inbox struct {
	Steers  []string
	Decided []agent.Input
	Events  []agent.Input
	Ended   []string
}

func (in *Inbox) Peek(ctx context.Context, _ string, kind agent.InputKind) ([]agent.Input, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch kind {
	case agent.InputSteer:
		out := make([]agent.Input, 0, len(in.Steers))
		for _, text := range in.Steers {
			out = append(out, agent.Input{Kind: agent.InputSteer, ID: text, Text: text})
		}
		return out, nil
	case agent.InputDecided:
		return in.Decided, nil
	case agent.InputEvent:
		return slices.Clone(in.Events), nil
	default:
		return nil, fmt.Errorf("agenttest: unknown input kind %q", kind)
	}
}

func (in *Inbox) Consume(ctx context.Context, _ string, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	in.Steers = slices.DeleteFunc(in.Steers, func(text string) bool { return slices.Contains(ids, text) })
	in.Events = slices.DeleteFunc(in.Events, func(event agent.Input) bool { return slices.Contains(ids, event.ID) })
	return nil
}

func (in *Inbox) Finished(ctx context.Context, taskIDs []string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return slices.ContainsFunc(taskIDs, func(id string) bool { return slices.Contains(in.Ended, id) }), nil
}

// Sink records every event.
type Sink struct {
	Events []agent.Event
}

func (s *Sink) Emit(event agent.Event) {
	s.Events = append(s.Events, event)
}

// Kinds lists the recorded event kinds in order.
func (s *Sink) Kinds() []agent.EventKind {
	kinds := make([]agent.EventKind, 0, len(s.Events))
	for _, event := range s.Events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

// Prompts returns Text as the system prompt and ContextText as the changing
// context; SystemCalls counts system prompt builds.
type Prompts struct {
	Text        string
	Custom      string
	ContextText string
	SystemCalls int
}

func (p *Prompts) System(context.Context, []transcript.Message) (string, string) {
	p.SystemCalls++
	return p.Text, p.Custom
}

func (p *Prompts) Context(context.Context) string {
	return p.ContextText
}

// it was asked about. Like the store, it fails on a stopped context.
type Todos struct {
	Items  []todo.Item
	Chains [][]string
}

func (t *Todos) Open(ctx context.Context, _ string, chain []string) ([]todo.Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t.Chains = append(t.Chains, chain)
	return todo.Open(t.Items), nil
}

// Fixture wires an engine to fresh fakes around one seeded user message.
type Fixture struct {
	Journal   *Journal
	Tools     *Tools
	Approvals *Approvals
	Inbox     *Inbox
	Sink      *Sink
	Prompts   *Prompts
	Todos     *Todos
	Clock     time.Time
	// Now, when set, is the engine's clock instead of Clock, for tools that
	// move time from their own goroutines.
	Now func() time.Time
	// Window is the model window of the fixture's task; 0 means unknown.
	Window int
	// Slept records the engine's waits, which return at once unless RealSleep is set.
	Slept     []time.Duration
	RealSleep bool
	// ModelSlots and Locks are shared by the engines of fixtures that should
	// compete for model requests and concurrency keys.
	ModelSlots *toolsched.Semaphore
	Locks      *toolsched.Locks
	ids        int
}

// NewFixture returns fakes with the run's user message already journaled.
func NewFixture() *Fixture {
	clock := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	inbox := &Inbox{}
	f := &Fixture{
		Journal:   &Journal{},
		Tools:     &Tools{Funcs: map[string]ToolFunc{}, Asks: map[string]bool{}, Inbox: inbox},
		Approvals: &Approvals{},
		Inbox:     inbox,
		Sink:      &Sink{},
		Prompts:   &Prompts{Text: "system"},
		Todos:     &Todos{},
		Clock:     clock,
	}
	f.Journal.Seed(transcript.Message{
		ID: "msg_user", SessionID: SessionID, RunID: RunID, Role: transcript.MessageRoleUser,
		Content: "do the task", Parts: transcript.NormalizeMessageParts("do the task", nil),
		CreatedAt: clock, UpdatedAt: clock,
	})
	return f
}

// WithHistory puts messages before the run's user message, as an earlier part
// of the session.
func (f *Fixture) WithHistory(messages ...transcript.Message) {
	user := f.Journal.Messages[len(f.Journal.Messages)-1]
	f.Journal.Messages, f.Journal.seq = nil, 0
	f.Journal.Seed(append(messages, user)...)
}

// Engine returns an engine over the fixture's fakes with a fixed clock and sequential IDs.
func (f *Fixture) Engine() *agent.Engine {
	sleep := func(ctx context.Context, d time.Duration) error {
		f.Slept = append(f.Slept, d)
		return ctx.Err()
	}
	if f.RealSleep {
		sleep = nil
	}
	now := f.Now
	if now == nil {
		now = func() time.Time { return f.Clock }
	}
	return agent.New(agent.Config{
		Journal:    f.Journal,
		Tools:      f.Tools,
		Approvals:  f.Approvals,
		Inbox:      f.Inbox,
		Sink:       f.Sink,
		Prompts:    f.Prompts,
		Todos:      f.Todos,
		Now:        now,
		Sleep:      sleep,
		ModelSlots: f.ModelSlots,
		Locks:      f.Locks,
		NewID: func(prefix string) string {
			f.ids++
			return fmt.Sprintf("%s_%d", prefix, f.ids)
		},
	})
}

// Task returns the fixture's run for model.
func (f *Fixture) Task(model agent.Model) agent.Task {
	return agent.Task{RunID: RunID, SessionID: SessionID, WorkingDir: "/work", Model: model, WindowTokens: f.Window}
}
