package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleExternalAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, core.ExternalAgentsResponse{
		Agents: s.core.ExternalAgents(r.Context()),
	})
}
