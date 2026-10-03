package api

import (
	"net/http"
	"slices"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.Setup.GetMCPConfig()
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	if roleOf(r) != core.RoleOwner {
		cfg.Servers = slices.Clone(cfg.Servers)
		for i := range cfg.Servers {
			cfg.Servers[i].Env = maskedEnv(cfg.Servers[i].Env)
		}
	}
	s.writeMCPConfigResponse(w, cfg)
}

// maskedEnv keeps the names of an MCP server's env and hides the values.
func maskedEnv(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	out := make(map[string]string, len(env))
	for name, value := range env {
		out[name] = setup.MaskSecret(value)
	}
	return out
}

func (s *Server) handleMCPUpdate(w http.ResponseWriter, r *http.Request) {
	var update setup.MCPConfigUpdate
	if !decodeJSON(w, r, &update) {
		return
	}
	cfg, err := s.Setup.UpdateMCPConfig(update)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeAppliedMCPConfig(w, r, cfg)
}

func (s *Server) handleMCPServerCreate(w http.ResponseWriter, r *http.Request) {
	var request setup.MCPServerCreateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	cfg, err := s.Setup.CreateMCPServer(request.Server)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeAppliedMCPConfig(w, r, cfg)
}

func (s *Server) handleMCPServerUpdate(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("id")
	var update setup.MCPServerUpdate
	if !decodeJSON(w, r, &update) {
		return
	}
	cfg, err := s.Setup.UpdateMCPServer(serverID, update)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeAppliedMCPConfig(w, r, cfg)
}

func (s *Server) handleMCPServerDelete(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.Setup.DeleteMCPServer(r.PathValue("id"))
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeAppliedMCPConfig(w, r, cfg)
}

func (s *Server) writeMCPConfigResponse(w http.ResponseWriter, cfg setup.MCPConfig) {
	writeJSON(w, http.StatusOK, setup.MCPConfigResponse{
		Config:  cfg,
		Enabled: cfg.Enabled,
		Status:  setup.MCPConfigStatus(cfg),
	})
}

// writeAppliedMCPConfig applies an MCP edit to the running daemon and answers
// with the saved settings.
func (s *Server) writeAppliedMCPConfig(w http.ResponseWriter, r *http.Request, cfg setup.MCPConfig) {
	if !s.reload(w, r.Context()) {
		return
	}
	s.writeMCPConfigResponse(w, cfg)
}
