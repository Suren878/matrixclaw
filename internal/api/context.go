package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionContext(w http.ResponseWriter, r *http.Request) {
	report, err := s.Core.SessionContext(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionContextResponse{Context: report})
}

func (s *Server) handleSessionUsage(w http.ResponseWriter, r *http.Request) {
	report, err := s.Core.Usage(r.Context(), core.UsageFilter{SessionID: r.PathValue("id")})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.UsageResponse{Usage: report})
}

func (s *Server) handleSessionCompact(w http.ResponseWriter, r *http.Request) {
	result, err := s.Core.CompactSession(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionCompactResponse{Compact: result})
}

func (s *Server) handleSessionClear(w http.ResponseWriter, r *http.Request) {
	message, err := s.Core.ClearContext(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.MessageResponse{Message: message})
}
