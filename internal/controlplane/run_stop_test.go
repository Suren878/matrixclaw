package controlplane

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
)

func TestStopNoticeExplainsOnlyRunsThatCanContinue(t *testing.T) {
	for _, reason := range []agent.StopReason{"", agent.StopDone, agent.StopBudgetExhausted, agent.StopLoopDetected, agent.StopContextExhausted} {
		if notice := StopNotice(reason); (notice != "") != reason.Continuable() {
			t.Fatalf("StopNotice(%q) = %q", reason, notice)
		}
	}
	if agent.StopContextExhausted.Continuable() {
		t.Fatal("a context-exhausted run is offered /continue")
	}
}
