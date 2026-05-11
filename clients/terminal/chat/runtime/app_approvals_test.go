package runtime

import (
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	surfacechat "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/chat"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestResolveApprovalMsgAppliesLocalApprovalLifecycle(t *testing.T) {
	now := time.Now().UTC()
	snapshot := core.ClientSnapshot{
		SessionID: "session-1",
		Messages: []core.Message{{
			ID:        "msg-tool-call",
			SessionID: "session-1",
			Role:      core.MessageRoleAssistant,
			Parts: []core.MessagePart{{
				Kind: core.MessagePartKindToolCall,
				ToolCall: &core.ToolCallPart{
					ID:       "tool-1",
					Name:     "write",
					Input:    `{"file_path":"/tmp/a.txt","content":"two"}`,
					Finished: false,
				},
			}},
			CreatedAt: now,
			UpdatedAt: now,
		}},
		Approvals: []core.Approval{{
			ID:          "approval-1",
			SessionID:   "session-1",
			ToolCallRef: "tool-1",
			ToolName:    "write",
			Path:        "/tmp/a.txt",
			State:       core.ApprovalStatePending,
		}},
	}

	t.Run("granted", func(t *testing.T) {
		model := newApp(nil, &Runtime{})
		model.width = 100
		model.height = 30
		model.session = "session-1"
		model.read = viewmodel.NewReadModel(snapshot)
		model.rebuildChat()

		next, cmd := model.Update(resolveApprovalMsg{
			approval: core.Approval{
				ID:          "approval-1",
				SessionID:   "session-1",
				ToolCallRef: "tool-1",
				RunID:       "run-1",
			},
			approvalID: "approval-1",
			approved:   true,
		})
		if next == nil {
			t.Fatal("expected model")
		}
		if cmd == nil {
			t.Fatal("expected snapshot reload command for granted approval")
		}
		if got := len(model.pendingApprovals()); got != 0 {
			t.Fatalf("len(model.pendingApprovals()) = %d, want 0", got)
		}
		item, ok := model.chat.MessageItem("tool-1").(surfacechat.ToolMessageItem)
		if !ok {
			t.Fatalf("model.chat.MessageItem(tool-1) = %T, want surfacechat.ToolMessageItem", model.chat.MessageItem("tool-1"))
		}
		if got := item.Status(); got != surfacechat.ToolStatusRunning {
			t.Fatalf("item.Status() = %v, want running", got)
		}
	})

	t.Run("denied", func(t *testing.T) {
		model := newApp(nil, &Runtime{})
		model.width = 100
		model.height = 30
		model.session = "session-1"
		model.read = viewmodel.NewReadModel(snapshot)
		model.rebuildChat()

		next, cmd := model.Update(resolveApprovalMsg{
			approval: core.Approval{
				ID:          "approval-1",
				SessionID:   "session-1",
				ToolCallRef: "tool-1",
			},
			approvalID: "approval-1",
			approved:   false,
		})
		if next == nil {
			t.Fatal("expected model")
		}
		if cmd == nil {
			t.Fatal("expected reload command for denied approval")
		}
		if got := len(model.pendingApprovals()); got != 0 {
			t.Fatalf("len(model.pendingApprovals()) = %d, want 0", got)
		}
		item, ok := model.chat.MessageItem("tool-1").(surfacechat.ToolMessageItem)
		if !ok {
			t.Fatalf("model.chat.MessageItem(tool-1) = %T, want surfacechat.ToolMessageItem", model.chat.MessageItem("tool-1"))
		}
		if got := item.Status(); got != surfacechat.ToolStatusCanceled {
			t.Fatalf("item.Status() = %v, want canceled", got)
		}
	})
}

func TestResolveApprovalApprovedSchedulesSnapshotRefresh(t *testing.T) {
	model := newApp(nil, &Runtime{})
	next, cmd := model.Update(resolveApprovalMsg{
		approvalID: "approval-1",
		approved:   true,
		approval: core.Approval{
			ID:    "approval-1",
			RunID: "run-1",
		},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected snapshot refresh command")
	}
}

func TestResolveApprovalDeniedReloadsSnapshot(t *testing.T) {
	model := newApp(nil, &Runtime{})
	next, cmd := model.Update(resolveApprovalMsg{
		approvalID: "approval-1",
		approved:   false,
		approval: core.Approval{
			ID: "approval-1",
		},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected snapshot reload command")
	}
}
