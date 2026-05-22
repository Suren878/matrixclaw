package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleSetupProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}

	providers, err := s.setup.ProviderSetupItems()
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.allowProviderSetup(r) {
		providers = configuredSetupProviders(providers)
	}
	writeJSON(w, http.StatusOK, setup.ProviderSetupListResponse{Providers: providers})
}

func (s *Server) handleSetupProviderByID(w http.ResponseWriter, r *http.Request) {
	providerPath := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/setup/providers/"), "/")
	providerID := providerPath
	modelsRequest := false
	if before, ok := strings.CutSuffix(providerPath, "/models"); ok {
		providerID = strings.Trim(before, "/")
		modelsRequest = true
	}
	if providerID == "" {
		writeErrorMessage(w, http.StatusBadRequest, "provider id is required")
		return
	}
	if modelsRequest {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
	} else if r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		writeMethodNotAllowed(w, http.MethodPatch, http.MethodDelete)
		return
	}
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	if !s.allowProviderSetup(r) {
		writeErrorMessage(w, http.StatusForbidden, "provider setup is disabled for this client")
		return
	}
	if modelsRequest {
		var update setup.ProviderSetupUpdate
		if !decodeJSONBody(w, r, &update) {
			return
		}
		models, err := s.setup.ProviderModelCatalogContext(r.Context(), providerID, update)
		if err != nil {
			writeErrorMessage(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, models)
		return
	}
	if r.Method == http.MethodDelete {
		if err := s.setup.DeleteProviderContext(r.Context(), providerID); err != nil {
			writeErrorMessage(w, http.StatusBadRequest, err.Error())
			return
		}
		if s.adminReload != nil {
			if err := s.adminReload(r.Context()); err != nil {
				writeErrorMessage(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, setup.ProviderSetupOKResponse{OK: true})
		return
	}
	var update setup.ProviderSetupUpdate
	if !decodeJSONBody(w, r, &update) {
		return
	}
	item, err := s.setup.ConfigureProviderContext(r.Context(), providerID, update)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.adminReload != nil {
		if err := s.adminReload(r.Context()); err != nil {
			writeErrorMessage(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.ensureSessionProviderLoaded(item.ID); err != nil {
			writeErrorMessage(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, setup.ProviderSetupResponse{Provider: item})
}

func (s *Server) ensureSessionProviderLoaded(providerID string) error {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" || s.core == nil {
		return nil
	}
	for _, option := range s.core.SessionProviderOptions() {
		if strings.EqualFold(strings.TrimSpace(option.ID), providerID) {
			return nil
		}
	}
	return fmt.Errorf("provider %q was saved, but daemon runtime did not load it", providerID)
}

func (s *Server) allowProviderSetup(r *http.Request) bool {
	if s.setup == nil {
		return false
	}
	allowed, err := s.setup.AllowProviderSetupForClient(r.URL.Query().Get("client"))
	if err != nil {
		return false
	}
	return allowed
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
