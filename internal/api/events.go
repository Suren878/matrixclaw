package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

type eventReadyResponse struct {
	SessionID string `json:"session_id"`
	AfterID   uint64 `json:"after_id"`
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeErrorMessage(w, http.StatusBadRequest, "session_id is required")
		return
	}
	afterID, err := parseSSEAfterID(r)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, "invalid after id")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorMessage(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	events := s.core.SubscribeEventsAfter(r.Context(), sessionID, afterID)
	writeSSE(w, "ready", eventReadyResponse{SessionID: sessionID, AfterID: afterID})
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			writeSSE(w, string(event.Type), event)
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, eventType string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		body, _ = json.Marshal(core.Event{Type: core.EventMessageUpdated, Payload: core.ErrorResponse{Error: err.Error()}})
	}
	if event, ok := payload.(core.Event); ok && event.ID > 0 {
		fmt.Fprintf(w, "id: %d\n", event.ID)
	}
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", body)
}

func parseSSEAfterID(r *http.Request) (uint64, error) {
	if value := strings.TrimSpace(r.URL.Query().Get("after")); value != "" {
		return strconv.ParseUint(value, 10, 64)
	}
	if value := strings.TrimSpace(r.Header.Get("Last-Event-ID")); value != "" {
		return strconv.ParseUint(value, 10, 64)
	}
	return 0, nil
}
