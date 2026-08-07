package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestToProviderMessagesKeepsSVGAsAttachmentReference(t *testing.T) {
	messages, err := toProviderMessages(context.Background(), Message{
		Role:    MessageRoleUser,
		Content: "Move this file.",
		Parts: []MessagePart{{
			Kind: MessagePartKindImage,
			Image: &ImagePart{
				MIMEType:    "image/svg+xml",
				Name:        "logo.svg",
				StoragePath: "telegram/images/logo.svg",
				Temporary:   true,
			},
		}},
	}, staticAttachmentReader{
		err: errors.New("storage file not found"),
	})
	if err != nil {
		t.Fatalf("toProviderMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if len(messages[0].Images) != 0 {
		t.Fatalf("inline images = %d, want 0 for SVG", len(messages[0].Images))
	}
	if want := `temp_path="telegram/images/logo.svg"`; !strings.Contains(messages[0].Content, want) {
		t.Fatalf("content = %q, want attachment reference %q", messages[0].Content, want)
	}
}

func TestToProviderMessagesKeepsRasterImageInline(t *testing.T) {
	messages, err := toProviderMessages(context.Background(), Message{
		Role:    MessageRoleUser,
		Content: "Describe this image.",
		Parts: []MessagePart{{
			Kind: MessagePartKindImage,
			Image: &ImagePart{
				MIMEType:    "image/png; charset=binary",
				StoragePath: "telegram/images/photo.png",
			},
		}},
	}, staticAttachmentReader{
		data: AttachmentData{MIMEType: "image/png", Data: []byte("png")},
	})
	if err != nil {
		t.Fatalf("toProviderMessages: %v", err)
	}
	if len(messages) != 1 || len(messages[0].Images) != 1 {
		t.Fatalf("messages = %#v, want one message with one inline image", messages)
	}
}

func TestToProviderMessagesDoesNotFailForExpiredRasterImage(t *testing.T) {
	messages, err := toProviderMessages(context.Background(), Message{
		Role: MessageRoleUser,
		Parts: []MessagePart{{
			Kind: MessagePartKindImage,
			Image: &ImagePart{
				MIMEType:    "image/png",
				Name:        "expired.png",
				StoragePath: "telegram/images/expired.png",
				Temporary:   true,
			},
		}},
	}, staticAttachmentReader{err: fmt.Errorf("%w: storage file not found", ErrAttachmentUnavailable)})
	if err != nil {
		t.Fatalf("toProviderMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if len(messages[0].Images) != 0 {
		t.Fatalf("inline images = %d, want 0", len(messages[0].Images))
	}
	for _, want := range []string{`temp_path="telegram/images/expired.png"`, "Unavailable attachments:", "file is no longer available"} {
		if !strings.Contains(messages[0].Content, want) {
			t.Errorf("content = %q, want %q", messages[0].Content, want)
		}
	}
}

func TestIsProviderSupportedImageMIMEType(t *testing.T) {
	tests := map[string]bool{
		"image/jpeg":                true,
		"image/png; charset=binary": true,
		"image/gif":                 true,
		"image/webp":                true,
		"image/svg+xml":             false,
		"image/tiff":                false,
		"":                          false,
	}
	for mimeType, want := range tests {
		if got := IsProviderSupportedImageMIMEType(mimeType); got != want {
			t.Errorf("IsProviderSupportedImageMIMEType(%q) = %t, want %t", mimeType, got, want)
		}
	}
}

type staticAttachmentReader struct {
	data AttachmentData
	err  error
}

func (r staticAttachmentReader) ReadAttachment(context.Context, string, bool, int64) (AttachmentData, error) {
	return r.data, r.err
}
