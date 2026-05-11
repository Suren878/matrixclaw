package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestLoadInitialMsgWithAssistantReplyRebuildsVisibleChat(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, &Runtime{})
	model.width = 100
	model.height = 30
	model.session = "session-1"
	model.read = viewmodel.NewReadModel(core.ClientSnapshot{
		SessionID: "session-1",
		Messages: []core.Message{{
			ID:        "msg-user",
			SessionID: "session-1",
			RunID:     "run-1",
			Role:      core.MessageRoleUser,
			Content:   "ping",
			Parts:     core.NormalizeMessageParts("ping", nil),
			CreatedAt: now,
			UpdatedAt: now,
		}},
		Run: &core.Run{
			ID:        "run-1",
			SessionID: "session-1",
			Status:    core.RunStatusAccepted,
		},
	})
	model.rebuildChat()

	next, cmd := model.Update(loadInitialMsg{
		snapshot: core.ClientSnapshot{
			SessionID: "session-1",
			Messages: []core.Message{
				{
					ID:        "msg-user",
					SessionID: "session-1",
					RunID:     "run-1",
					Role:      core.MessageRoleUser,
					Content:   "ping",
					Parts:     core.NormalizeMessageParts("ping", nil),
					CreatedAt: now,
					UpdatedAt: now,
				},
				{
					ID:        "msg-assistant",
					SessionID: "session-1",
					RunID:     "run-1",
					Role:      core.MessageRoleAssistant,
					Content:   "pong",
					Parts:     core.NormalizeMessageParts("pong", nil),
					CreatedAt: now.Add(time.Second),
					UpdatedAt: now.Add(time.Second),
					Model:     "gpt-5.4-nano",
					Provider:  "openai-compatible",
				},
			},
			Run: &core.Run{
				ID:         "run-1",
				SessionID:  "session-1",
				Status:     core.RunStatusCompleted,
				FinishedAt: ptrTime(now.Add(time.Second)),
				UpdatedAt:  now.Add(time.Second),
			},
		},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected follow-up subscribe/focus command batch")
	}
	if model.chat == nil {
		t.Fatal("expected chat model")
	}
	if got := model.chat.Len(); got < 2 {
		t.Fatalf("chat.Len() = %d, want at least 2", got)
	}
	view := model.viewContent()
	if !strings.Contains(view, "pong") {
		t.Fatalf("app view missing assistant reply: %q", view)
	}
}

func TestAppViewDoesNotOverrideTerminalDefaultColors(t *testing.T) {
	model := newApp(nil, nil)
	model.width = 80
	model.height = 24

	view := model.View()
	if view.ForegroundColor != nil {
		t.Fatalf("ForegroundColor = %v, want nil", view.ForegroundColor)
	}
	if view.BackgroundColor != nil {
		t.Fatalf("BackgroundColor = %v, want nil", view.BackgroundColor)
	}
}
