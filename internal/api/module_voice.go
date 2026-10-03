package api

import (
	"errors"
	"net/http"

	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
)

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
