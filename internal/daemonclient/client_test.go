package daemonclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestClientLoadSnapshot(t *testing.T) {
	now := time.Now().UTC()
	session := core.Session{ID: "session-1", Title: "Test", Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now}
	binding := core.ClientBinding{Client: "tui", ExternalKey: "local", SessionID: session.ID, UpdatedAt: now}
	message := core.Message{
		ID:        "message-1",
		SessionID: session.ID,
		RunID:     "run-1",
		Role:      core.MessageRoleAssistant,
		Content:   "hello",
		Parts:     core.NormalizeMessageParts("hello", nil),
		CreatedAt: now,
	}
	run := core.Run{
		ID:            "run-1",
		SessionID:     session.ID,
		UserMessageID: "user-1",
		Status:        core.RunStatusCompleted,
		StartedAt:     now,
		UpdatedAt:     now,
		FinishedAt:    &now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/snapshot":
			if got := r.URL.Query().Get("client"); got != binding.Client {
				t.Fatalf("snapshot client = %q, want %q", got, binding.Client)
			}
			if got := r.URL.Query().Get("external_key"); got != binding.ExternalKey {
				t.Fatalf("snapshot external_key = %q, want %q", got, binding.ExternalKey)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"snapshot": core.ClientSnapshot{
				SessionID: session.ID,
				Session:   &session,
				Messages:  []core.Message{message},
				Run:       &run,
				Approvals: []core.Approval{{
					ID:        "approval-1",
					SessionID: session.ID,
					State:     core.ApprovalStatePending,
				}},
				Files: []core.FileSnapshot{{
					ID:        "file-1",
					SessionID: session.ID,
					Path:      "/tmp/a.txt",
					Content:   "hello",
					Version:   0,
					CreatedAt: now,
					UpdatedAt: now,
				}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, "tui", "local")
	snapshot, err := client.LoadSnapshot(context.Background())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if snapshot.SessionID != session.ID {
		t.Fatalf("snapshot.SessionID = %q, want %q", snapshot.SessionID, session.ID)
	}
	if snapshot.Session == nil || snapshot.Session.Title != session.Title {
		t.Fatalf("snapshot.Session = %#v, want title %q", snapshot.Session, session.Title)
	}
	if len(snapshot.Messages) != 1 {
		t.Fatalf("len(snapshot.Messages) = %d, want 1", len(snapshot.Messages))
	}
	if len(snapshot.Approvals) != 1 {
		t.Fatalf("len(snapshot.Approvals) = %d, want 1", len(snapshot.Approvals))
	}
	if len(snapshot.Files) != 1 || snapshot.Files[0].Path != "/tmp/a.txt" {
		t.Fatalf("snapshot.Files = %#v, want one file snapshot", snapshot.Files)
	}
	if snapshot.Run == nil || snapshot.Run.ID != run.ID {
		t.Fatalf("snapshot.Run = %#v, want run %q", snapshot.Run, run.ID)
	}
}

func TestNewClientUsesSeparateJSONAndEventHTTPClients(t *testing.T) {
	t.Parallel()

	client := New("http://127.0.0.1:8080", "tui", "local")
	if client.HTTPClient == nil {
		t.Fatal("HTTPClient is nil")
	}
	if client.HTTPClient.Timeout <= 0 {
		t.Fatalf("HTTPClient.Timeout = %s, want non-zero JSON timeout", client.HTTPClient.Timeout)
	}
	if client.EventHTTPClient != nil {
		t.Fatalf("EventHTTPClient = %#v, want nil default so SSE uses a non-timeboxed client", client.EventHTTPClient)
	}
}

func TestCompactSessionUsesLongDefaultJSONTimeout(t *testing.T) {
	t.Parallel()

	client := New("http://127.0.0.1:8080", "tui", "local")
	if client.compactHTTPClient() == client.HTTPClient {
		t.Fatal("compactHTTPClient() reused default short JSON client")
	}
	if got := client.compactHTTPClient().Timeout; got <= client.HTTPClient.Timeout {
		t.Fatalf("compact timeout = %s, want greater than JSON timeout %s", got, client.HTTPClient.Timeout)
	}

	custom := &http.Client{Timeout: time.Second}
	client.HTTPClient = custom
	if got := client.compactHTTPClient(); got != custom {
		t.Fatalf("compactHTTPClient() = %#v, want custom client", got)
	}
}

func TestVoiceRuntimeActionsUseLongDefaultJSONTimeout(t *testing.T) {
	t.Parallel()

	client := New("http://127.0.0.1:8080", "tui", "local")
	if client.voiceRuntimeHTTPClient() == client.HTTPClient {
		t.Fatal("voiceRuntimeHTTPClient() reused default short JSON client")
	}
	if got := client.voiceRuntimeHTTPClient().Timeout; got <= client.HTTPClient.Timeout {
		t.Fatalf("voice runtime timeout = %s, want greater than JSON timeout %s", got, client.HTTPClient.Timeout)
	}

	custom := &http.Client{Timeout: time.Second}
	client.HTTPClient = custom
	if got := client.voiceRuntimeHTTPClient(); got == custom {
		t.Fatal("voiceRuntimeHTTPClient() reused custom short-timeout client")
	} else if got.Timeout != defaultVoiceRuntimeTimeout {
		t.Fatalf("voiceRuntimeHTTPClient().Timeout = %s, want %s", got.Timeout, defaultVoiceRuntimeTimeout)
	}

	longCustom := &http.Client{Timeout: time.Hour}
	client.HTTPClient = longCustom
	if got := client.voiceRuntimeHTTPClient(); got != longCustom {
		t.Fatalf("voiceRuntimeHTTPClient() = %#v, want long custom client", got)
	}
}

func TestClientSendsAPIToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	client := New(server.URL, "tui", "local").WithAPIToken("test-token")
	if _, err := client.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
}

func TestClientRenameAndDeleteSession(t *testing.T) {
	now := time.Now().UTC()
	session := core.Session{ID: "session-1", Title: "Renamed", Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/sessions/session-1":
			_ = json.NewEncoder(w).Encode(core.SessionResponse{Session: session})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/sessions/session-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, "tui", "local")
	renamed, err := client.RenameSession(context.Background(), "session-1", "Renamed")
	if err != nil {
		t.Fatalf("RenameSession() error = %v", err)
	}
	if renamed.Title != "Renamed" {
		t.Fatalf("RenameSession().Title = %q, want %q", renamed.Title, "Renamed")
	}

	if err := client.DeleteSession(context.Background(), "session-1"); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
}

func TestClientCurrentBindingReturnsTypedNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/bindings/current" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "no active binding yet"})
	}))
	defer server.Close()

	client := New(server.URL, "tui", "local")
	_, err := client.CurrentBinding(context.Background())
	if err == nil {
		t.Fatal("CurrentBinding() error = nil, want not found")
	}
	if !IsAPIStatus(err, http.StatusNotFound) {
		t.Fatalf("CurrentBinding() error = %v, want typed 404", err)
	}
}

func TestClientLoadSnapshotDerivesToolStateFromApprovalsAndMessages(t *testing.T) {
	now := time.Now().UTC()
	session := core.Session{ID: "session-1", Title: "Test", Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now}
	binding := core.ClientBinding{Client: "tui", ExternalKey: "local", SessionID: session.ID, UpdatedAt: now}
	messages := []core.Message{{
		ID:        "msg-tool-call",
		SessionID: session.ID,
		RunID:     "run-1",
		Role:      core.MessageRoleAssistant,
		Parts: []core.MessagePart{{
			Kind: core.MessagePartKindToolCall,
			ToolCall: &core.ToolCallPart{
				ID:       "tool-1",
				Name:     "write",
				Input:    `{"file_path":"notes.txt"}`,
				Finished: false,
			},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}}
	run := core.Run{
		ID:            "run-1",
		SessionID:     session.ID,
		UserMessageID: "user-1",
		Status:        core.RunStatusFailed,
		StartedAt:     now,
		UpdatedAt:     now,
		FinishedAt:    &now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/snapshot":
			if got := r.URL.Query().Get("client"); got != binding.Client {
				t.Fatalf("snapshot client = %q, want %q", got, binding.Client)
			}
			if got := r.URL.Query().Get("external_key"); got != binding.ExternalKey {
				t.Fatalf("snapshot external_key = %q, want %q", got, binding.ExternalKey)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"snapshot": core.ClientSnapshot{
				SessionID:             session.ID,
				Session:               &session,
				Messages:              messages,
				Run:                   &run,
				ToolUpdates:           []core.ToolUpdate{{ToolCallID: "tool-1", ToolName: "write", State: core.ToolLifecycleFailed, Error: "approval denied"}},
				ApprovalNotifications: []core.PermissionNotification{{ApprovalID: "approval-1", ToolCallID: "tool-1", Denied: true}},
				Files:                 []core.FileSnapshot{},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, "tui", "local")
	snapshot, err := client.LoadSnapshot(context.Background())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if len(snapshot.Approvals) != 0 {
		t.Fatalf("len(snapshot.Approvals) = %d, want 0", len(snapshot.Approvals))
	}
	if len(snapshot.ToolUpdates) != 1 || snapshot.ToolUpdates[0].State != core.ToolLifecycleFailed {
		t.Fatalf("snapshot.ToolUpdates = %#v, want one failed tool update", snapshot.ToolUpdates)
	}
	if len(snapshot.ApprovalNotifications) != 1 || !snapshot.ApprovalNotifications[0].Denied {
		t.Fatalf("snapshot.ApprovalNotifications = %#v, want one denied notification", snapshot.ApprovalNotifications)
	}
}
