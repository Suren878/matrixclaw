package externalagents

import (
	"context"
	"time"
)

type Availability struct {
	Installed bool
	Enabled   bool
	AuthState string
	Mode      string
	Detail    string
}

type Descriptor struct {
	ID          string
	Aliases     []string
	DisplayName string
	Installed   bool
	Enabled     bool
	AuthState   string
	Mode        string
	Detail      string
}

type Agent interface {
	ID() string
	DisplayName() string
	Available(ctx context.Context) Availability
}

type AliasProvider interface {
	Aliases() []string
}

type RuntimeAgent interface {
	Agent
	StartSession(ctx context.Context, req StartSessionRequest) (ExternalSession, error)
	ResumeSession(ctx context.Context, session ExternalSession) (ExternalSession, error)
	Send(ctx context.Context, session ExternalSession, input Input) (<-chan Event, error)
	Interrupt(ctx context.Context, session ExternalSession) error
	Close() error
}

type StartSessionRequest struct {
	CWD                   string
	Model                 string
	ApprovalPolicy        string
	Sandbox               string
	BaseInstructions      string
	DeveloperInstructions string
	Metadata              map[string]any
}

type ExternalSession struct {
	AgentID           string
	ExternalThreadID  string
	ExternalSessionID string
	CWD               string
	Model             string
	ApprovalPolicy    string
	Sandbox           string
	Metadata          map[string]any
}

type Input struct {
	Text string
}

type EventKind string

const (
	EventTurnStarted     EventKind = "turn.started"
	EventMessageDelta    EventKind = "message.delta"
	EventReasoningDelta  EventKind = "reasoning.delta"
	EventToolStarted     EventKind = "tool.started"
	EventToolOutputDelta EventKind = "tool.output.delta"
	EventDiffUpdated     EventKind = "diff.updated"
	EventTurnCompleted   EventKind = "turn.completed"
	EventTurnFailed      EventKind = "turn.failed"
)

type Event struct {
	Kind             EventKind `json:"kind"`
	AgentID          string    `json:"agent_id"`
	ExternalThreadID string    `json:"external_thread_id,omitempty"`
	ExternalTurnID   string    `json:"external_turn_id,omitempty"`
	ItemID           string    `json:"item_id,omitempty"`
	Text             string    `json:"text,omitempty"`
	Error            string    `json:"error,omitempty"`
	RawMethod        string    `json:"raw_method,omitempty"`
	Raw              []byte    `json:"raw,omitempty"`
	At               time.Time `json:"at"`
}

type SessionAttachment struct {
	SessionID         string
	AgentID           string
	ExternalThreadID  string
	ExternalSessionID string
	CWD               string
	Model             string
	ApprovalPolicy    string
	Sandbox           string
	MetadataJSON      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (a SessionAttachment) ExternalSession() ExternalSession {
	return ExternalSession{
		AgentID:           a.AgentID,
		ExternalThreadID:  a.ExternalThreadID,
		ExternalSessionID: a.ExternalSessionID,
		CWD:               a.CWD,
		Model:             a.Model,
		ApprovalPolicy:    a.ApprovalPolicy,
		Sandbox:           a.Sandbox,
	}
}
