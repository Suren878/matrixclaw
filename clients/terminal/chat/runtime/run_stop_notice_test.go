package runtime

import (
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestStoppedRunShowsHowToContinueUntilTheNextRunStarts(t *testing.T) {
	m := &appModel{}

	m.showRunStopNotice(core.Run{ID: "r1", Status: core.RunStatusCompleted, StopReason: agent.StopDone})
	if len(m.notes) != 0 {
		t.Fatalf("finished run left a notice: %+v", m.notes)
	}
	m.showRunStopNotice(core.Run{ID: "r1", Status: core.RunStatusCompleted, StopReason: agent.StopBudgetExhausted})
	if len(m.notes) != 1 || !strings.Contains(m.notes[0].Content().Text, "/continue") {
		t.Fatalf("notice = %+v", m.notes)
	}
	m.showRunStopNotice(core.Run{ID: "r2", Status: core.RunStatusRunning})
	if len(m.notes) != 0 {
		t.Fatalf("notice kept while a run is active: %+v", m.notes)
	}
}
