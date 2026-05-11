package clientruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func TestControlplaneRuntimeRestartNotificationUsesRuntimeClientIdentity(t *testing.T) {
	var got core.AdminRestartRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/admin/restart" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(core.OKResponse{OK: true})
	}))
	defer server.Close()

	rt := ControlplaneRuntime{
		Client:      " terminal:alias ",
		ExternalKey: " local-alias ",
		Daemon: func(externalKey string) (*daemonclient.Client, error) {
			return daemonclient.New(server.URL, "terminal:alias", externalKey), nil
		},
	}

	if err := rt.RestartDaemonWithNotification(context.Background()); err != nil {
		t.Fatalf("RestartDaemonWithNotification() error = %v", err)
	}
	if got.Notification == nil {
		t.Fatal("notification is nil")
	}
	if got.Notification.Client != "terminal:alias" {
		t.Fatalf("notification client = %q, want terminal:alias", got.Notification.Client)
	}
	if got.Notification.ExternalKey != "local-alias" {
		t.Fatalf("notification external_key = %q, want local-alias", got.Notification.ExternalKey)
	}
}

func TestStateTracksMessagesAndFiles(t *testing.T) {
	now := time.Now().UTC()
	model := NewState(core.ClientSnapshot{
		SessionID: "session-1",
		Messages: []core.Message{{
			ID:        "msg-1",
			SessionID: "session-1",
			Role:      core.MessageRoleUser,
			Content:   "hello",
			Parts:     core.NormalizeMessageParts("hello", nil),
			CreatedAt: now,
			UpdatedAt: now,
		}},
		Files: []core.FileSnapshot{{
			ID:        "file-1",
			SessionID: "session-1",
			Path:      "/tmp/a.txt",
			Content:   "one",
			Version:   0,
			CreatedAt: now,
			UpdatedAt: now,
		}},
	})

	messagePayload, _ := json.Marshal(core.Message{
		ID:        "msg-2",
		SessionID: "session-1",
		Role:      core.MessageRoleAssistant,
		Content:   "done",
		Parts:     core.NormalizeMessageParts("done", nil),
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err := model.Apply(daemonclient.LiveEvent{
		Type:      core.EventMessageCreated,
		SessionID: "session-1",
		Payload:   messagePayload,
	}); err != nil {
		t.Fatalf("Apply(message.created) error = %v", err)
	}

	filePayload, _ := json.Marshal(core.FileSnapshot{
		ID:        "file-2",
		SessionID: "session-1",
		Path:      "/tmp/a.txt",
		Content:   "two",
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err := model.Apply(daemonclient.LiveEvent{
		Type:      core.EventFileVersioned,
		SessionID: "session-1",
		Payload:   filePayload,
	}); err != nil {
		t.Fatalf("Apply(file.versioned) error = %v", err)
	}

	snapshot := model.Snapshot()
	if len(snapshot.Messages) != 2 {
		t.Fatalf("len(snapshot.Messages) = %d, want 2", len(snapshot.Messages))
	}
	if snapshot.Messages[1].Role != core.MessageRoleAssistant {
		t.Fatalf("snapshot.Messages[1].Role = %q, want assistant", snapshot.Messages[1].Role)
	}
	if len(snapshot.Files) != 2 {
		t.Fatalf("len(snapshot.Files) = %d, want 2", len(snapshot.Files))
	}
}

func TestStateApprovalLifecycleTracksResolvedNotifications(t *testing.T) {
	now := time.Now().UTC()
	model := NewState(core.ClientSnapshot{SessionID: "session-1"})

	requestPayload, _ := json.Marshal(core.PermissionRequest{
		ID:         "approval-1",
		SessionID:  "session-1",
		ToolCallID: "tool-1",
		ToolName:   "write",
		Action:     "write",
	})
	if err := model.Apply(daemonclient.LiveEvent{
		Type:      core.EventApprovalRequest,
		SessionID: "session-1",
		Payload:   requestPayload,
		At:        now,
	}); err != nil {
		t.Fatalf("Apply(approval.requested) error = %v", err)
	}

	resolvedPayload, _ := json.Marshal(core.PermissionNotification{
		ApprovalID: "approval-1",
		ToolCallID: "tool-1",
		Denied:     true,
	})
	if err := model.Apply(daemonclient.LiveEvent{
		Type:      core.EventApprovalResult,
		SessionID: "session-1",
		Payload:   resolvedPayload,
		At:        now.Add(time.Second),
	}); err != nil {
		t.Fatalf("Apply(approval.resolved) error = %v", err)
	}

	snapshot := model.Snapshot()
	if len(snapshot.Approvals) != 0 {
		t.Fatalf("len(snapshot.Approvals) = %d, want 0", len(snapshot.Approvals))
	}
	if len(snapshot.ApprovalNotifications) != 1 || !snapshot.ApprovalNotifications[0].Denied {
		t.Fatalf("snapshot.ApprovalNotifications = %#v, want denied notification", snapshot.ApprovalNotifications)
	}
}

func TestStateIgnoresDifferentSessionEventAndDeduplicatesFiles(t *testing.T) {
	now := time.Now().UTC()
	model := NewState(core.ClientSnapshot{
		SessionID: "session-1",
		Files: []core.FileSnapshot{{
			ID:        "file-1",
			SessionID: "session-1",
			Path:      "/tmp/a.txt",
			Content:   "one",
			Version:   0,
			CreatedAt: now,
			UpdatedAt: now,
		}},
	})

	otherMessage, _ := json.Marshal(core.Message{
		ID:        "msg-2",
		SessionID: "session-2",
		Role:      core.MessageRoleAssistant,
		Content:   "other",
	})
	if err := model.Apply(daemonclient.LiveEvent{
		Type:      core.EventMessageCreated,
		SessionID: "session-2",
		Payload:   otherMessage,
	}); err != nil {
		t.Fatalf("Apply(different session) error = %v", err)
	}

	duplicateFile, _ := json.Marshal(core.FileSnapshot{
		ID:        "file-1",
		SessionID: "session-1",
		Path:      "/tmp/a.txt",
		Content:   "one",
		Version:   0,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err := model.Apply(daemonclient.LiveEvent{
		Type:      core.EventFileVersioned,
		SessionID: "session-1",
		Payload:   duplicateFile,
	}); err != nil {
		t.Fatalf("Apply(file.versioned duplicate) error = %v", err)
	}

	snapshot := model.Snapshot()
	if len(snapshot.Messages) != 0 {
		t.Fatalf("len(snapshot.Messages) = %d, want 0", len(snapshot.Messages))
	}
	if len(snapshot.Files) != 1 {
		t.Fatalf("len(snapshot.Files) = %d, want 1", len(snapshot.Files))
	}
}

func TestStateSnapshotUsesStableOrdering(t *testing.T) {
	model := NewState(core.ClientSnapshot{
		SessionID: "session-1",
		ToolUpdates: []core.ToolUpdate{
			{ToolCallID: "tool-b"},
			{ToolCallID: "tool-a"},
		},
		Approvals: []core.Approval{
			{ID: "approval-b", ToolCallRef: "tool-b"},
			{ID: "approval-a", ToolCallRef: "tool-a"},
		},
		ApprovalNotifications: []core.PermissionNotification{
			{ToolCallID: "tool-b"},
			{ToolCallID: "tool-a"},
		},
		Files: []core.FileSnapshot{
			{ID: "file-b2", Path: "/tmp/b.txt", Version: 2},
			{ID: "file-a1", Path: "/tmp/a.txt", Version: 1},
			{ID: "file-b1", Path: "/tmp/b.txt", Version: 1},
		},
	})

	snapshot := model.Snapshot()
	if got := []string{snapshot.ToolUpdates[0].ToolCallID, snapshot.ToolUpdates[1].ToolCallID}; got[0] != "tool-a" || got[1] != "tool-b" {
		t.Fatalf("tool update order = %#v, want tool-a/tool-b", got)
	}
	if got := []string{snapshot.Approvals[0].ID, snapshot.Approvals[1].ID}; got[0] != "approval-a" || got[1] != "approval-b" {
		t.Fatalf("approval order = %#v, want approval-a/approval-b", got)
	}
	if got := []string{snapshot.ApprovalNotifications[0].ToolCallID, snapshot.ApprovalNotifications[1].ToolCallID}; got[0] != "tool-a" || got[1] != "tool-b" {
		t.Fatalf("notification order = %#v, want tool-a/tool-b", got)
	}
	if got := []string{snapshot.Files[0].ID, snapshot.Files[1].ID, snapshot.Files[2].ID}; got[0] != "file-a1" || got[1] != "file-b1" || got[2] != "file-b2" {
		t.Fatalf("file order = %#v, want path then version order", got)
	}
}
