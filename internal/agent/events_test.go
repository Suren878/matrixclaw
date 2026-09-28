package agent_test

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestFinishedTasksReachTheModelAsNotesBeforeItsNextStep(t *testing.T) {
	f := agenttest.NewFixture()
	f.Inbox.Events = []agent.Input{{Kind: agent.InputEvent, ID: "task_1", Text: "Background task task_1 finished with exit code 0: go test ./..."}}
	f.Tools.Funcs["read"] = func(tools.Call) tools.Result {
		f.Inbox.Events = append(f.Inbox.Events, agent.Input{Kind: agent.InputEvent, ID: "task_2", Text: "Background task task_2 finished with exit code 1: make"})
		return tools.Result{Content: "file body"}
	}
	model := agenttest.NewScriptedModel(calls(call("c1", "read")), text("Both done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(f.Inbox.Events) != 0 {
		t.Fatalf("outcome = %+v, events left = %+v", outcome, f.Inbox.Events)
	}
	var notes []string
	for _, message := range f.Journal.Messages {
		if message.Origin == transcript.OriginEngine {
			notes = append(notes, message.Content)
		}
	}
	if len(notes) != 2 || notes[0] != "Background task task_1 finished with exit code 0: go test ./..." || notes[1] != "Background task task_2 finished with exit code 1: make" {
		t.Fatalf("notes = %q", notes)
	}
	requests := model.Requests()
	if last := requests[0].Messages[len(requests[0].Messages)-1]; last.Role != "user" || last.Content != notes[0] {
		t.Fatalf("first request ends with %+v", last)
	}
	if last := requests[1].Messages[len(requests[1].Messages)-1]; last.Content != notes[1] {
		t.Fatalf("second request ends with %+v", last)
	}
}
