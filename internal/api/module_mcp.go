package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getMCPConfig(w, r)
	case http.MethodPatch:
		s.updateMCPConfig(w, r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch)
	}
}

func (s *Server) getMCPConfig(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.setup.GetMCPConfig()
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, setup.MCPConfigResponse{
		Config:  cfg,
		Enabled: cfg.Enabled,
		Status:  setup.MCPConfigStatus(cfg),
	})
}

func (s *Server) updateMCPConfig(w http.ResponseWriter, r *http.Request) {
	var update setup.MCPConfigUpdate
	if !decodeJSONBody(w, r, &update) {
		return
	}
	cfg, err := s.setup.UpdateMCPConfig(update)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, setup.MCPConfigResponse{
		Config:  cfg,
		Enabled: cfg.Enabled,
		Status:  setup.MCPConfigStatus(cfg),
	})
}

func (s *Server) handleMCPByID(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	raw := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/modules/mcp/"))
	if raw == "" {
		writeNotFound(w)
		return
	}
	if decoded, err := url.PathUnescape(raw); err == nil {
		raw = decoded
	}
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) != 2 || parts[1] != "server" {
		writeNotFound(w)
		return
	}
	if r.Method != http.MethodPatch {
		writeMethodNotAllowed(w, http.MethodPatch)
		return
	}
	var update setup.MCPServerUpdate
	if !decodeJSONBody(w, r, &update) {
		return
	}
	cfg, err := s.setup.UpdateMCPServer(parts[0], update)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, setup.MCPConfigResponse{
		Config:  cfg,
		Enabled: cfg.Enabled,
		Status:  setup.MCPConfigStatus(cfg),
	})
}
