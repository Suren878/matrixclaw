package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.Core.ListSessions(r.Context(), core.SessionListFilter{})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionsResponse{Sessions: sessions})
}

func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	var req core.CreateSessionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if roleOf(r) != core.RoleOwner && core.NormalizePermissionMode(req.PermissionMode) != core.PermissionModeDefault {
		writeError(w, core.ErrOwnerOnly)
		return
	}
	session, err := s.Core.CreateSession(r.Context(), core.CreateSessionInput{
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
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	session, err := s.Core.GetSession(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionResponse{Session: session})
}

func (s *Server) handleSessionRename(w http.ResponseWriter, r *http.Request) {
	var req core.RenameSessionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	session, err := s.Core.RenameSession(r.Context(), core.RenameSessionInput{
		SessionID: r.PathValue("id"),
		Title:     req.Title,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionResponse{Session: session})
}

func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.Core.DeleteSession(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSessionPermissionsUpdate(w http.ResponseWriter, r *http.Request) {
	var req core.UpdateSessionPermissionModeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.PermissionMode) == "" {
		writeErrorMessage(w, http.StatusBadRequest, "permission_mode is required")
		return
	}
	session, err := s.Core.UpdateSessionPermissionMode(r.Context(), core.UpdateSessionPermissionModeInput{
		SessionID:      r.PathValue("id"),
		PermissionMode: core.NormalizePermissionMode(req.PermissionMode),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionResponse{Session: session})
}
