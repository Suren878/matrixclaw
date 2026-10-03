package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleBrowserModule(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, setup.BrowserModuleResponse{Module: s.Modules.Browser.Descriptor()})
}

func (s *Server) handleBrowserModuleUpdate(w http.ResponseWriter, r *http.Request) {
	var update setup.BrowserModuleUpdate
	if !decodeJSON(w, r, &update) {
		return
	}
	if _, err := s.Setup.UpdateBrowserModule(update); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reload(w, r.Context()) {
		return
	}
	writeJSON(w, http.StatusOK, setup.BrowserModuleResponse{Module: s.Modules.Browser.Descriptor()})
}

func (s *Server) handleBrowserProviderAction(w http.ResponseWriter, r *http.Request) {
	var request setup.BrowserProviderActionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	updated, err := s.Modules.Browser.Action(r.Context(), r.PathValue("provider"), request)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	// Installing or removing the runtime adds or drops the browser's MCP server.
	if !s.reload(w, r.Context()) {
		return
	}
	writeJSON(w, http.StatusOK, setup.BrowserProviderActionResponse{Provider: updated})
}
