package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	surfacechat "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/chat"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	"github.com/Suren878/matrixclaw/internal/clientruntime"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestBuildChatItemsMarksPendingApprovalTool(t *testing.T) {
	now := time.Now().UTC()
	snapshot := snapshotFromCore(core.ClientSnapshot{
		SessionID: "session-1",
		Messages: []core.Message{
			{
				ID:        "msg-assistant",
				SessionID: "session-1",
				Role:      core.MessageRoleAssistant,
				Parts: []core.MessagePart{
					{
						Kind: core.MessagePartKindToolCall,
						ToolCall: &core.ToolCallPart{
							ID:       "tool-pending",
							Name:     "write",
							Input:    `{"file_path":"/tmp/a.txt","content":"two"}`,
							Finished: false,
						},
					},
				},
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		Approvals: []core.Approval{{
			ID:          "approval-1",
			SessionID:   "session-1",
			ToolCallRef: "tool-pending",
			ToolName:    "write",
			Path:        "/tmp/a.txt",
		}},
	})

	items := buildChatItems(nil, snapshot)
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	toolItem, ok := items[0].(surfacechat.ToolMessageItem)
	if !ok {
		t.Fatalf("items[0] = %T, want surfacechat.ToolMessageItem", items[0])
	}
	if got := toolItem.Status(); got != surfacechat.ToolStatusAwaitingPermission {
		t.Fatalf("toolItem.Status() = %v, want awaiting permission", got)
	}

	chatModel := buildChatModel(nil, snapshot)
	if chatModel.Len() != len(items) {
		t.Fatalf("chatModel.Len() = %d, want %d", chatModel.Len(), len(items))
	}
}

func TestBuildChatItemsAppliesToolStatuses(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name          string
		updates       []core.ToolUpdate
		notifications []surfacepermission.PermissionNotification
		want          surfacechat.ToolStatus
	}{
		{
			name: "waiting approval update",
			updates: []core.ToolUpdate{{
				ToolCallID: "tool-1",
				ToolName:   "write",
				State:      core.ToolLifecycleWaitingApproval,
			}},
			want: surfacechat.ToolStatusAwaitingPermission,
		},
		{
			name: "requested update",
			updates: []core.ToolUpdate{{
				ToolCallID: "tool-1",
				ToolName:   "write",
				State:      core.ToolLifecycleRequested,
				ApprovalID: "approval-1",
			}},
			want: surfacechat.ToolStatusRunning,
		},
		{
			name: "failed update",
			updates: []core.ToolUpdate{{
				ToolCallID: "tool-1",
				ToolName:   "write",
				State:      core.ToolLifecycleFailed,
				Error:      "boom",
			}},
			want: surfacechat.ToolStatusError,
		},
		{
			name: "completed update",
			updates: []core.ToolUpdate{{
				ToolCallID: "tool-1",
				ToolName:   "write",
				State:      core.ToolLifecycleCompleted,
			}},
			want: surfacechat.ToolStatusSuccess,
		},
		{
			name: "denied notification",
			notifications: []surfacepermission.PermissionNotification{{
				ToolCallID: "tool-1",
				Denied:     true,
			}},
			want: surfacechat.ToolStatusCanceled,
		},
		{
			name: "granted notification",
			notifications: []surfacepermission.PermissionNotification{{
				ToolCallID: "tool-1",
				Granted:    true,
			}},
			want: surfacechat.ToolStatusRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := viewmodel.Snapshot{
				SessionID:             "session-1",
				Messages:              viewmodel.ToSurfaceMessages([]core.Message{toolCallMessage(now, "tool-1", "write")}),
				ToolUpdates:           tt.updates,
				ApprovalNotifications: tt.notifications,
			}

			items := buildChatItems(nil, snapshot)
			if len(items) != 1 {
				t.Fatalf("len(items) = %d, want 1", len(items))
			}
			toolItem, ok := items[0].(surfacechat.ToolMessageItem)
			if !ok {
				t.Fatalf("items[0] = %T, want surfacechat.ToolMessageItem", items[0])
			}
			if got := toolItem.Status(); got != tt.want {
				t.Fatalf("toolItem.Status() = %v, want %v", got, tt.want)
			}
		})
	}
}

func toolCallMessage(now time.Time, toolCallID, name string) core.Message {
	return core.Message{
		ID:        "msg-assistant",
		SessionID: "session-1",
		Role:      core.MessageRoleAssistant,
		Parts: []core.MessagePart{{
			Kind: core.MessagePartKindToolCall,
			ToolCall: &core.ToolCallPart{
				ID:       toolCallID,
				Name:     name,
				Input:    `{"file_path":"/tmp/a.txt","content":"two"}`,
				Finished: false,
			},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestBuildChatItemsGroupConsecutiveReadCards(t *testing.T) {
	now := time.Now().UTC()
	snapshot := snapshotFromCore(core.ClientSnapshot{
		SessionID: "session-1",
		Messages: []core.Message{
			{
				ID:        "msg-read-1",
				SessionID: "session-1",
				Role:      core.MessageRoleAssistant,
				Parts: []core.MessagePart{{
					Kind: core.MessagePartKindToolCall,
					ToolCall: &core.ToolCallPart{
						ID:       "tool-read-1",
						Name:     "read",
						Input:    `{"file_path":"internal/core/events.go"}`,
						Finished: true,
					},
				}},
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:        "msg-tool-1",
				SessionID: "session-1",
				Role:      core.MessageRoleTool,
				Parts: []core.MessagePart{{
					Kind: core.MessagePartKindToolResult,
					ToolResult: &core.ToolResultPart{
						ToolCallID: "tool-read-1",
						Name:       "read",
						Content:    "package core",
						Metadata:   []byte(`{"file_path":"internal/core/events.go","content":"package core"}`),
					},
				}},
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:        "msg-read-2",
				SessionID: "session-1",
				Role:      core.MessageRoleAssistant,
				Parts: []core.MessagePart{{
					Kind: core.MessagePartKindToolCall,
					ToolCall: &core.ToolCallPart{
						ID:       "tool-read-2",
						Name:     "read",
						Input:    `{"file_path":"internal/core/types.go"}`,
						Finished: true,
					},
				}},
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:        "msg-tool-2",
				SessionID: "session-1",
				Role:      core.MessageRoleTool,
				Parts: []core.MessagePart{{
					Kind: core.MessagePartKindToolResult,
					ToolResult: &core.ToolResultPart{
						ToolCallID: "tool-read-2",
						Name:       "read",
						Content:    "type Session struct{}",
						Metadata:   []byte(`{"file_path":"internal/core/types.go","content":"type Session struct{}"}`),
					},
				}},
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	})

	items := buildChatItems(nil, snapshot)
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1 grouped item", len(items))
	}

	rendered := items[0].Render(100)
	if !strings.Contains(rendered, "Read") {
		t.Fatalf("grouped read render missing header: %q", rendered)
	}
	if !strings.Contains(rendered, "internal/core") || !strings.Contains(rendered, "events.go") || !strings.Contains(rendered, "types.go") {
		t.Fatalf("grouped read render missing tree paths: %q", rendered)
	}
}

func snapshotFromCore(snapshot core.ClientSnapshot) viewmodel.Snapshot {
	return viewmodel.FromStateSnapshot(clientruntime.NewState(snapshot).Snapshot())
}
