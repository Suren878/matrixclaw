package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

type ExternalAgentDescriptor struct {
	ID          string   `json:"id"`
	Aliases     []string `json:"aliases,omitempty"`
	DisplayName string   `json:"display_name"`
	Installed   bool     `json:"installed"`
	Enabled     bool     `json:"enabled"`
	AuthState   string   `json:"auth_state,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	Path        string   `json:"path,omitempty"`
	Version     string   `json:"version,omitempty"`
	Detail      string   `json:"detail,omitempty"`
}

const (
	legacyCodexRuntime    SessionRuntime = "codex"
	legacyCodexAppRuntime SessionRuntime = "codex-app"
)

func NormalizeSessionKind(kind SessionKind) SessionKind {
	switch SessionKind(strings.TrimSpace(string(kind))) {
	case "", SessionKindAssistant:
		return SessionKindAssistant
	case SessionKindExternalAgent:
		return SessionKindExternalAgent
	default:
		return SessionKindAssistant
	}
}

func NormalizeSessionRuntime(runtimeID SessionRuntime) SessionRuntime {
	switch SessionRuntime(strings.ToLower(strings.TrimSpace(string(runtimeID)))) {
	case "", SessionRuntimeMatrixClaw, "assistant", "native", "core":
		return SessionRuntimeMatrixClaw
	case SessionRuntimeExternalAgent, "external", "agent", legacyCodexRuntime, legacyCodexAppRuntime:
		return SessionRuntimeExternalAgent
	default:
		return SessionRuntime(strings.ToLower(strings.TrimSpace(string(runtimeID))))
	}
}

func sessionRuntimeForCreate(runtimeID SessionRuntime, kind SessionKind, externalAgentID string) SessionRuntime {
	rawRuntimeID := strings.ToLower(strings.TrimSpace(string(runtimeID)))
	if strings.TrimSpace(externalAgentID) != "" {
		return SessionRuntimeExternalAgent
	}
	switch rawRuntimeID {
	case string(legacyCodexRuntime), string(legacyCodexAppRuntime):
		return SessionRuntimeExternalAgent
	}
	runtimeID = NormalizeSessionRuntime(runtimeID)
	if runtimeID != SessionRuntimeMatrixClaw {
		return SessionRuntimeExternalAgent
	}
	if NormalizeSessionKind(kind) == SessionKindExternalAgent {
		return SessionRuntimeExternalAgent
	}
	return SessionRuntimeMatrixClaw
}

func sessionKindForRuntime(runtimeID SessionRuntime) SessionKind {
	if NormalizeSessionRuntime(runtimeID) == SessionRuntimeMatrixClaw {
		return SessionKindAssistant
	}
	return SessionKindExternalAgent
}

func externalAgentIDForRuntime(runtimeID SessionRuntime, explicit string) string {
	explicit = normalizeText(explicit)
	if explicit != "" {
		return explicit
	}
	rawRuntimeID := strings.ToLower(strings.TrimSpace(string(runtimeID)))
	switch rawRuntimeID {
	case "", string(SessionRuntimeMatrixClaw), string(SessionRuntimeExternalAgent), "external", "agent", "assistant", "native", "core":
		return ""
	default:
		return rawRuntimeID
	}
}

func (c *Core) ExternalAgents(ctx context.Context) []ExternalAgentDescriptor {
	if c.externalAgents == nil {
		return nil
	}
	descriptors := c.externalAgents.List(ctx)
	out := make([]ExternalAgentDescriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		out = append(out, ExternalAgentDescriptor{
			ID:          descriptor.ID,
			Aliases:     descriptor.Aliases,
			DisplayName: descriptor.DisplayName,
			Installed:   descriptor.Installed,
			Enabled:     descriptor.Enabled,
			AuthState:   descriptor.AuthState,
			Mode:        descriptor.Mode,
			Path:        descriptor.Path,
			Version:     descriptor.Version,
			Detail:      descriptor.Detail,
		})
	}
	return out
}

func (c *Core) createExternalAgentAttachment(ctx context.Context, session Session, input CreateSessionInput) error {
	if c.externalStore == nil {
		return fmt.Errorf("%w: external agent store unavailable", ErrExecutionUnavailable)
	}
	agentID := externalAgentIDForRuntime(input.RuntimeID, input.ExternalAgentID)
	if agentID == "" {
		return fmt.Errorf("%w: external_agent_id is required", ErrInvalidInput)
	}
	runtime, err := c.externalRuntime(agentID)
	if err != nil {
		return err
	}
	availability := runtime.Available(ctx)
	if !availability.Installed {
		return fmt.Errorf("%w: external agent %q is not installed", ErrExecutionUnavailable, agentID)
	}
	if !availability.Enabled {
		return fmt.Errorf("%w: external agent %q is not enabled", ErrExecutionUnavailable, agentID)
	}
	approvalPolicy, sandbox := externalAgentPolicyForPermissionMode(session.PermissionMode)
	externalSession, err := runtime.StartSession(ctx, externalagents.StartSessionRequest{
		CWD:            session.WorkingDir,
		Model:          normalizeText(input.ModelID),
		ApprovalPolicy: approvalPolicy,
		Sandbox:        sandbox,
		Metadata: map[string]any{
			"matrixclaw_session_id": session.ID,
			"matrixclaw_runtime_id": session.RuntimeID,
		},
	})
	if err != nil {
		return err
	}
	metadataJSON, err := externalAgentMetadataJSON(externalSession.Metadata)
	if err != nil {
		return err
	}
	if err := c.externalStore.SaveExternalAgentSession(ctx, externalagents.SessionAttachment{
		SessionID:         session.ID,
		AgentID:           externalSession.AgentID,
		ExternalThreadID:  externalSession.ExternalThreadID,
		ExternalSessionID: externalSession.ExternalSessionID,
		CWD:               externalSession.CWD,
		Model:             externalSession.Model,
		ApprovalPolicy:    approvalPolicy,
		Sandbox:           sandbox,
		MetadataJSON:      metadataJSON,
		CreatedAt:         session.CreatedAt,
		UpdatedAt:         session.UpdatedAt,
	}); err != nil {
		return err
	}
	return nil
}

func externalAgentMetadataJSON(metadata map[string]any) (string, error) {
	if len(metadata) == 0 {
		return "{}", nil
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("%w: external agent metadata: %v", ErrInvalidInput, err)
	}
	return string(data), nil
}

func externalAgentPolicyForPermissionMode(mode PermissionMode) (string, string) {
	switch NormalizePermissionMode(string(mode)) {
	case PermissionModeFullAuto:
		return "never", "danger-full-access"
	case PermissionModeAcceptEdits:
		return "on-request", "workspace-write"
	default:
		return "on-request", "read-only"
	}
}
