package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (s *Server) voiceModule(w http.ResponseWriter, id string) *voicemodule.Module {
	switch id {
	case setup.VoiceModuleTTS:
		return s.Modules.TTS
	case setup.VoiceModuleSTT:
		return s.Modules.STT
	default:
		writeErrorMessage(w, http.StatusNotFound, "voice module not found")
		return nil
	}
}

func (s *Server) voiceDescriptors() []setup.VoiceModuleDescriptor {
	return []setup.VoiceModuleDescriptor{s.Modules.TTS.Descriptor(), s.Modules.STT.Descriptor()}
}

func (s *Server) handleVoiceModules(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, setup.VoiceModulesResponse{Modules: s.voiceDescriptors()})
}

func (s *Server) handleVoiceModuleUpdate(w http.ResponseWriter, r *http.Request) {
	moduleID := r.PathValue("module")
	if s.voiceModule(w, moduleID) == nil {
		return
	}
	var update setup.VoiceModuleUpdate
	if !decodeJSON(w, r, &update) {
		return
	}
	if _, err := s.Setup.UpdateVoiceModule(moduleID, update); err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.reload(w, r.Context()) {
		return
	}
	writeJSON(w, http.StatusOK, setup.VoiceModulesResponse{Modules: s.voiceDescriptors()})
}

func (s *Server) handleVoiceProviderAction(w http.ResponseWriter, r *http.Request) {
	module := s.voiceModule(w, r.PathValue("module"))
	if module == nil {
		return
	}
	providerID := r.PathValue("provider")
	var request setup.VoiceProviderActionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	updated, err := module.Action(r.Context(), providerID, request)
	if err != nil {
		writeVoiceError(w, err)
		return
	}
	if strings.EqualFold(strings.TrimSpace(request.Action), localruntime.ActionDownload) {
		if persisted, err := s.persistDownloadedVoiceModel(r.Context(), module.ID(), providerID, request.ModelID, updated); err != nil {
			writeVoiceError(w, err)
			return
		} else if persisted {
			if decorated, found := findVoiceProvider(module.Descriptor(), providerID); found {
				updated = decorated
			}
		}
	}
	writeJSON(w, http.StatusOK, setup.VoiceProviderActionResponse{Provider: updated})
}

// persistDownloadedVoiceModel selects a model the user just downloaded.
func (s *Server) persistDownloadedVoiceModel(ctx context.Context, moduleID string, providerID string, modelID string, provider setup.VoiceProviderOption) (bool, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return false, nil
	}
	cfg := provider.Config
	switch moduleID {
	case setup.VoiceModuleTTS:
		if providerID != "piper" && providerID != "supertonic" {
			return false, nil
		}
		cfg.VoiceID = modelID
		if providerID == "piper" {
			cfg.Language = voiceLanguageFromVoiceID(modelID)
		}
	case setup.VoiceModuleSTT:
		if providerID != "whispercpp" {
			return false, nil
		}
		cfg.ModelID = modelID
	default:
		return false, nil
	}
	if _, err := s.Setup.UpdateVoiceModule(moduleID, setup.VoiceModuleUpdate{ProviderID: providerID, ProviderConfig: &cfg}); err != nil {
		return false, err
	}
	return true, s.Reload(ctx)
}

func findVoiceProvider(module setup.VoiceModuleDescriptor, providerID string) (setup.VoiceProviderOption, bool) {
	for _, provider := range module.Providers {
		if provider.ID == providerID {
			return provider, true
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
	var request voicemodule.TextToSpeechRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	response, err := s.Modules.TTS.TextToSpeech(r.Context(), request)
	if err != nil {
		writeVoiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSpeechToText(w http.ResponseWriter, r *http.Request) {
	var request voicemodule.SpeechToTextRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	response, err := s.Modules.STT.SpeechToText(r.Context(), request)
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
	case errors.Is(err, voicemodule.ErrModuleDisabled),
		errors.Is(err, voicemodule.ErrProviderUnavailable):
		writeErrorMessage(w, http.StatusConflict, message)
	case errors.Is(err, voicemodule.ErrInvalidRequest),
		errors.Is(err, voicemodule.ErrUnsupportedProvider):
		writeErrorMessage(w, http.StatusBadRequest, message)
	default:
		writeErrorMessage(w, http.StatusBadGateway, message)
	}
}
