package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/sessionllm"
)

func (s *Server) handleSessionProviders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, core.SessionProvidersResponse{Providers: s.Core.SessionProviderOptions()})
}

func (s *Server) handleSessionLLMModels(w http.ResponseWriter, r *http.Request) {
	sessionID, providerID := r.PathValue("id"), r.URL.Query().Get("provider")
	response, err := s.Core.ModelsForSession(r.Context(), sessionID, providerID)
	if shouldReloadSessionLLM(err) {
		if reloadErr := s.reloadSessionLLMRegistry(r.Context()); reloadErr != nil {
			writeError(w, fmt.Errorf("reload provider registry: %w", reloadErr))
			return
		}
		response, err = s.Core.ModelsForSession(r.Context(), sessionID, providerID)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSessionLLMUpdate(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	var req core.UpdateSessionLLMRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ModelID) == "" {
		writeErrorMessage(w, http.StatusBadRequest, "model_id is required")
		return
	}
	session, err := s.Core.UpdateSessionLLM(r.Context(), sessionID, req.ProviderID, req.ModelID)
	if shouldReloadSessionLLM(err) {
		if reloadErr := s.reloadSessionLLMRegistry(r.Context()); reloadErr != nil {
			writeError(w, fmt.Errorf("reload provider registry: %w", reloadErr))
			return
		}
		session, err = s.Core.UpdateSessionLLM(r.Context(), sessionID, req.ProviderID, req.ModelID)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionResponse{Session: session})
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
