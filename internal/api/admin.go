package api

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleAdminReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.adminReload == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "admin reload is not configured")
		return
	}
	if err := s.adminReload(r.Context()); err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.markRuntimeReloaded()
	writeJSON(w, http.StatusOK, core.OKResponse{OK: true})
}

func (s *Server) handleAdminRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.adminRestart == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "admin restart is not configured")
		return
	}
	var req core.AdminRestartRequest
	if !decodeOptionalJSONBody(w, r, &req) {
		return
	}
	if err := s.adminRestart(r.Context(), req); err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, core.OKResponse{OK: true})
}

func (s *Server) handleAdminStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.adminStop == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "admin stop is not configured")
		return
	}
	if err := s.adminStop(r.Context()); err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, core.OKResponse{OK: true})
}

// applySetup makes the running daemon follow setup.json after an edit.
func (s *Server) applySetup(ctx context.Context) error {
	if s.adminReload == nil {
		return nil
	}
	return s.adminReload(ctx)
}

// reload is applySetup for a handler; it answers 500 and returns false when
// applying fails.
func (s *Server) reload(w http.ResponseWriter, ctx context.Context) bool {
	if err := s.applySetup(ctx); err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return false
	}
	return true
}
