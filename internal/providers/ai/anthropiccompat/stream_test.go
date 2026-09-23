package anthropic

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestStreamDoesNotCompleteBeforeMessageStop(t *testing.T) {
	partial := "event: content_block_delta\ndata: {\"delta\":{\"text\":\"Hello\"}}\n\n"
	if _, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(partial)); !errors.Is(err, providers.ErrIncompleteResponse) {
		t.Fatalf("truncated stream error=%v", err)
	}
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(partial+"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	if err != nil || response.Text != "Hello" {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}
