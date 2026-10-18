package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

type eventReadyResponse struct {
	SessionID string `json:"session_id"`
	AfterID   uint64 `json:"after_id"`
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
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

	events := s.Core.SubscribeEventsAfter(r.Context(), sessionID, afterID)
	if writeSSE(w, "ready", eventReadyResponse{SessionID: sessionID, AfterID: afterID}) != nil {
		return
	}
	flusher.Flush()

	// The stream ends when the bus closes it (the subscriber fell behind) or an
	// event cannot be written; the client then reconnects and reloads.
	keepalive := time.NewTicker(sseKeepalive)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
		case event, ok := <-events:
			if !ok {
				return
			}
			if writeSSE(w, string(event.Type), event) != nil {
				return
			}
		}
		flusher.Flush()
	}
}

// sseKeepalive is how often the event stream sends a comment line, so proxies
// and clients see it alive while no event comes.
const sseKeepalive = 20 * time.Second

// writeSSE writes one event; one that cannot be encoded is logged and ends
// the stream, so the client reloads instead of missing it.
func writeSSE(w io.Writer, eventType string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("api: encode %s event: %v", eventType, err)
		return err
	}
	var id string
	if event, ok := payload.(core.Event); ok && event.ID > 0 {
		id = fmt.Sprintf("id: %d\n", event.ID)
	}
	_, err = fmt.Fprintf(w, "%sevent: %s\ndata: %s\n\n", id, eventType, body)
	return err
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
