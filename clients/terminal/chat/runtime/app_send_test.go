package runtime

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	surfaceeditor "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/editor"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestSendMessageResultAddsVisibleUserMessageToChat(t *testing.T) {
	now := time.Now().UTC()
	model := newApp(nil, nil)
	model.width = 100
	model.height = 30
	model.read = viewmodel.NewReadModel(core.ClientSnapshot{SessionID: "session-1"})
	model.rebuildChat()

	next, cmd := model.Update(sendMessageResultMsg{
		content: "hello there",
		result: core.AcceptRunResult{
			SessionID: "session-1",
			UserMessage: core.Message{
				ID:        "msg-user",
				SessionID: "session-1",
				Role:      core.MessageRoleUser,
				Content:   "hello there",
				CreatedAt: now,
				UpdatedAt: now,
			},
			Run: core.Run{ID: "run-1", SessionID: "session-1", Status: core.RunStatusAccepted},
		},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected follow-up refresh command")
	}
	if model.chat == nil {
		t.Fatal("expected chat model")
	}
	if got := len(model.read.Snapshot().Messages); got != 1 {
		t.Fatalf("len(messages) = %d, want 1", got)
	}
	if got := model.chat.Len(); got != 1 {
		t.Fatalf("chat.Len() = %d, want 1", got)
	}
	item := model.chat.MessageItem("msg-user")
	if item == nil {
		t.Fatal("expected chat item for user message")
	}
	rendered := strings.TrimSpace(item.Render(80))
	if !strings.Contains(rendered, "hello") || !strings.Contains(rendered, "there") {
		t.Fatalf("chat item missing user text: %q", rendered)
	}

	view := model.viewContent()
	if !strings.Contains(view, "hello") || !strings.Contains(view, "there") {
		t.Fatalf("app view missing user text: %q", view)
	}
}

func TestSendMessageResultSchedulesSnapshotRefresh(t *testing.T) {
	model := newApp(nil, &Runtime{})
	next, cmd := model.Update(sendMessageResultMsg{
		result: core.AcceptRunResult{
			SessionID: "session-1",
			UserMessage: core.Message{
				ID:        "msg-user",
				SessionID: "session-1",
				Role:      core.MessageRoleUser,
				Content:   "hello",
			},
			Run: core.Run{ID: "run-1", SessionID: "session-1", Status: core.RunStatusAccepted},
		},
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd == nil {
		t.Fatal("expected snapshot refresh command")
	}
}

func TestSendMessageErrorRestoresAttachments(t *testing.T) {
	model := newApp(nil, nil)
	next, cmd := model.Update(sendMessageResultMsg{
		content: "hello",
		attachments: []surfaceeditor.Attachment{{
			FilePath: "notes.txt",
			FileName: "notes.txt",
			MimeType: "text/plain",
			Content:  []byte("hello"),
		}},
		err: errors.New("boom"),
	})
	if next == nil {
		t.Fatal("expected model")
	}
	if cmd != nil {
		t.Fatal("expected no command")
	}
	if got := model.input.Editor().Value(); got != "hello" {
		t.Fatalf("editor value = %q, want %q", got, "hello")
	}
	if got := len(model.input.Editor().Attachments()); got != 1 {
		t.Fatalf("len(attachments) = %d, want 1", got)
	}
}
