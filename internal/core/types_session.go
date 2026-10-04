package core

import (
	"time"

	"github.com/Suren878/matrixclaw/internal/permission"
)

type SessionStatus string

const (
	SessionStatusActive   SessionStatus = "active"
	SessionStatusArchived SessionStatus = "archived"
)

type SessionKind string

const (
	SessionKindAssistant     SessionKind = "assistant"
	SessionKindExternalAgent SessionKind = "external_agent"
)

type SessionRuntime string

const (
	SessionRuntimeMatrixClaw    SessionRuntime = "matrixclaw"
	SessionRuntimeExternalAgent SessionRuntime = "external_agent"
)

type Session struct {
	ID                string         `json:"id"`
	Title             string         `json:"title"`
	Kind              SessionKind    `json:"kind"`
	RuntimeID         SessionRuntime `json:"runtime_id,omitempty"`
	ParentSessionID   string         `json:"parent_session_id,omitempty"`
	Hidden            bool           `json:"hidden,omitempty"`
	WorkingDir        string         `json:"working_dir,omitempty"`
	ProviderID        string         `json:"provider_id,omitempty"`
	ModelID           string         `json:"model_id,omitempty"`
	ExternalAgentID   string         `json:"external_agent_id,omitempty"`
	ExternalAgentName string         `json:"external_agent_name,omitempty"`
	PermissionMode    PermissionMode `json:"permission_mode,omitempty"`
	Status            SessionStatus  `json:"status"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// RunsUnattended reports whether a session's tools run without asking: an
// external agent session or one in full_auto.
func RunsUnattended(session Session) bool {
	return CapabilitiesForSession(session).ExternalAgent || NormalizePermissionMode(string(session.PermissionMode)) == PermissionModeFullAuto
}

type SessionCapabilities struct {
	ProviderSelection bool `json:"provider_selection"`
	PermissionMode    bool `json:"permission_mode"`
	NativeTools       bool `json:"native_tools"`
	ExternalAgent     bool `json:"external_agent"`
}

func CapabilitiesForSession(session Session) SessionCapabilities {
	if NormalizeSessionRuntime(session.RuntimeID) == SessionRuntimeExternalAgent || NormalizeSessionKind(session.Kind) == SessionKindExternalAgent {
		return SessionCapabilities{ExternalAgent: true}
	}
	return SessionCapabilities{
		ProviderSelection: true,
		PermissionMode:    true,
		NativeTools:       true,
	}
}

type SessionListFilter struct {
	IncludeArchived bool
	IncludeHidden   bool
}

type CreateSessionInput struct {
	Title           string
	Kind            SessionKind
	RuntimeID       SessionRuntime
	ParentSessionID string
	Hidden          bool
	WorkingDir      string
	ProviderID      string
	ModelID         string
	PermissionMode  PermissionMode
	ExternalAgentID string
	// Readonly starts an external agent that never asks and cannot write.
	Readonly bool
	// Restricted refuses a session that would run tools without asking.
	Restricted bool
}

type RenameSessionInput struct {
	SessionID string
	Title     string
}

type PermissionMode string

const (
	PermissionModeDefault     PermissionMode = permission.ModeDefault
	PermissionModeAcceptEdits PermissionMode = permission.ModeAcceptEdits
	PermissionModeFullAuto    PermissionMode = permission.ModeFullAuto
)

type UpdateSessionPermissionModeInput struct {
	SessionID      string
	PermissionMode PermissionMode
}

type SessionProviderOption struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Type         string `json:"type,omitempty"`
	DefaultModel string `json:"default_model,omitempty"`
	Configured   bool   `json:"configured"`
}
