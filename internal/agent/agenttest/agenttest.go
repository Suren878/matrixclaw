// Package agenttest provides a scripted model and in-memory fakes for the agent ports.
package agenttest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
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

// Tools authorizes registered names only and records executed and finished calls.
// OnExecute and OnFinish run first and fail the call with their error.
type Tools struct {
	Funcs     map[string]ToolFunc
	Calls     []tools.Call
	Finished  []string
	OnExecute func(name string, call tools.Call) error
	OnFinish  func(name string, call tools.Call) error
}

func (t *Tools) Specs(context.Context) []tools.Spec {
	names := make([]string, 0, len(t.Funcs))
	for name := range t.Funcs {
		names = append(names, name)
	}
	sort.Strings(names)
	specs := make([]tools.Spec, 0, len(names))
	for _, name := range names {
		specs = append(specs, tools.Spec{ID: name, Name: name, Description: name})
	}
	return specs
}

func (t *Tools) Authorize(_ context.Context, name string, _ tools.Call) (agent.Decision, error) {
	if _, ok := t.Funcs[name]; !ok {
		return agent.Decision{Reason: fmt.Sprintf("invalid input: unknown tool %q", name)}, nil
	}
	return agent.Decision{Allowed: true}, nil
}

func (t *Tools) Execute(_ context.Context, name string, call tools.Call) (tools.Result, error) {
	if t.OnExecute != nil {
		if err := t.OnExecute(name, call); err != nil {
			return tools.Result{}, err
		}
	}
	t.Calls = append(t.Calls, call)
	return t.Funcs[name](call), nil
}

func (t *Tools) Finish(_ context.Context, name string, call tools.Call, _ tools.Result, _ transcript.Message) error {
	if t.OnFinish != nil {
		if err := t.OnFinish(name, call); err != nil {
			return err
		}
	}
	t.Finished = append(t.Finished, call.ToolCallID)
	return nil
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

// Inbox hands out pending steers, whose IDs are their text, until they are consumed,
// and granted approvals on every peek. Like the store, it fails on a stopped context.
type Inbox struct {
	Steers   []string
	Approved []agent.Input
	Cancel   bool
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
	case agent.InputApproved:
		return in.Approved, nil
	default:
		return nil, fmt.Errorf("agenttest: unknown input kind %q", kind)
	}
}

func (in *Inbox) Consume(ctx context.Context, _ string, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	in.Steers = slices.DeleteFunc(in.Steers, func(text string) bool { return slices.Contains(ids, text) })
	return nil
}

func (in *Inbox) Canceled(ctx context.Context, _ string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return in.Cancel, nil
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

// Prompts returns Text as the system prompt.
type Prompts struct {
	Text string
}

func (p *Prompts) System(context.Context, []transcript.Message) (string, string) {
	return p.Text, ""
}

// Fixture wires an engine to fresh fakes around one seeded user message.
type Fixture struct {
	Journal   *Journal
	Tools     *Tools
	Approvals *Approvals
	Inbox     *Inbox
	Sink      *Sink
	Prompts   *Prompts
	Clock     time.Time
	// Window is the model window of the fixture's task; 0 means unknown.
	Window int
	// Slept records the engine's waits, which return at once unless RealSleep is set.
	Slept     []time.Duration
	RealSleep bool
	ids       int
}

// NewFixture returns fakes with the run's user message already journaled.
func NewFixture() *Fixture {
	clock := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	f := &Fixture{
		Journal:   &Journal{},
		Tools:     &Tools{Funcs: map[string]ToolFunc{}},
		Approvals: &Approvals{},
		Inbox:     &Inbox{},
		Sink:      &Sink{},
		Prompts:   &Prompts{Text: "system"},
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
	return agent.New(agent.Config{
		Journal:   f.Journal,
		Tools:     f.Tools,
		Approvals: f.Approvals,
		Inbox:     f.Inbox,
		Sink:      f.Sink,
		Prompts:   f.Prompts,
		Now:       func() time.Time { return f.Clock },
		Sleep:     sleep,
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
