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
	Mode        string   `json:"mode,omitempty"`
	Path        string   `json:"path,omitempty"`
	Version     string   `json:"version,omitempty"`
	Detail      string   `json:"detail,omitempty"`
}

func (c *Core) ExternalAgents(ctx context.Context) []ExternalAgentDescriptor {
	registry := c.externalAgentRegistry()
	if registry == nil {
		return nil
	}
	descriptors := registry.List(ctx)
	out := make([]ExternalAgentDescriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		out = append(out, ExternalAgentDescriptor(descriptor))
	}
	return out
}

func (c *Core) ResolveExternalAgentID(id string) (string, bool) {
	registry := c.externalAgentRegistry()
	if registry == nil {
		return "", false
	}
	return registry.CanonicalID(id)
}

func (c *Core) createExternalAgentAttachment(ctx context.Context, session Session, input CreateSessionInput) error {
	if c.externalStore == nil {
		return fmt.Errorf("%w: external agent store unavailable", ErrExecutionUnavailable)
	}
	agentID := strings.TrimSpace(input.ExternalAgentID)
	if agentID == "" {
		return fmt.Errorf("%w: external_agent_id is required", ErrInvalidInput)
	}
	canonicalAgentID, ok := c.ResolveExternalAgentID(agentID)
	if !ok {
		return fmt.Errorf("%w: external agent %q is not configured", ErrExecutionUnavailable, agentID)
	}
	runtime, err := c.externalRuntime(canonicalAgentID)
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
	externalSession, err := runtime.StartSession(ctx, externalagents.StartSessionRequest{
		CWD:         session.WorkingDir,
		Model:       input.ModelID,
		Permissions: externalagents.Permissions{Mode: string(session.PermissionMode), Readonly: input.Readonly},
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
		AgentID:           canonicalAgentID,
		ExternalThreadID:  externalSession.ExternalThreadID,
		ExternalSessionID: externalSession.ExternalSessionID,
		CWD:               externalSession.CWD,
		Model:             externalSession.Model,
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
		return "", fmt.Errorf("%w: external agent metadata: %w", ErrInvalidInput, err)
	}
	return string(data), nil
}
