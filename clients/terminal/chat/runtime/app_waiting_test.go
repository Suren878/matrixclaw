package runtime

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/readmodel"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestRunWaitingForBackgroundTasksKeepsTheSessionBusy(t *testing.T) {
	m := newApp(context.Background(), nil)
	run := core.Run{ID: "run_1", SessionID: "session_1", Status: core.RunStatusWaitingEvents}
	m.read = readmodel.New(core.ClientSnapshot{SessionID: "session_1", Run: &run})

	if !runIsActive(&run) {
		t.Fatal("a run waiting for background tasks is not active")
	}
	if got := m.workingStatusPhase(); got != "Waiting for background tasks" {
		t.Fatalf("phase = %q", got)
	}
}
