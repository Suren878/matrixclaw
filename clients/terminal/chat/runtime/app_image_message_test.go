package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/readmodel"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestImageTheUserSentShowsInTheTranscript(t *testing.T) {
	m := newApp(context.Background(), nil)
	m.width, m.height = 120, 40
	m.read = readmodel.New(core.ClientSnapshot{SessionID: "session_1", Messages: []transcript.Message{{
		ID: "m1", SessionID: "session_1", Role: transcript.MessageRoleUser,
		Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindImage, Image: &transcript.ImagePart{
			MIMEType: "image/png", Name: "screenshot.png", StoragePath: "terminal/images/1-screenshot.png",
		}}},
	}}})
	m.rebuildChat()

	if view := ansi.Strip(m.chat.View()); !strings.Contains(view, "screenshot.png") {
		t.Fatalf("transcript lacks the image name:\n%s", view)
	}
}
