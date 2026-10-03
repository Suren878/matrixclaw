package api

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleAdminReload(w http.ResponseWriter, r *http.Request) {
	if err := s.Reload(r.Context()); err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.markRuntimeReloaded()
	writeJSON(w, http.StatusOK, core.OKResponse{OK: true})
}

func (s *Server) handleAdminRestart(w http.ResponseWriter, r *http.Request) {
	var req core.AdminRestartRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.Restart(r.Context(), req); err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, core.OKResponse{OK: true})
}

func (s *Server) handleAdminStop(w http.ResponseWriter, r *http.Request) {
	if err := s.Stop(r.Context()); err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, core.OKResponse{OK: true})
}

// reload makes the running daemon follow setup.json after an edit; it answers
// 500 and returns false when applying fails.
func (s *Server) reload(w http.ResponseWriter, ctx context.Context) bool {
	if err := s.Reload(ctx); err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return false
	}
	return true
}
