package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleSetupProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.Setup.ProviderItems()
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.allowProviderSetup(r) {
		providers = configuredSetupProviders(providers)
	}
	writeJSON(w, http.StatusOK, setup.ProviderSetupListResponse{Providers: providers})
}

func (s *Server) handleSetupProviderModels(w http.ResponseWriter, r *http.Request) {
	if !s.requireProviderSetup(w, r) {
		return
	}
	var update setup.ProviderSetupUpdate
	if !decodeJSON(w, r, &update) {
		return
	}
	models, err := s.Setup.ProviderModelCatalogFor(r.Context(), r.PathValue("id"), update)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func (s *Server) handleSetupProviderDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireProviderSetup(w, r) {
		return
	}
	if err := s.Setup.DeleteProvider(r.PathValue("id")); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reload(w, r.Context()) {
		return
	}
	writeJSON(w, http.StatusOK, setup.ProviderSetupOKResponse{OK: true})
}

func (s *Server) handleSetupProviderUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireProviderSetup(w, r) {
		return
	}
	var update setup.ProviderSetupUpdate
	if !decodeJSON(w, r, &update) {
		return
	}
	item, err := s.Setup.ConfigureProvider(r.PathValue("id"), update)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reload(w, r.Context()) {
		return
	}
	if err := s.ensureSessionProviderLoaded(item.ID); err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, setup.ProviderSetupResponse{Provider: item})
}

func (s *Server) ensureSessionProviderLoaded(providerID string) error {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil
	}
	for _, option := range s.Core.SessionProviderOptions() {
		if strings.EqualFold(strings.TrimSpace(option.ID), providerID) {
			return nil
		}
	}
	return fmt.Errorf("provider %q was saved, but daemon runtime did not load it", providerID)
}

func (s *Server) requireProviderSetup(w http.ResponseWriter, r *http.Request) bool {
	if s.allowProviderSetup(r) {
		return true
	}
	writeErrorMessage(w, http.StatusForbidden, "provider setup is disabled for this client")
	return false
}

func (s *Server) allowProviderSetup(r *http.Request) bool {
	allowed, err := s.Setup.AllowProviderSetupForClient(r.URL.Query().Get("client"))
	return err == nil && allowed
}

func configuredSetupProviders(providers []setup.ProviderSetupItem) []setup.ProviderSetupItem {
	filtered := make([]setup.ProviderSetupItem, 0, len(providers))
	for _, provider := range providers {
		if provider.Configured {
			filtered = append(filtered, provider)
		}
	}
	return filtered
}
