package openaicompat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestChatMessageOmitsImagesForTextOnlyModel(t *testing.T) {
	runtime := &Runtime{capabilities: providers.ModelCapabilities{ImageInput: false}}
	message := runtime.chatMessage(providers.Message{
		Role:    "user",
		Content: "Describe this image.",
		Images:  []providers.ImageContent{{MIMEType: "image/png", DataBase64: "cG5n"}},
	})
	body, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "image_url") || strings.Contains(string(body), "cG5n") {
		t.Fatalf("text-only payload leaked image data: %s", body)
	}
	if content, ok := message.Content.(string); !ok || content != "Describe this image." {
		t.Fatalf("content = %#v, want original text", message.Content)
	}
}

func TestChatMessageIncludesImagesForImageCapableModel(t *testing.T) {
	runtime := &Runtime{capabilities: providers.ModelCapabilities{ImageInput: true}}
	message := runtime.chatMessage(providers.Message{
		Role:    "user",
		Content: "Describe this image.",
		Images:  []providers.ImageContent{{MIMEType: "image/png", DataBase64: "cG5n"}},
	})
	parts, ok := message.Content.([]chatCompletionContentPart)
	if !ok {
		t.Fatalf("content type = %T, want []chatCompletionContentPart", message.Content)
	}
	if len(parts) != 2 || parts[1].Type != "image_url" || parts[1].ImageURL == nil {
		t.Fatalf("content parts = %#v", parts)
	}
}

func TestModelModalitiesImageInput(t *testing.T) {
	if got := modelModalitiesImageInput(nil); got != nil {
		t.Fatalf("nil modalities = %v, want nil", *got)
	}
	if got := modelModalitiesImageInput([]string{"text"}); got == nil || *got {
		t.Fatalf("text modalities = %v, want false", got)
	}
	if got := modelModalitiesImageInput([]string{"text", "image"}); got == nil || !*got {
		t.Fatalf("image modalities = %v, want true", got)
	}
}
