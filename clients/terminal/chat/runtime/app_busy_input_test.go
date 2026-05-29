package runtime

import (
	"strings"
	"testing"

	surfaceinput "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/input"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestBusySubmitSendsInsteadOfRejecting(t *testing.T) {
	m := newRuntimeChatTestModel(t, runtimeChatTestMessages(1)...)
	m.busy = true
	m.busyInputMode = core.BusyInputModeQueue

	cmd := m.handleSubmit(surfaceinput.SubmitMsg{Content: "queue this"})

	if cmd == nil {
		t.Fatalf("handleSubmit returned nil cmd, want busy input send command")
	}
	if strings.Contains(m.err, "agent is busy") {
		t.Fatalf("err = %q, want busy submit allowed", m.err)
	}
}

func TestBusyCommandsSetModesAndDispatchText(t *testing.T) {
	m := newRuntimeChatTestModel(t, runtimeChatTestMessages(1)...)

	handled, cmd := m.handleBusySubmitCommand("/busy steer")
	if !handled || cmd != nil {
		t.Fatalf("/busy steer handled=%v cmd=%v, want handled without send", handled, cmd)
	}
	if m.busyInputMode != core.BusyInputModeSteer {
		t.Fatalf("busy mode = %q, want steer", m.busyInputMode)
	}

	mode, text, ok := parseBusyMessageCommand("/queue run later")
	if !ok || mode != core.BusyInputModeQueue || text != "run later" {
		t.Fatalf("parse /queue = (%q, %q, %v), want queue run later", mode, text, ok)
	}
	mode, text, ok = parseBusyMessageCommand("/steer use tests")
	if !ok || mode != core.BusyInputModeSteer || text != "use tests" {
		t.Fatalf("parse /steer = (%q, %q, %v), want steer use tests", mode, text, ok)
	}
}

func TestQueuedAcceptRunResultReloadsWithoutEmptyMessage(t *testing.T) {
	m := newRuntimeChatTestModel(t, runtimeChatTestMessages(1)...)

	cmd := m.handleSendMessageResult(sendMessageResultMsg{
		content: "queued input",
		result: core.AcceptRunResult{
			SessionID: "session",
			Status:    core.AcceptRunStatusQueued,
		},
	})

	if cmd == nil {
		t.Fatalf("handleSendMessageResult returned nil cmd, want snapshot reload")
	}
	snapshot := m.currentSnapshot()
	if len(snapshot.Messages) != 1 {
		t.Fatalf("messages = %#v, want no empty accepted-run message appended", snapshot.Messages)
	}
}
