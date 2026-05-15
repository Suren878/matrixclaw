package input

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfaceeditor "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/editor"
)

func TestHandleSendMessageKeepsUnsupportedAttachmentsWhenSubmissionIsRejected(t *testing.T) {
	model := New(surfacecommon.DefaultCommon())
	archive := surfaceeditor.Attachment{
		FilePath: "/tmp/test.zip",
		FileName: "test.zip",
		MimeType: "application/zip",
		Content:  []byte("zip"),
	}
	if ok := model.Editor().UpdateAttachments(archive); !ok {
		t.Fatal("expected attachment to be accepted by editor")
	}

	_ = model.handleSendMessage()

	attachments := model.Editor().Attachments()
	if got := len(attachments); got != 1 {
		t.Fatalf("len(attachments) = %d, want 1", got)
	}
	if attachments[0].FileName != archive.FileName {
		t.Fatalf("attachment filename = %q, want %q", attachments[0].FileName, archive.FileName)
	}
}

func TestSlashOnEmptyEditorOpensCommands(t *testing.T) {
	model := New(surfacecommon.DefaultCommon())

	cmd := model.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	if got := model.Value(); got != "" {
		t.Fatalf("editor value = %q, want empty", got)
	}
	if cmd == nil {
		t.Fatal("expected command")
	}
	if _, ok := cmd().(OpenCommandsMsg); !ok {
		t.Fatal("slash key did not open commands menu")
	}
}

func TestCtrlNOpensPlanInsteadOfNewSession(t *testing.T) {
	model := New(surfacecommon.DefaultCommon())

	cmd := model.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("expected command")
	}
	if _, ok := cmd().(OpenPlanMsg); !ok {
		t.Fatal("ctrl+n did not open plan")
	}
}
