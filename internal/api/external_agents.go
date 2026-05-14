package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
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

func (s *Server) handleExternalAgentByID(w http.ResponseWriter, r *http.Request) {
	agentID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/external-agents/"), "/")
	if agentID == "" {
		writeErrorMessage(w, http.StatusBadRequest, "external agent id is required")
		return
	}
	if r.Method != http.MethodPatch {
		writeMethodNotAllowed(w, http.MethodPatch)
		return
	}
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	var update core.UpdateExternalAgentRequest
	if !decodeJSONBody(w, r, &update) {
		return
	}
	if _, err := s.setup.UpdateExternalAgent(agentID, setup.ExternalAgentConfig{
		Enabled: update.Enabled,
		Path:    update.Path,
	}); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.adminReload != nil {
		if err := s.adminReload(r.Context()); err != nil {
			writeErrorMessage(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, core.ExternalAgentsResponse{
		Agents: s.core.ExternalAgents(r.Context()),
	})
}
