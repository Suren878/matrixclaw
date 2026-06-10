package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
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
	module, err := s.setup.BrowserModule()
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	module = localruntime.New("").DecorateBrowserModule(module)
	writeJSON(w, http.StatusOK, setup.BrowserModuleResponse{Module: module})
}

func (s *Server) updateBrowserModule(w http.ResponseWriter, r *http.Request) {
	var update setup.BrowserModuleUpdate
	if !decodeJSONBody(w, r, &update) {
		return
	}
	module, err := s.setup.UpdateBrowserModule(update)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.adminReload != nil {
		if err := s.adminReload(r.Context()); err != nil {
			writeErrorMessage(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	module = localruntime.New("").DecorateBrowserModule(module)
	writeJSON(w, http.StatusOK, setup.BrowserModuleResponse{Module: module})
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
	module, err := s.setup.BrowserModule()
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	var provider setup.BrowserProviderOption
	found := false
	for _, item := range module.Providers {
		if item.ID == providerID {
			provider = item
			found = true
			break
		}
	}
	if !found {
		writeErrorMessage(w, http.StatusNotFound, "browser provider not found")
		return
	}
	updated, err := localruntime.New("").ApplyBrowserAction(r.Context(), provider, request)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if browserProviderActionReloadsModule(module, provider, request.Action) && s.adminReload != nil {
		if err := s.adminReload(r.Context()); err != nil {
			writeErrorMessage(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, setup.BrowserProviderActionResponse{Provider: updated})
}

func browserProviderActionReloadsModule(module setup.BrowserModuleDescriptor, provider setup.BrowserProviderOption, action string) bool {
	if !module.Enabled || module.ProviderID != provider.ID {
		return false
	}
	action = strings.ToLower(strings.TrimSpace(action))
	return action == provider.ActionIDs.InstallRuntime || action == provider.ActionIDs.DeleteRuntime
}
