package externalagents

import (
	"context"
	"time"
)

type Availability struct {
	Installed bool
	Enabled   bool
	Mode      string
	Path      string
	Version   string
	Detail    string
}

type Descriptor struct {
	ID          string
	Aliases     []string
	DisplayName string
	Installed   bool
	Enabled     bool
	Mode        string
	Path        string
	Version     string
	Detail      string
}

type RuntimeAgent interface {
	ID() string
	DisplayName() string
	// Models are the models the agent suggests to pick from, default first.
	Models(ctx context.Context) []string
	Available(ctx context.Context) Availability
	StartSession(ctx context.Context, req StartSessionRequest) (ExternalSession, error)
	ResumeSession(ctx context.Context, session ExternalSession) (ExternalSession, error)
	Send(ctx context.Context, session ExternalSession, input Input) (<-chan Event, error)
	Interrupt(ctx context.Context, session ExternalSession) error
	Close() error
}

// Permissions are what the agent may do unasked; each adapter maps them to
// its own terms.
type Permissions struct {
	// Mode is a permission mode (permission.ModeDefault, ...).
	Mode string
	// Readonly refuses every change without asking, whatever the mode.
	Readonly bool
}

type StartSessionRequest struct {
	CWD         string
	Model       string
	Permissions Permissions
}

type ExternalSession struct {
	AgentID           string
	ExternalThreadID  string
	ExternalSessionID string
	CWD               string
	Model             string
	// Permissions are the session's now; core fills them before every turn.
	Permissions Permissions
	Metadata    map[string]any
}

type Input struct {
	Text string
}

type EventKind string

const (
	EventTurnStarted     EventKind = "turn.started"
	EventHeartbeat       EventKind = "turn.heartbeat"
	EventMessageDelta    EventKind = "message.delta"
	EventReasoningDelta  EventKind = "reasoning.delta"
	EventToolStarted     EventKind = "tool.started"
	EventToolOutputDelta EventKind = "tool.output.delta"
	EventToolCompleted   EventKind = "tool.completed"
	EventDiffUpdated     EventKind = "diff.updated"
	EventTurnCompleted   EventKind = "turn.completed"
	EventTurnFailed      EventKind = "turn.failed"
)

type Event struct {
	Kind              EventKind `json:"kind"`
	AgentID           string    `json:"agent_id"`
	ExternalThreadID  string    `json:"external_thread_id,omitempty"`
	ExternalSessionID string    `json:"external_session_id,omitempty"`
	ExternalTurnID    string    `json:"external_turn_id,omitempty"`
	ItemID            string    `json:"item_id,omitempty"`
	ToolName          string    `json:"tool_name,omitempty"`
	ToolInput         string    `json:"tool_input,omitempty"`
	Text              string    `json:"text,omitempty"`
	Error             string    `json:"error,omitempty"`
	At                time.Time `json:"at"`
}

type SessionAttachment struct {
	SessionID         string
	AgentID           string
	ExternalThreadID  string
	ExternalSessionID string
	CWD               string
	Model             string
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
	}
}
