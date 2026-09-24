// Package agenttest provides a scripted model and in-memory fakes for the agent ports.
package agenttest

import (
	"context"
	"errors"
	"fmt"
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

// Journal keeps the transcript in memory and counts streaming writes.
type Journal struct {
	Messages []transcript.Message
	States   []agent.State
	Steps    []agent.Step
	Begins   int
	Streams  int
	Finishes int
	LoadErr  error
	seq      int64
}

// Seed stores messages as if they were written before the run.
func (j *Journal) Seed(messages ...transcript.Message) {
	for _, message := range messages {
		j.insert(message)
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

func (j *Journal) Load(context.Context, string) (agent.Window, error) {
	if j.LoadErr != nil {
		return agent.Window{}, j.LoadErr
	}
	return agent.Window{Messages: append([]transcript.Message(nil), j.Messages...)}, nil
}

func (j *Journal) Append(_ context.Context, msg transcript.Message) (int64, error) {
	return j.insert(msg), nil
}

func (j *Journal) BeginStreaming(_ context.Context, msg transcript.Message) (int64, error) {
	j.Begins++
	return j.insert(msg), nil
}

func (j *Journal) Stream(_ context.Context, msg transcript.Message) error {
	j.Streams++
	return j.replace(msg)
}

func (j *Journal) FinishStreaming(_ context.Context, msg transcript.Message) error {
	j.Finishes++
	return j.replace(msg)
}

func (j *Journal) Checkpoint(_ context.Context, state agent.State) error {
	j.States = append(j.States, state)
	return nil
}

func (j *Journal) RecordStep(_ context.Context, step agent.Step) error {
	j.Steps = append(j.Steps, step)
	return nil
}

func (j *Journal) insert(msg transcript.Message) int64 {
	j.seq++
	msg.Seq = j.seq
	j.Messages = append(j.Messages, msg)
	return j.seq
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
type Tools struct {
	Funcs    map[string]ToolFunc
	Calls    []tools.Call
	Finished []string
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
	t.Calls = append(t.Calls, call)
	return t.Funcs[name](call), nil
}

func (t *Tools) Finish(_ context.Context, _ string, call tools.Call, _ tools.Result, _ transcript.Message) error {
	t.Finished = append(t.Finished, call.ToolCallID)
	return nil
}

// Approvals records requests; Grant, when set, resolves each request at once.
type Approvals struct {
	Requests []agent.Pending
	Open     bool
	Grant    func(agent.Pending)
}

func (a *Approvals) Request(_ context.Context, p agent.Pending) error {
	a.Requests = append(a.Requests, p)
	if a.Grant != nil {
		a.Grant(p)
		return nil
	}
	a.Open = true
	return nil
}

func (a *Approvals) Pending(context.Context, string) (bool, error) {
	return a.Open, nil
}

// Inbox hands out steers once and granted approvals on every drain.
type Inbox struct {
	Steers   []string
	Approved []agent.Input
	Cancel   bool
}

func (in *Inbox) Drain(_ context.Context, _ string, kind agent.InputKind) ([]agent.Input, error) {
	switch kind {
	case agent.InputSteer:
		out := make([]agent.Input, 0, len(in.Steers))
		for _, text := range in.Steers {
			out = append(out, agent.Input{Kind: agent.InputSteer, Text: text})
		}
		in.Steers = nil
		return out, nil
	case agent.InputApproved:
		return in.Approved, nil
	default:
		return nil, fmt.Errorf("agenttest: unknown input kind %q", kind)
	}
}

func (in *Inbox) Canceled(context.Context, string) (bool, error) {
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

// Prompts returns Text as system prompt, extended by the compact summary it is given.
type Prompts struct {
	Text         string
	BaseTokens   int
	WindowTokens int
}

func (p *Prompts) System(_ context.Context, summary string, _ []transcript.Message) (string, string) {
	if summary == "" {
		return p.Text, ""
	}
	return p.Text + "\n\nSession context summary:\n" + summary, ""
}

func (p *Prompts) PlanSnapshot(context.Context) string {
	return ""
}

func (p *Prompts) Budget(context.Context) (int, int, error) {
	return p.BaseTokens, p.WindowTokens, nil
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

// Engine returns an engine over the fixture's fakes with a fixed clock and sequential IDs.
func (f *Fixture) Engine() *agent.Engine {
	return agent.New(agent.Config{
		Journal:   f.Journal,
		Tools:     f.Tools,
		Approvals: f.Approvals,
		Inbox:     f.Inbox,
		Sink:      f.Sink,
		Prompts:   f.Prompts,
		Now:       func() time.Time { return f.Clock },
		NewID: func(prefix string) string {
			f.ids++
			return fmt.Sprintf("%s_%d", prefix, f.ids)
		},
	})
}

// Task returns the fixture's run for model.
func (f *Fixture) Task(model agent.Model) agent.Task {
	return agent.Task{RunID: RunID, SessionID: SessionID, WorkingDir: "/work", Model: model}
}
