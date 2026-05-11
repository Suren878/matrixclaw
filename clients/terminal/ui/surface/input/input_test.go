package input

import (
	"testing"

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
