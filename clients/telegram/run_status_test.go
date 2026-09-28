package telegram

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type runStatusHarness struct {
	t      *testing.T
	now    time.Time
	daemon *runDaemon
	api    *runRenderBotAPI
	worker *Worker
}

func newRunStatusHarness(t *testing.T) *runStatusHarness {
	h := &runStatusHarness{t: t, now: time.Unix(100, 0), daemon: newRunDaemon(), api: &runRenderBotAPI{}}
	h.daemon.progress = core.RunProgress{Steps: 1, StepLimit: 300}
	h.worker = newRunDaemonWorker(t, h.daemon, h.api, &h.now)
	h.daemon.add(transcript.Message{ID: "user", Role: transcript.MessageRoleUser, Content: "Fix the build"})
	return h
}

// deliver advances the clock and runs one delivery pass.
func (h *runStatusHarness) deliver(after time.Duration) error {
	h.now = h.now.Add(after)
	return h.worker.deliverPendingRuns(context.Background())
}

func (h *runStatusHarness) mustDeliver(after time.Duration) {
	h.t.Helper()
	if err := h.deliver(after); err != nil {
		h.t.Fatal(err)
	}
}

func (h *runStatusHarness) progress(steps int, tasks int) {
	h.daemon.set(func(d *runDaemon) { d.progress = core.RunProgress{Steps: steps, StepLimit: 300, Tasks: tasks} })
}

func (h *runStatusHarness) finish(stopReason agent.StopReason) {
	h.daemon.set(func(d *runDaemon) {
		d.run.Status = core.RunStatusCompleted
		d.run.StopReason = stopReason
	})
}

func TestRunStatusIsOneSilentMessageEditedAsTheRunGoes(t *testing.T) {
	h := newRunStatusHarness(t)
	h.mustDeliver(0)
	if h.api.sendCount() != 0 {
		t.Fatalf("sent before any tool call: %q", h.api.messageTexts())
	}

	h.daemon.add(toolCallMessage("call-0", "bash", `{"command":"go test ./..."}`, false))
	h.mustDeliver(time.Second)
	status := h.api.messages[0]
	if h.api.sendCount() != 1 || status.Text != "⏳ Working · step 1/300\nUsing bash: go test ./..." || !status.DisableNotification || !status.SkipReplyKeyboardRemove {
		t.Fatalf("status = %+v", h.api.messages)
	}

	h.daemon.add(toolResultMessage("call-0", "bash"))
	h.progress(2, 1)
	h.mustDeliver(time.Second)
	if h.api.editCount() != 0 {
		t.Fatalf("edited within %s of the last edit", runStatusEditInterval)
	}
	h.mustDeliver(time.Second)
	if h.api.editCount() != 1 || h.api.messageText(1) != "⏳ Working · step 2/300\nThinking...\nBackground tasks: 1" {
		t.Fatalf("edits = %d, status = %q", h.api.editCount(), h.api.messageText(1))
	}
	h.mustDeliver(3 * time.Second)
	if h.api.editCount() != 1 {
		t.Fatal("edited without a change")
	}

	for i := 1; i <= 20; i++ {
		id := fmt.Sprintf("call-%d", i)
		h.daemon.add(toolCallMessage(id, "read_file", `{"path":"main.go"}`, false))
		h.mustDeliver(time.Second)
		h.daemon.add(toolResultMessage(id, "read_file"))
		h.mustDeliver(time.Second)
	}
	h.daemon.add(finalReply("The build is green."))
	h.progress(22, 0)
	h.finish(agent.StopDone)
	h.mustDeliver(time.Millisecond)

	texts := h.api.messageTexts()
	if len(texts) != 2 || texts[0] != "✅ Done · step 22/300" || texts[1] != "The build is green." || h.api.messages[1].DisableNotification || !h.daemon.acked {
		t.Fatalf("messages = %q acked = %v", texts, h.daemon.acked)
	}
}

func TestRunStatusSurvivesTelegramRefusingEdits(t *testing.T) {
	h := newRunStatusHarness(t)
	h.daemon.add(toolCallMessage("call-1", "bash", `{"command":"make"}`, false))
	h.mustDeliver(0)

	h.api.editError = &APIError{ErrorCode: 400, Description: "Bad Request: message is not modified"}
	h.progress(2, 0)
	h.mustDeliver(3 * time.Second)
	if h.api.sendCount() != 1 {
		t.Fatalf("not modified sent a new status: %q", h.api.messageTexts())
	}

	h.api.editError = &APIError{ErrorCode: 400, Description: "Bad Request: message to edit not found"}
	h.progress(3, 0)
	h.mustDeliver(3 * time.Second)
	if h.api.sendCount() != 2 || h.api.messageText(2) != "⏳ Working · step 3/300\nUsing bash: make" {
		t.Fatalf("deleted status was not sent again: %q", h.api.messageTexts())
	}

	h.api.editError = &APIError{ErrorCode: 429, Description: "Too Many Requests: retry after 30", RetryAfter: 30 * time.Second}
	h.progress(4, 0)
	if err := h.deliver(3 * time.Second); !IsRetryable(err) {
		t.Fatalf("flood wait error = %v", err)
	}
	h.api.editError = nil
	h.daemon.add(toolResultMessage("call-1", "bash"), finalReply("Built."))
	h.finish(agent.StopDone)
	h.mustDeliver(10 * time.Second)
	if h.daemon.acked || h.api.sendCount() != 2 {
		t.Fatalf("delivered before the flood wait ended: %q", h.api.messageTexts())
	}
	h.mustDeliver(25 * time.Second)
	if texts := h.api.messageTexts(); len(texts) != 3 || texts[1] != "✅ Done · step 4/300" || texts[2] != "Built." || !h.daemon.acked {
		t.Fatalf("messages = %q acked = %v", texts, h.daemon.acked)
	}
}

func TestPlainAnswerHasNoRunStatus(t *testing.T) {
	h := newRunStatusHarness(t)
	h.daemon.add(finalReply("Hello."))
	h.finish(agent.StopDone)
	h.mustDeliver(0)

	if texts := h.api.messageTexts(); len(texts) != 1 || texts[0] != "Hello." {
		t.Fatalf("messages = %q", texts)
	}
}

func TestRunStatusShowsWaitsAndHowTheRunStopped(t *testing.T) {
	h := newRunStatusHarness(t)
	h.daemon.add(toolCallMessage("call-1", "bash", `{"command":"rm -rf build"}`, false), toolCallMessage("call-2", "bash", `{"command":"ls"}`, false))
	h.daemon.set(func(d *runDaemon) {
		d.run.Status = core.RunStatusWaitingApproval
		d.approvals = []core.Approval{{ID: "a1", RunID: d.run.ID, State: core.ApprovalStatePending, ToolName: "bash"}}
	})
	h.mustDeliver(0)
	texts := h.api.messageTexts()
	if len(texts) != 2 || !strings.HasPrefix(texts[0], "Approval required") || texts[1] != "✋ Waiting for approval · step 1/300\nUsing bash: rm -rf build (+1 more)" {
		t.Fatalf("messages = %q", texts)
	}

	h.daemon.add(toolResultMessage("call-1", "bash"), toolResultMessage("call-2", "bash"), finalReply("Stopped here; run /continue."))
	h.progress(300, 0)
	h.finish(agent.StopBudgetExhausted)
	h.mustDeliver(time.Second)
	texts = h.api.messageTexts()
	if len(texts) != 4 || texts[1] != "⚠️ Stopped at the budget · step 300/300" || texts[3] != "The run stopped at its budget." {
		t.Fatalf("messages = %q", texts)
	}
}
