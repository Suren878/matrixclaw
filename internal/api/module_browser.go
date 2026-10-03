package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleBrowserModule(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getBrowserModule(w, r)
	case http.MethodPatch:
		s.updateBrowserModule(w, r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch)
	}
}

func (s *Server) getBrowserModule(w http.ResponseWriter, _ *http.Request) {
	s.writeBrowserModuleResponse(w)
}

func (s *Server) writeBrowserModuleResponse(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, setup.BrowserModuleResponse{Module: s.modules.Browser.Descriptor()})
}

func (s *Server) updateBrowserModule(w http.ResponseWriter, r *http.Request) {
	var update setup.BrowserModuleUpdate
	if !decodeJSONBody(w, r, &update) {
		return
	}
	if _, err := s.setup.UpdateBrowserModule(update); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reload(w, r.Context()) {
		return
	}
	s.writeBrowserModuleResponse(w)
}

func (s *Server) handleBrowserProvider(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/modules/browser/providers/"), "/")
	providerID, suffix, _ := strings.Cut(raw, "/")
	if providerID == "" || suffix != "action" {
		writeNotFound(w)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var request setup.BrowserProviderActionRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	updated, err := s.modules.Browser.Action(r.Context(), providerID, request)
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
