package viewmodel

import (
	"encoding/json"
	"testing"
	"time"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func TestReadModelTracksMessagesAndFiles(t *testing.T) {
	now := time.Now().UTC()
	model := NewReadModel(core.ClientSnapshot{
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
		Model:     "gpt-test",
		Provider:  "openai-compatible",
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
	if snapshot.Messages[1].Role != surfacemessage.Assistant {
		t.Fatalf("snapshot.Messages[1].Role = %q, want %q", snapshot.Messages[1].Role, surfacemessage.Assistant)
	}
	if len(snapshot.Files) != 2 {
		t.Fatalf("len(snapshot.Files) = %d, want 2", len(snapshot.Files))
	}
}

func TestReadModelDeduplicatesFileVersions(t *testing.T) {
	now := time.Now().UTC()
	model := NewReadModel(core.ClientSnapshot{
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

	filePayload, _ := json.Marshal(core.FileSnapshot{
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
		Payload:   filePayload,
	}); err != nil {
		t.Fatalf("Apply(file.versioned duplicate) error = %v", err)
	}

	snapshot := model.Snapshot()
	if len(snapshot.Files) != 1 {
		t.Fatalf("len(snapshot.Files) = %d, want 1", len(snapshot.Files))
	}
}

func TestReadModelIgnoresDifferentSessionEvent(t *testing.T) {
	now := time.Now().UTC()
	model := NewReadModel(core.ClientSnapshot{
		SessionID: "session-1",
		Messages: []core.Message{{
			ID:        "msg-1",
			SessionID: "session-1",
			Role:      core.MessageRoleAssistant,
			Content:   "hello",
			Parts:     core.NormalizeMessageParts("hello", nil),
			CreatedAt: now,
			UpdatedAt: now,
		}},
	})

	messagePayload, _ := json.Marshal(core.Message{
		ID:        "msg-2",
		SessionID: "session-2",
		Role:      core.MessageRoleAssistant,
		Content:   "other",
		Parts:     core.NormalizeMessageParts("other", nil),
		CreatedAt: now.Add(time.Second),
		UpdatedAt: now.Add(time.Second),
	})
	if err := model.Apply(daemonclient.LiveEvent{
		Type:      core.EventMessageCreated,
		SessionID: "session-2",
		Payload:   messagePayload,
	}); err != nil {
		t.Fatalf("Apply(message.created different session) error = %v", err)
	}

	if got := len(model.Snapshot().Messages); got != 1 {
		t.Fatalf("len(snapshot.Messages) = %d, want 1", got)
	}
}

func TestReadModelApprovalLifecycleTracksResolvedNotifications(t *testing.T) {
	now := time.Now().UTC()
	model := NewReadModel(core.ClientSnapshot{SessionID: "session-1"})

	requestPayload, _ := json.Marshal(core.PermissionRequest{
		ID:          "approval-1",
		SessionID:   "session-1",
		ToolCallID:  "tool-1",
		ToolName:    "write",
		Description: "Write file",
		Action:      "write",
		Path:        "/tmp/notes.txt",
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
	if len(snapshot.ApprovalNotifications) != 1 {
		t.Fatalf("len(snapshot.ApprovalNotifications) = %d, want 1", len(snapshot.ApprovalNotifications))
	}
	got := snapshot.ApprovalNotifications[0]
	want := surfacepermission.PermissionNotification{ToolCallID: "tool-1", Denied: true}
	if got != want {
		t.Fatalf("snapshot.ApprovalNotifications[0] = %#v, want %#v", got, want)
	}

	reRequestPayload, _ := json.Marshal(core.PermissionRequest{
		ID:         "approval-2",
		SessionID:  "session-1",
		ToolCallID: "tool-1",
		ToolName:   "write",
		Action:     "write",
		Path:       "/tmp/notes.txt",
	})
	if err := model.Apply(daemonclient.LiveEvent{
		Type:      core.EventApprovalRequest,
		SessionID: "session-1",
		Payload:   reRequestPayload,
		At:        now.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("Apply(second approval.requested) error = %v", err)
	}

	snapshot = model.Snapshot()
	if len(snapshot.ApprovalNotifications) != 0 {
		t.Fatalf("len(snapshot.ApprovalNotifications) = %d, want 0 after new request", len(snapshot.ApprovalNotifications))
	}
	if len(snapshot.Approvals) != 1 {
		t.Fatalf("len(snapshot.Approvals) = %d, want 1 after new request", len(snapshot.Approvals))
	}
}

func TestReadModelTracksToolUpdates(t *testing.T) {
	model := NewReadModel(core.ClientSnapshot{SessionID: "session-1"})

	payload, _ := json.Marshal(core.ToolUpdate{
		ToolCallID: "tool-1",
		ToolName:   "bash",
		State:      core.ToolLifecycleFailed,
		Error:      "boom",
	})
	if err := model.Apply(daemonclient.LiveEvent{
		Type:      core.EventToolUpdated,
		SessionID: "session-1",
		Payload:   payload,
	}); err != nil {
		t.Fatalf("Apply(tool.updated) error = %v", err)
	}

	snapshot := model.Snapshot()
	if len(snapshot.ToolUpdates) != 1 {
		t.Fatalf("len(snapshot.ToolUpdates) = %d, want 1", len(snapshot.ToolUpdates))
	}
	if snapshot.ToolUpdates[0].ToolCallID != "tool-1" {
		t.Fatalf("snapshot.ToolUpdates[0] = %#v, want tool-1", snapshot.ToolUpdates[0])
	}
}

func TestReadModelTracksRunUpdates(t *testing.T) {
	now := time.Now().UTC()
	model := NewReadModel(core.ClientSnapshot{
		SessionID: "session-1",
		Run: &core.Run{
			ID:        "run-1",
			SessionID: "session-1",
			Status:    core.RunStatusAccepted,
			StartedAt: now,
			UpdatedAt: now,
		},
	})

	payload, _ := json.Marshal(core.Run{
		ID:         "run-1",
		SessionID:  "session-1",
		Status:     core.RunStatusCompleted,
		StartedAt:  now,
		UpdatedAt:  now.Add(time.Second),
		FinishedAt: ptrTime(now.Add(time.Second)),
	})
	if err := model.Apply(daemonclient.LiveEvent{
		Type:      core.EventRunUpdated,
		SessionID: "session-1",
		Payload:   payload,
	}); err != nil {
		t.Fatalf("Apply(run.updated) error = %v", err)
	}

	snapshot := model.Snapshot()
	if snapshot.Run == nil {
		t.Fatal("snapshot.Run = nil, want run")
	}
	if snapshot.Run.Status != core.RunStatusCompleted {
		t.Fatalf("snapshot.Run.Status = %q, want %q", snapshot.Run.Status, core.RunStatusCompleted)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
