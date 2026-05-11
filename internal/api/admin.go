package api

import (
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
