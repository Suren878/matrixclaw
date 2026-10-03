package realtime

import (
	"context"
	"errors"
	"strings"
	"time"

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

const unstreamedSessionTTL = time.Minute

func (m *Manager) session(sessionID string) (*voiceSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[strings.TrimSpace(sessionID)]
	return session, ok
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

func (m *Manager) markSessionClosed(session *voiceSession) SessionInfo {
	session.mu.Lock()
	defer session.mu.Unlock()
	now := m.now().UTC()
	session.info.Status = SessionStatusClosed
	session.info.UpdatedAt = now
	session.info.ClosedAt = &now
	return session.info
}

func (m *Manager) forgetSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, strings.TrimSpace(sessionID))
}

// pruneUnstreamedLocked drops sessions whose client never opened the stream,
// for example after a failed websocket dial.
func (m *Manager) pruneUnstreamedLocked(now time.Time) {
	for id, session := range m.sessions {
		session.mu.Lock()
		stale := session.info.Status == SessionStatusCreated && now.Sub(session.info.CreatedAt) > unstreamedSessionTTL
		session.mu.Unlock()
		if stale {
			delete(m.sessions, id)
		}
	}
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
			Name:        name,
			Description: strings.TrimSpace(spec.Description),
			Parameters:  spec.InputJSONSchema,
		})
	}
	return out
}
