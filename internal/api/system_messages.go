package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionSystemMessage(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var req core.CreateSystemMessageRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	message, err := s.core.CreateSystemMessage(r.Context(), sessionID, req.Content)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, core.MessageResponse{Message: message})
}
