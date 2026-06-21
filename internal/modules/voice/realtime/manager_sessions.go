package realtime

import (
	"context"
	"errors"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *Manager) resolveCoreSession(ctx context.Context, req SessionCreateRequest) (core.Session, error) {
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID != "" {
		session, err := m.core.GetSession(ctx, sessionID)
		if err != nil {
			return core.Session{}, err
		}
		if strings.TrimSpace(req.Client) != "" && strings.TrimSpace(req.ExternalKey) != "" {
			if _, err := m.core.UseBinding(ctx, core.UseBindingInput{Client: req.Client, ExternalKey: req.ExternalKey, SessionID: session.ID}); err != nil {
				return core.Session{}, err
			}
		}
		return session, nil
	}
	if strings.TrimSpace(req.Client) != "" && strings.TrimSpace(req.ExternalKey) != "" {
		binding, err := m.core.CurrentBinding(ctx, req.Client, req.ExternalKey)
		if err == nil {
			return m.core.GetSession(ctx, binding.SessionID)
		}
		if !errors.Is(err, core.ErrBindingNotFound) && !errors.Is(err, core.ErrNotFound) {
			return core.Session{}, err
		}
	}
	session, err := m.core.CreateSession(ctx, core.CreateSessionInput{
		Title:      "Voice conversation",
		WorkingDir: req.WorkingDir,
	})
	if err != nil {
		return core.Session{}, err
	}
	if strings.TrimSpace(req.Client) != "" && strings.TrimSpace(req.ExternalKey) != "" {
		if _, err := m.core.UseBinding(ctx, core.UseBindingInput{Client: req.Client, ExternalKey: req.ExternalKey, SessionID: session.ID}); err != nil {
			return core.Session{}, err
		}
	}
	return session, nil
}

func (m *Manager) session(sessionID string) (*voiceSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[strings.TrimSpace(sessionID)]
	return session, ok
}

func (m *Manager) provider(ctx context.Context, providerID string) (Provider, ProviderDescriptor, bool) {
	providerID = normalizeID(providerID)
	m.providersMu.RLock()
	provider, ok := m.providers[providerID]
	m.providersMu.RUnlock()
	if !ok {
		return nil, ProviderDescriptor{}, false
	}
	return provider, provider.Descriptor(ctx), true
}

func (m *Manager) providerDescriptors(ctx context.Context) []ProviderDescriptor {
	m.providersMu.RLock()
	providers := make([]Provider, 0, len(m.providers))
	for _, provider := range m.providers {
		providers = append(providers, provider)
	}
	m.providersMu.RUnlock()
	out := make([]ProviderDescriptor, 0, len(providers))
	for _, provider := range providers {
		out = append(out, provider.Descriptor(ctx))
	}
	return out
}

func (m *Manager) markSessionStreaming(session *voiceSession) SessionInfo {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.info.Status != SessionStatusStreaming {
		session.info.Status = SessionStatusStreaming
		session.info.UpdatedAt = m.now().UTC()
	}
	return session.info
}

func (m *Manager) markSessionClosed(session *voiceSession, message string, status SessionStatus) SessionInfo {
	session.mu.Lock()
	defer session.mu.Unlock()
	now := m.now().UTC()
	session.info.Status = status
	session.info.Error = strings.TrimSpace(message)
	session.info.UpdatedAt = now
	session.info.ClosedAt = &now
	return session.info
}

func (m *Manager) activeSessionCountLocked() int {
	count := 0
	for _, session := range m.sessions {
		session.mu.Lock()
		status := session.info.Status
		session.mu.Unlock()
		if status != SessionStatusClosed && status != SessionStatusFailed {
			count++
		}
	}
	return count
}

func (m *Manager) currentConfig(ctx context.Context) Config {
	if m == nil {
		return Config{}
	}
	if m.configSource != nil {
		return normalizeConfig(m.configSource(ctx))
	}
	return normalizeConfig(m.config)
}

func (m *Manager) systemInstruction(session *voiceSession) string {
	session.mu.Lock()
	defer session.mu.Unlock()
	return strings.TrimSpace(session.systemInstruction)
}

func (m *Manager) toolDeclarations(client string) []ToolDeclaration {
	if m == nil || m.core == nil {
		return nil
	}
	telephony := strings.EqualFold(strings.TrimSpace(client), "telephony")
	specs := m.core.ListToolSpecs()
	out := make([]ToolDeclaration, 0, len(specs))
	for _, spec := range specs {
		name := strings.TrimSpace(spec.ID)
		if name == "" {
			continue
		}
		if telephony && name != "telephony_end_call" {
			continue
		}
		out = append(out, ToolDeclaration{
			Name:             name,
			Description:      strings.TrimSpace(spec.Description),
			Parameters:       spec.InputJSONSchema,
			RequiresApproval: spec.RequiresApproval(),
		})
	}
	return out
}
