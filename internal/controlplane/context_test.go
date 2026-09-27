package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func (r tokenReportRuntime) ClearContext(context.Context, string) (transcript.Message, error) {
	return transcript.Message{ID: "boundary", Compaction: &transcript.Compaction{Cleared: true}}, nil
}

func TestContextClearConfirmClearsThroughTheContextRuntime(t *testing.T) {
	result, err := New(tokenReportRuntime{}, "").Handle(context.Background(), "key", "/context clear confirm")
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Context cleared." || !result.ReloadSnapshot {
		t.Fatalf("result = %+v", result)
	}
}
