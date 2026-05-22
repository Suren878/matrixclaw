package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleWebSearch(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getWebSearchConfig(w, r)
	case http.MethodPatch:
		s.updateWebSearchConfig(w, r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch)
	}
}

func (s *Server) getWebSearchConfig(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.setup.GetWebSearchConfig()
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, setup.WebSearchConfigResponse{
		Config:   cfg,
		Provider: cfg.Provider,
		Status:   setup.WebSearchConfigStatus(cfg),
	})
}

func (s *Server) updateWebSearchConfig(w http.ResponseWriter, r *http.Request) {
	var update setup.WebSearchConfig
	if !decodeJSONBody(w, r, &update) {
		return
	}
	cfg, err := s.setup.UpdateWebSearchConfig(update)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, setup.WebSearchConfigResponse{
		Config:   cfg,
		Provider: cfg.Provider,
		Status:   setup.WebSearchConfigStatus(cfg),
	})
}
