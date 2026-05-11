package chat

import (
	"testing"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
)

func TestToolResultImagePayloadFromContent(t *testing.T) {
	data, mediaType, ok := toolResultImagePayload(&surfacemessage.ToolResult{
		Content:  "iVBORw0KGgo=",
		MIMEType: "image/png",
	})
	if !ok {
		t.Fatal("expected image payload")
	}
	if data != "iVBORw0KGgo=" || mediaType != "image/png" {
		t.Fatalf("payload = (%q, %q), want (%q, %q)", data, mediaType, "iVBORw0KGgo=", "image/png")
	}
}

func TestToolResultImagePayloadFromMetadata(t *testing.T) {
	data, mediaType, ok := toolResultImagePayload(&surfacemessage.ToolResult{
		Metadata: `{"mime_type":"image/jpeg","content_base64":"/9j/4AAQ"}`,
	})
	if !ok {
		t.Fatal("expected image payload")
	}
	if data != "/9j/4AAQ" || mediaType != "image/jpeg" {
		t.Fatalf("payload = (%q, %q), want (%q, %q)", data, mediaType, "/9j/4AAQ", "image/jpeg")
	}
}
