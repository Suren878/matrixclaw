package api

import (
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
		writeError(w, withStatus(http.StatusBadGateway, err))
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
		writeError(w, withStatus(http.StatusBadGateway, err))
		return
	}
	writeJSON(w, http.StatusOK, response)
}
