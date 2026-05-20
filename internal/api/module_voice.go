package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) handleVoiceModules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	modules, err := s.setup.VoiceModules()
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	modules = localruntime.New("").DecorateVoiceModules(modules)
	writeJSON(w, http.StatusOK, setup.VoiceModulesResponse{Modules: modules})
}

func (s *Server) handleVoiceModuleByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/modules/voice/"), "/")
	moduleID, suffix, _ := strings.Cut(rest, "/")
	if moduleID == "" {
		writeErrorMessage(w, http.StatusBadRequest, "voice module id is required")
		return
	}
	if strings.HasPrefix(suffix, "providers/") {
		s.handleVoiceProvider(w, r, moduleID, strings.TrimPrefix(suffix, "providers/"))
		return
	}
	if r.Method == http.MethodPost {
		switch moduleID {
		case setup.VoiceModuleTTS:
			s.handleTextToSpeech(w, r)
			return
		case setup.VoiceModuleSTT:
			s.handleSpeechToText(w, r)
			return
		}
	}
	if r.Method != http.MethodPatch {
		writeMethodNotAllowed(w, http.MethodPatch, http.MethodPost)
		return
	}
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	var update setup.VoiceModuleUpdate
	if !decodeJSONBody(w, r, &update) {
		return
	}
	modules, err := s.setup.UpdateVoiceModule(moduleID, update)
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
	modules = localruntime.New("").DecorateVoiceModules(modules)
	writeJSON(w, http.StatusOK, setup.VoiceModulesResponse{Modules: modules})
}

func (s *Server) handleVoiceProvider(w http.ResponseWriter, r *http.Request, moduleID string, suffix string) {
	providerID, actionSuffix, _ := strings.Cut(strings.Trim(suffix, "/"), "/")
	if providerID == "" {
		writeErrorMessage(w, http.StatusBadRequest, "voice provider id is required")
		return
	}
	if actionSuffix != "action" {
		writeNotFound(w)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	var request setup.VoiceProviderActionRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	modules, err := s.setup.VoiceModules()
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	provider, ok := findVoiceProvider(modules, moduleID, providerID)
	if !ok {
		writeErrorMessage(w, http.StatusNotFound, "voice provider not found")
		return
	}
	updated, err := localruntime.New("").ApplyVoiceAction(r.Context(), moduleID, provider, request)
	if err != nil {
		writeVoiceError(w, err)
		return
	}
	if strings.EqualFold(strings.TrimSpace(request.Action), localruntime.ActionDownload) {
		if modules, ok, err := s.persistDownloadedVoiceModel(r.Context(), moduleID, providerID, request.ModelID, updated); err != nil {
			writeVoiceError(w, err)
			return
		} else if ok {
			if decorated, found := findVoiceProvider(localruntime.New("").DecorateVoiceModules(modules), moduleID, providerID); found {
				updated = decorated
			}
		}
	}
	writeJSON(w, http.StatusOK, setup.VoiceProviderActionResponse{Provider: updated})
}

func (s *Server) persistDownloadedVoiceModel(ctx context.Context, moduleID string, providerID string, modelID string, provider setup.VoiceProviderOption) ([]setup.VoiceModuleDescriptor, bool, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil, false, nil
	}
	cfg := provider.Config
	switch moduleID {
	case setup.VoiceModuleTTS:
		if providerID != "piper" {
			return nil, false, nil
		}
		cfg.VoiceID = modelID
		cfg.Language = voiceLanguageFromVoiceID(modelID)
	case setup.VoiceModuleSTT:
		if providerID != "whispercpp" {
			return nil, false, nil
		}
		cfg.ModelID = modelID
	default:
		return nil, false, nil
	}
	modules, err := s.setup.UpdateVoiceModule(moduleID, setup.VoiceModuleUpdate{ProviderID: providerID, ProviderConfig: &cfg})
	if err != nil {
		return nil, false, err
	}
	if s.adminReload != nil {
		if err := s.adminReload(ctx); err != nil {
			return nil, false, err
		}
	}
	return modules, true, nil
}

func findVoiceProvider(modules []setup.VoiceModuleDescriptor, moduleID string, providerID string) (setup.VoiceProviderOption, bool) {
	for _, module := range modules {
		if module.ID != moduleID {
			continue
		}
		for _, provider := range module.Providers {
			if provider.ID == providerID {
				return provider, true
			}
		}
	}
	return setup.VoiceProviderOption{}, false
}

func voiceLanguageFromVoiceID(voiceID string) string {
	voiceID = strings.TrimSpace(voiceID)
	if before, _, ok := strings.Cut(voiceID, "-"); ok {
		return before
	}
	return ""
}

func (s *Server) handleTextToSpeech(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	var request voicemodule.TextToSpeechRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	response, err := voicemodule.NewService(s.setup).TextToSpeech(r.Context(), request)
	if err != nil {
		writeVoiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSpeechToText(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	var request voicemodule.SpeechToTextRequest
	if !decodeJSONBodyLimit(w, r, &request, voiceAudioJSONBodyLimitBytes) {
		return
	}
	response, err := voicemodule.NewService(s.setup).SpeechToText(r.Context(), request)
	if err != nil {
		writeVoiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func writeVoiceError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "disabled"):
		writeErrorMessage(w, http.StatusConflict, message)
	case strings.Contains(message, "not installed"), strings.Contains(message, "not available"):
		writeErrorMessage(w, http.StatusConflict, message)
	case strings.Contains(message, "required"), strings.Contains(message, "invalid"), strings.Contains(message, "not supported"):
		writeErrorMessage(w, http.StatusBadRequest, message)
	default:
		writeErrorMessage(w, http.StatusBadGateway, message)
	}
}
