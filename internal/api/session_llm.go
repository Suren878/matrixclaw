package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/sessionllm"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleSessionProviders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, core.SessionProvidersResponse{Providers: s.Core.SessionProviderOptions()})
}

func (s *Server) handleSessionLLMModels(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	providerID, modelID, models, err := s.Core.ModelsForSession(r.Context(), sessionID)
	if shouldReloadSessionLLM(err) {
		if reloadErr := s.reloadSessionLLMRegistry(r.Context()); reloadErr != nil {
			writeErrorMessage(w, http.StatusInternalServerError, "reload provider registry: "+reloadErr.Error())
			return
		}
		providerID, modelID, models, err = s.Core.ModelsForSession(r.Context(), sessionID)
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

func (s *Server) handleSessionLLMUpdate(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	var req core.UpdateSessionLLMRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	switch {
	case strings.TrimSpace(req.ProviderID) != "":
		session, err := s.Core.UpdateSessionProvider(r.Context(), sessionID, req.ProviderID)
		if shouldReloadSessionLLM(err) {
			if reloadErr := s.reloadSessionLLMRegistry(r.Context()); reloadErr != nil {
				writeErrorMessage(w, http.StatusInternalServerError, "reload provider registry: "+reloadErr.Error())
				return
			}
			session, err = s.Core.UpdateSessionProvider(r.Context(), sessionID, req.ProviderID)
		}
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionResponse{Session: session})
	case strings.TrimSpace(req.ModelID) != "":
		session, err := s.Core.UpdateSessionModel(r.Context(), sessionID, req.ModelID)
		if shouldReloadSessionLLM(err) {
			if reloadErr := s.reloadSessionLLMRegistry(r.Context()); reloadErr != nil {
				writeErrorMessage(w, http.StatusInternalServerError, "reload provider registry: "+reloadErr.Error())
				return
			}
			session, err = s.Core.UpdateSessionModel(r.Context(), sessionID, req.ModelID)
		}
		if err != nil {
			writeError(w, err)
			return
		}
		if roleOf(r) == core.RoleOwner {
			if err := s.persistSessionModelSelection(r.Context(), session); err != nil {
				writeError(w, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, core.SessionResponse{Session: session})
	default:
		writeErrorMessage(w, http.StatusBadRequest, "provider_id or model_id is required")
	}
}

func (s *Server) persistSessionModelSelection(ctx context.Context, session core.Session) error {
	providerID := strings.TrimSpace(session.ProviderID)
	modelID := strings.TrimSpace(session.ModelID)
	if providerID == "" || modelID == "" {
		return nil
	}
	if _, err := s.Setup.ConfigureProvider(providerID, setup.ProviderSetupUpdate{Model: &modelID}); err != nil {
		return err
	}
	return s.reloadSessionLLMRegistry(ctx)
}

func (s *Server) reloadSessionLLMRegistry(ctx context.Context) error {
	if err := s.Reload(ctx); err != nil {
		return err
	}
	s.markRuntimeReloaded()
	return nil
}

func shouldReloadSessionLLM(err error) bool {
	return errors.Is(err, sessionllm.ErrProviderNotConfigured) ||
		errors.Is(err, sessionllm.ErrNoActiveProvider) ||
		errors.Is(err, core.ErrExecutionUnavailable)
}
