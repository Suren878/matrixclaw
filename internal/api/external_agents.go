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
	if s.core == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "core service is not configured")
		return
	}
	canonicalID, ok := s.core.ResolveExternalAgentID(agentID)
	if !ok {
		writeErrorMessage(w, http.StatusNotFound, "external agent not found: "+agentID)
		return
	}
	var update core.UpdateExternalAgentRequest
	if !decodeJSONBody(w, r, &update) {
		return
	}
	cfg := s.setup.ExternalAgentConfig(canonicalID)
	if update.Enabled != nil {
		cfg.Enabled = *update.Enabled
	}
	if strings.TrimSpace(update.Path) != "" {
		cfg.Path = strings.TrimSpace(update.Path)
	}
	if _, err := s.setup.UpdateExternalAgent(canonicalID, setup.ExternalAgentConfig{
		Enabled: cfg.Enabled,
		Path:    cfg.Path,
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
