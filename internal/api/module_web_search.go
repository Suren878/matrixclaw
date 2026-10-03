package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleWebSearch(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.Setup.GetWebSearchConfig()
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, setup.WebSearchResponse(cfg))
}

func (s *Server) handleWebSearchUpdate(w http.ResponseWriter, r *http.Request) {
	var update setup.WebSearchConfigUpdate
	if !decodeJSON(w, r, &update) {
		return
	}
	cfg, err := s.Setup.UpdateWebSearchConfig(update)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reload(w, r.Context()) {
		return
	}
	writeJSON(w, http.StatusOK, setup.WebSearchResponse(cfg))
}
