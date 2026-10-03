package controlplane

import (
	"net/http"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestContextClearConfirmClearsTheSession(t *testing.T) {
	daemon := newFakeDaemon(t).on("POST /v1/sessions/{id}/clear", func(*http.Request) any {
		return core.MessageResponse{Message: transcript.Message{ID: "boundary", Compaction: &transcript.Compaction{Cleared: true}}}
	})

	result := daemon.run("/context clear confirm")

	if result.Text != "Context cleared." || !result.ReloadSnapshot || daemon.called("POST /v1/sessions/s1/clear") != 1 {
		t.Fatalf("result = %+v", result)
	}
}
