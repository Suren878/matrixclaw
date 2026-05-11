package core

import (
	"testing"
	"time"
)

func TestDeriveClientSnapshotToolStateFromApprovals(t *testing.T) {
	now := time.Now().UTC()
	messages := []Message{snapshotToolCallMessage(now, "write", false)}
	tests := []struct {
		name          string
		approvalState ApprovalState
		wantPending   int
		wantLifecycle ToolLifecycleState
		wantError     string
		wantGranted   bool
		wantDenied    bool
	}{
		{name: "pending", approvalState: ApprovalStatePending, wantPending: 1, wantLifecycle: ToolLifecycleWaitingApproval},
		{name: "rejected", approvalState: ApprovalStateRejected, wantLifecycle: ToolLifecycleFailed, wantError: "approval denied", wantDenied: true},
		{name: "approved", approvalState: ApprovalStateApproved, wantLifecycle: ToolLifecycleRequested, wantGranted: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			approvals := []Approval{snapshotApproval(now, tt.approvalState)}
			pending, updates, notifications := deriveClientSnapshotToolState(approvals, messages)
			if len(pending) != tt.wantPending {
				t.Fatalf("len(pending) = %d, want %d", len(pending), tt.wantPending)
			}
			if len(updates) != 1 {
				t.Fatalf("len(updates) = %d, want 1", len(updates))
			}
			if updates[0].State != tt.wantLifecycle {
				t.Fatalf("updates[0].State = %q, want %q", updates[0].State, tt.wantLifecycle)
			}
			if updates[0].Error != tt.wantError {
				t.Fatalf("updates[0].Error = %q, want %q", updates[0].Error, tt.wantError)
			}
			if tt.approvalState == ApprovalStateApproved && updates[0].ApprovalID != "approval-1" {
				t.Fatalf("updates[0].ApprovalID = %q, want %q", updates[0].ApprovalID, "approval-1")
			}
			if len(notifications) > 1 {
				t.Fatalf("notifications = %#v, want at most one notification", notifications)
			}
			if tt.wantGranted && (len(notifications) != 1 || !notifications[0].Granted) {
				t.Fatalf("notifications = %#v, want one granted notification", notifications)
			}
			if tt.wantDenied && (len(notifications) != 1 || !notifications[0].Denied) {
				t.Fatalf("notifications = %#v, want one denied notification", notifications)
			}
			if !tt.wantGranted && !tt.wantDenied && len(notifications) != 0 {
				t.Fatalf("len(notifications) = %d, want 0", len(notifications))
			}
		})
	}
}

func TestDeriveClientSnapshotToolStateFromToolResult(t *testing.T) {
	now := time.Now().UTC()
	messages := []Message{
		snapshotToolCallMessage(now, "read", true),
		{
			ID:        "msg-tool-result",
			SessionID: "session-1",
			RunID:     "run-1",
			Role:      MessageRoleTool,
			Parts: []MessagePart{{
				Kind: MessagePartKindToolResult,
				ToolResult: &ToolResultPart{
					ToolCallID: "tool-1",
					Name:       "read",
					Content:    "hello",
				},
			}},
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	pending, updates, notifications := deriveClientSnapshotToolState(nil, messages)
	if len(pending) != 0 {
		t.Fatalf("len(pending) = %d, want 0", len(pending))
	}
	if len(updates) != 1 {
		t.Fatalf("len(updates) = %d, want 1", len(updates))
	}
	if updates[0].State != ToolLifecycleCompleted {
		t.Fatalf("updates[0].State = %q, want %q", updates[0].State, ToolLifecycleCompleted)
	}
	if updates[0].ResultMessageID != "msg-tool-result" {
		t.Fatalf("updates[0].ResultMessageID = %q, want %q", updates[0].ResultMessageID, "msg-tool-result")
	}
	if len(notifications) != 0 {
		t.Fatalf("len(notifications) = %d, want 0", len(notifications))
	}
}

func snapshotToolCallMessage(now time.Time, name string, finished bool) Message {
	return Message{
		ID:        "msg-tool-call",
		SessionID: "session-1",
		RunID:     "run-1",
		Role:      MessageRoleAssistant,
		Parts: []MessagePart{{
			Kind: MessagePartKindToolCall,
			ToolCall: &ToolCallPart{
				ID:       "tool-1",
				Name:     name,
				Input:    `{"file_path":"notes.txt"}`,
				Finished: finished,
			},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func snapshotApproval(now time.Time, state ApprovalState) Approval {
	approval := Approval{
		ID:          "approval-1",
		SessionID:   "session-1",
		RunID:       "run-1",
		ToolCallRef: "tool-1",
		ToolName:    "write",
		Action:      "write",
		Path:        "/tmp",
		State:       state,
		RequestedAt: now,
	}
	if state != ApprovalStatePending {
		approval.DecidedAt = &now
	}
	return approval
}
