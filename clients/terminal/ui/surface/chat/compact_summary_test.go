package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

func TestContextClearedRendersDistinctCardAndPreview(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	message := surfacemessage.Message{
		ID:   "msg_clear",
		Role: surfacemessage.System,
		Parts: []surfacemessage.ContentPart{
			surfacemessage.TextContent{Text: "🧹 Context cleared\n\nContext cleared by user."},
		},
	}

	items := ExtractMessageItems(&sty, &message, nil)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	rendered := items[0].RawRender(100)
	for _, want := range []string{"Context cleared", "press enter to view"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("context clear render missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "Context compacted") {
		t.Fatalf("context clear render used compact label:\n%s", rendered)
	}

	handler, ok := items[0].(KeyEventHandler)
	if !ok {
		t.Fatalf("context clear item does not handle key events")
	}
	handled, cmd := handler.HandleKeyEvent(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if !handled || cmd == nil {
		t.Fatalf("HandleKeyEvent enter = (%v, %v), want preview command", handled, cmd)
	}
	msg, ok := cmd().(surfacedialog.ActionOpenFilePreview)
	if !ok {
		t.Fatalf("preview command returned %T, want ActionOpenFilePreview", cmd())
	}
	if msg.Data.Title != "Context Clear" {
		t.Fatalf("preview title = %q, want Context Clear", msg.Data.Title)
	}
	if !strings.Contains(msg.Data.Content, "Context cleared by user.") {
		t.Fatalf("preview content missing clear detail:\n%s", msg.Data.Content)
	}
}
