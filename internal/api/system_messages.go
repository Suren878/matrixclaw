package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionSystemMessage(w http.ResponseWriter, r *http.Request) {
	var req core.CreateSystemMessageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	message, err := s.Core.CreateSystemMessage(r.Context(), r.PathValue("id"), req.Content)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, core.MessageResponse{Message: message})
}
