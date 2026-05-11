package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestMonitorRunSendsAssistantMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/snapshot":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"snapshot": core.ClientSnapshot{
					SessionID: "session_1",
					Messages: []core.Message{
						{ID: "msg_a", SessionID: "session_1", RunID: "run_1", Role: core.MessageRoleAssistant, Content: "Done"},
					},
					Run: &core.Run{ID: "run_1", SessionID: "session_1", Status: core.RunStatusCompleted},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: ready\n")
			fmt.Fprint(w, "data: {\"session_id\":\"session_1\"}\n\n")
		default:
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, server.URL)
	state := &runDeliveryState{
		assistant: map[string]sentAssistantMessage{},
		approvals: map[string]int64{},
	}

	err := worker.monitorRun(context.Background(), chatTarget{
		chatID:      42,
		externalKey: "42",
	}, "session_1", "run_1", state)
	if err != nil {
		t.Fatalf("monitorRun() error = %v", err)
	}
	if len(api.sendMessageRequests) != 1 {
		t.Fatalf("sendMessageRequests len = %d, want 1", len(api.sendMessageRequests))
	}
	if api.sendMessageRequests[0].Text != "Done" {
		t.Fatalf("assistant text = %q, want %q", api.sendMessageRequests[0].Text, "Done")
	}
}

func TestMonitorRunRendersSSEAssistantUpdates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/snapshot":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"snapshot": core.ClientSnapshot{
					SessionID: "session_1",
					Run:       &core.Run{ID: "run_1", SessionID: "session_1", Status: core.RunStatusRunning},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: ready\n")
			fmt.Fprint(w, "data: {\"session_id\":\"session_1\"}\n\n")
			writeTestEvent(t, w, core.Event{
				ID:        1,
				Type:      core.EventMessageCreated,
				SessionID: "session_1",
				RunID:     "run_1",
				Payload: core.Message{
					ID:        "msg_a",
					SessionID: "session_1",
					RunID:     "run_1",
					Role:      core.MessageRoleAssistant,
					Content:   "Hel",
				},
			})
			writeTestEvent(t, w, core.Event{
				ID:        2,
				Type:      core.EventMessageUpdated,
				SessionID: "session_1",
				RunID:     "run_1",
				Payload: core.Message{
					ID:        "msg_a",
					SessionID: "session_1",
					RunID:     "run_1",
					Role:      core.MessageRoleAssistant,
					Content:   "Hello",
				},
			})
			writeTestEvent(t, w, core.Event{
				ID:        3,
				Type:      core.EventRunUpdated,
				SessionID: "session_1",
				RunID:     "run_1",
				Payload: core.Run{
					ID:        "run_1",
					SessionID: "session_1",
					Status:    core.RunStatusCompleted,
				},
			})
		default:
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, server.URL)
	worker.config.StreamFlushInterval = 10 * time.Millisecond
	state := &runDeliveryState{
		assistant: map[string]sentAssistantMessage{},
		approvals: map[string]int64{},
	}

	err := worker.monitorRun(context.Background(), chatTarget{
		chatID:      42,
		externalKey: "42",
	}, "session_1", "run_1", state)
	if err != nil {
		t.Fatalf("monitorRun() error = %v", err)
	}
	if len(api.sendMessageRequests) != 1 {
		t.Fatalf("sendMessageRequests len = %d, want 1", len(api.sendMessageRequests))
	}
	if api.sendMessageRequests[0].Text != "Hello" {
		t.Fatalf("assistant text = %q, want %q", api.sendMessageRequests[0].Text, "Hello")
	}
}

func writeTestEvent(t *testing.T, w http.ResponseWriter, event core.Event) {
	t.Helper()
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(w, "id: %d\n", event.ID)
	fmt.Fprintf(w, "event: %s\n", event.Type)
	fmt.Fprintf(w, "data: %s\n\n", body)
}
