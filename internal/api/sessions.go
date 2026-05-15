package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessions, err := s.core.ListSessions(r.Context(), core.SessionListFilter{})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionsResponse{Sessions: sessions})
	case http.MethodPost:
		var req core.CreateSessionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		session, err := s.core.CreateSession(r.Context(), core.CreateSessionInput{
			Title:           req.Title,
			Kind:            core.SessionKind(req.Kind),
			RuntimeID:       core.SessionRuntime(req.RuntimeID),
			WorkingDir:      req.WorkingDir,
			ProviderID:      req.ProviderID,
			ModelID:         req.ModelID,
			PermissionMode:  core.PermissionMode(req.PermissionMode),
			ExternalAgentID: req.ExternalAgentID,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, core.SessionResponse{Session: session})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"))
	if path == "" {
		writeNotFound(w)
		return
	}
	childRoutes := []struct {
		suffix string
		handle func(http.ResponseWriter, *http.Request, string)
	}{
		{suffix: "/llm", handle: s.handleSessionLLMUpdate},
		{suffix: "/permissions", handle: s.handleSessionPermissionsUpdate},
		{suffix: "/models", handle: s.handleSessionLLMModels},
		{suffix: "/context", handle: s.handleSessionContext},
		{suffix: "/usage", handle: s.handleSessionUsage},
		{suffix: "/plan/items", handle: s.handleSessionPlanItems},
		{suffix: "/plan/status", handle: s.handleSessionPlanStatus},
		{suffix: "/plan/run", handle: s.handleSessionPlanRun},
		{suffix: "/plan", handle: s.handleSessionPlan},
		{suffix: "/compact", handle: s.handleSessionCompact},
		{suffix: "/system-message", handle: s.handleSessionSystemMessage},
	}
	for _, route := range childRoutes {
		sessionID, matched, ok := sessionChildID(path, route.suffix)
		if !matched {
			continue
		}
		if !ok {
			writeNotFound(w)
			return
		}
		route.handle(w, r, sessionID)
		return
	}

	sessionID := path
	if strings.Contains(sessionID, "/") {
		writeNotFound(w)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var req core.RenameSessionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		session, err := s.core.RenameSession(r.Context(), core.RenameSessionInput{
			SessionID: sessionID,
			Title:     req.Title,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionResponse{Session: session})
	case http.MethodDelete:
		if err := s.core.DeleteSession(r.Context(), sessionID); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(w, http.MethodPatch, http.MethodDelete)
	}
}

func sessionChildID(path string, suffix string) (string, bool, bool) {
	if !strings.HasSuffix(path, suffix) {
		return "", false, false
	}
	sessionID := strings.TrimSpace(strings.TrimSuffix(path, suffix))
	return sessionID, true, sessionID != "" && !strings.Contains(sessionID, "/")
}

func (s *Server) handleSessionPermissionsUpdate(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPatch {
		writeMethodNotAllowed(w, http.MethodPatch)
		return
	}
	var req core.UpdateSessionPermissionModeRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	session, err := s.core.UpdateSessionPermissionMode(r.Context(), core.UpdateSessionPermissionModeInput{
		SessionID:      sessionID,
		PermissionMode: core.NormalizePermissionMode(req.PermissionMode),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionResponse{Session: session})
}
