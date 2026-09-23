package api

import (
	"net/http"
	"strconv"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
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

		var messages []transcript.Message
		var err error
		if rawAfter := r.URL.Query().Get("after_seq"); rawAfter != "" {
			afterSeq, parseErr := strconv.ParseInt(rawAfter, 10, 64)
			if parseErr != nil {
				writeErrorMessage(w, http.StatusBadRequest, "invalid after_seq")
				return
			}
			messages, err = s.core.ListMessagesAfter(r.Context(), sessionID, afterSeq, limit)
		} else {
			messages, err = s.core.ListMessages(r.Context(), sessionID, limit)
		}
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
