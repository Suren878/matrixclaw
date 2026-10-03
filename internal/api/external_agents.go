package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleExternalAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, core.ExternalAgentsResponse{
		Agents: s.Core.ExternalAgents(r.Context()),
	})
}

func (s *Server) handleExternalAgentUpdate(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	canonicalID, ok := s.Core.ResolveExternalAgentID(agentID)
	if !ok {
		writeErrorMessage(w, http.StatusNotFound, "external agent not found: "+agentID)
		return
	}
	var update core.UpdateExternalAgentRequest
	if !decodeJSON(w, r, &update) {
		return
	}
	cfg := s.Setup.ExternalAgentConfig(canonicalID)
	if update.Enabled != nil {
		cfg.Enabled = *update.Enabled
	}
	if strings.TrimSpace(update.Path) != "" {
		cfg.Path = strings.TrimSpace(update.Path)
	}
	if _, err := s.Setup.UpdateExternalAgent(canonicalID, setup.ExternalAgentConfig{
		Enabled: cfg.Enabled,
		Path:    cfg.Path,
	}); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reload(w, r.Context()) {
		return
	}
	writeJSON(w, http.StatusOK, core.ExternalAgentsResponse{
		Agents: s.Core.ExternalAgents(r.Context()),
	})
}
