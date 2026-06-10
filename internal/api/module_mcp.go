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
	writeMCPConfigResponse(w, cfg)
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
	if !s.reloadAfterMCPConfigChange(w, r) {
		return
	}
	writeMCPConfigResponse(w, cfg)
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
	raw = strings.Trim(raw, "/")
	if raw == "servers" {
		s.createMCPServer(w, r)
		return
	}
	parts := strings.Split(raw, "/")
	if len(parts) != 2 || parts[1] != "server" {
		writeNotFound(w)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		s.updateMCPServer(w, r, parts[0])
	case http.MethodDelete:
		s.deleteMCPServer(w, r, parts[0])
	default:
		writeMethodNotAllowed(w, http.MethodPatch, http.MethodDelete)
	}
}

func (s *Server) createMCPServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var request setup.MCPServerCreateRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	cfg, err := s.setup.CreateMCPServer(request.Server)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reloadAfterMCPConfigChange(w, r) {
		return
	}
	writeMCPConfigResponse(w, cfg)
}

func (s *Server) updateMCPServer(w http.ResponseWriter, r *http.Request, serverID string) {
	var update setup.MCPServerUpdate
	if !decodeJSONBody(w, r, &update) {
		return
	}
	cfg, err := s.setup.UpdateMCPServer(serverID, update)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reloadAfterMCPConfigChange(w, r) {
		return
	}
	writeMCPConfigResponse(w, cfg)
}

func (s *Server) deleteMCPServer(w http.ResponseWriter, r *http.Request, serverID string) {
	cfg, err := s.setup.DeleteMCPServer(serverID)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reloadAfterMCPConfigChange(w, r) {
		return
	}
	writeMCPConfigResponse(w, cfg)
}

func (s *Server) reloadAfterMCPConfigChange(w http.ResponseWriter, r *http.Request) bool {
	if s.adminReload == nil {
		return true
	}
	if err := s.adminReload(r.Context()); err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return false
	}
	return true
}

func writeMCPConfigResponse(w http.ResponseWriter, cfg setup.MCPConfig) {
	writeJSON(w, http.StatusOK, setup.MCPConfigResponse{
		Config:  cfg,
		Enabled: cfg.Enabled,
		Status:  setup.MCPConfigStatus(cfg),
	})
}
