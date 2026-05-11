package api

import (
	"net/http"
	"strconv"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessionID := r.URL.Query().Get("session_id")
		limit := 50
		if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
			parsed, err := strconv.Atoi(rawLimit)
			if err != nil {
				writeErrorMessage(w, http.StatusBadRequest, "invalid limit")
				return
			}
			limit = parsed
		}

		messages, err := s.core.ListMessages(r.Context(), sessionID, limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.MessagesResponse{Messages: messages})
	case http.MethodPost:
		var input core.HandleMessageInput
		if !decodeJSONBody(w, r, &input) {
			return
		}

		result, err := s.core.AcceptRun(r.Context(), input)
		if err != nil {
			writeAcceptRunError(w, result, err)
			return
		}

		writeJSON(w, http.StatusAccepted, result)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}
