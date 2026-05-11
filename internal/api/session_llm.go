package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleSessionProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionProvidersResponse{Providers: s.core.SessionProviderOptions()})
}

func (s *Server) handleSessionLLMModels(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	providerID, modelID, models, err := s.core.ModelsForSession(r.Context(), sessionID)
	if shouldReloadSessionLLM(err) {
		if reloadErr := s.reloadSessionLLMRegistry(r.Context()); reloadErr != nil {
			writeErrorMessage(w, http.StatusInternalServerError, "reload provider registry: "+reloadErr.Error())
			return
		}
		providerID, modelID, models, err = s.core.ModelsForSession(r.Context(), sessionID)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionModelsResponse{
		ProviderID: providerID,
		ModelID:    modelID,
		Models:     models,
	})
}

func (s *Server) handleSessionLLMUpdate(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPatch {
		writeMethodNotAllowed(w, http.MethodPatch)
		return
	}
	var req core.UpdateSessionLLMRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	switch {
	case strings.TrimSpace(req.ProviderID) != "":
		session, err := s.core.UpdateSessionProvider(r.Context(), sessionID, req.ProviderID)
		if shouldReloadSessionLLM(err) {
			if reloadErr := s.reloadSessionLLMRegistry(r.Context()); reloadErr != nil {
				writeErrorMessage(w, http.StatusInternalServerError, "reload provider registry: "+reloadErr.Error())
				return
			}
			session, err = s.core.UpdateSessionProvider(r.Context(), sessionID, req.ProviderID)
		}
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionResponse{Session: session})
	case strings.TrimSpace(req.ModelID) != "":
		session, err := s.core.UpdateSessionModel(r.Context(), sessionID, req.ModelID)
		if err != nil {
			writeError(w, err)
			return
		}
		if err := s.persistSessionModelSelection(r.Context(), session); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionResponse{Session: session})
	default:
		writeErrorMessage(w, http.StatusBadRequest, "provider_id or model_id is required")
	}
}

func (s *Server) persistSessionModelSelection(ctx context.Context, session core.Session) error {
	if s.setup == nil {
		return nil
	}
	providerID := strings.TrimSpace(session.ProviderID)
	modelID := strings.TrimSpace(session.ModelID)
	if providerID == "" || modelID == "" {
		return nil
	}
	if _, err := s.setup.ConfigureProviderContext(ctx, providerID, setup.ProviderSetupUpdate{Model: modelID}); err != nil {
		return err
	}
	return s.reloadSessionLLMRegistry(ctx)
}

func (s *Server) reloadSessionLLMRegistry(ctx context.Context) error {
	if s.adminReload == nil {
		return nil
	}
	if err := s.adminReload(ctx); err != nil {
		return err
	}
	s.markRuntimeReloaded()
	return nil
}

func shouldReloadSessionLLM(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not configured") ||
		strings.Contains(message, "provider registry unavailable")
}
