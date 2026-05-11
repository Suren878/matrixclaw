package daemonclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestSubscribeEventsFlushesFinalEventAtEOF(t *testing.T) {
	event := core.Event{
		ID:        7,
		Type:      core.EventRunUpdated,
		SessionID: "session-1",
		RunID:     "run-1",
		At:        time.Now().UTC(),
		Payload:   core.Run{ID: "run-1", SessionID: "session-1", Status: core.RunStatusRunning},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		body, _ := json.Marshal(event)
		fmt.Fprint(w, "id: 7\n")
		fmt.Fprint(w, "event: run.updated\n")
		fmt.Fprintf(w, "data: %s\n", body)
	}))
	defer server.Close()

	client := New(server.URL, "test", "local")
	events, errs, err := client.SubscribeEvents(context.Background(), "session-1", 0)
	if err != nil {
		t.Fatalf("SubscribeEvents() error = %v", err)
	}

	select {
	case got := <-events:
		if got.ID != 7 || got.Type != core.EventRunUpdated {
			t.Fatalf("event = %#v, want id 7 run.updated", got)
		}
	case err := <-errs:
		t.Fatalf("SubscribeEvents() async error = %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for final SSE event")
	}
}

func TestSubscribeEventsDecodesErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad session"}`))
	}))
	defer server.Close()

	client := New(server.URL, "test", "local")
	_, _, err := client.SubscribeEvents(context.Background(), "session-1", 0)
	if err == nil {
		t.Fatal("SubscribeEvents() error = nil, want APIError")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T, want APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest || !strings.Contains(apiErr.Message, "bad session") {
		t.Fatalf("APIError = %#v, want decoded status and message", apiErr)
	}
}
