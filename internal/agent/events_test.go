package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestEachEventIsConsumedOnceItIsJournaled(t *testing.T) {
	f := agenttest.NewFixture()
	f.Inbox.Events = []agent.Input{{Kind: agent.InputEvent, ID: "task_1", Text: "first"}, {Kind: agent.InputEvent, ID: "task_2", Text: "second"}}
	f.Journal.OnAppend = func(message transcript.Message) error {
		if message.Content == "second" {
			return errors.New("disk full")
		}
		return nil
	}

	_, _ = f.Engine().Run(context.Background(), f.Task(agenttest.NewScriptedModel(text("Done."))))

	if len(f.Inbox.Events) != 1 || f.Inbox.Events[0].ID != "task_2" {
		t.Fatalf("events left = %+v", f.Inbox.Events)
	}
}

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

// awaitTool asks the engine to wait up to ten minutes for taskIDs; each call
// takes a minute of the fixture's clock.
func awaitTool(f *agenttest.Fixture, taskIDs ...string) agenttest.ToolFunc {
	return func(tools.Call) tools.Result {
		f.Clock = f.Clock.Add(time.Minute)
		return tools.Result{Content: "Waiting.", Await: &tools.Await{TaskIDs: taskIDs, Until: f.Clock.Add(10 * time.Minute)}}
	}
}

func resume(t *testing.T, f *agenttest.Fixture, model agent.Model, counters agent.Counters) agent.Outcome {
	t.Helper()
	task := f.Task(model)
	task.Resume = counters
	outcome, err := f.Engine().Run(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func TestAwaitParksTheRunUntilAnAwaitedTaskFinishes(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["await"] = awaitTool(f, "task_a")
	model := agenttest.NewScriptedModel(calls(call("w1", "await")), text("Tests passed."))

	parked := run(t, f, model)
	if parked.Status != agent.StatusWaitingEvents || parked.Counters.Await == nil || parked.Counters.Await.TaskIDs[0] != "task_a" {
		t.Fatalf("parked = %+v", parked)
	}
	if last := f.Journal.States[len(f.Journal.States)-1]; last.Counters.Await == nil {
		t.Fatalf("the park checkpoint lacks the await: %+v", last)
	}

	f.Inbox.Events = []agent.Input{{Kind: agent.InputEvent, ID: "task_b", Text: "Background task task_b finished with exit code 0."}}
	still := resume(t, f, model, parked.Counters)
	if still.Status != agent.StatusWaitingEvents || len(model.Requests()) != 1 || len(f.Inbox.Events) != 1 {
		t.Fatalf("an unawaited task woke the run: %+v, requests %d", still, len(model.Requests()))
	}

	f.Inbox.Events = append(f.Inbox.Events, agent.Input{Kind: agent.InputEvent, ID: "task_a", Text: "Background task task_a finished with exit code 0."})
	done := resume(t, f, model, still.Counters)
	if done.Status != agent.StatusCompleted || done.Assistant.Content != "Tests passed." || done.Counters.Await != nil || len(f.Inbox.Events) != 0 {
		t.Fatalf("done = %+v, events left %+v", done, f.Inbox.Events)
	}
	last := model.Requests()[1].Messages
	if got := last[len(last)-1].Content; got != "Background task task_a finished with exit code 0." {
		t.Fatalf("the woken request ends with %q", got)
	}
}

func TestAnAwaitedTaskThatEndedWithoutAnEventWakesTheRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["await"] = awaitTool(f, "task_a")
	model := agenttest.NewScriptedModel(calls(call("w1", "await")), text("Read it already; done."))
	parked := run(t, f, model)

	f.Inbox.Ended = []string{"task_a"}
	done := resume(t, f, model, parked.Counters)

	if done.Status != agent.StatusCompleted || done.Counters.Await != nil {
		t.Fatalf("done = %+v", done)
	}
}

func TestUserInputWakesAnAwaitingRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["await"] = awaitTool(f)
	model := agenttest.NewScriptedModel(calls(call("w1", "await")), text("Stopped the build."))
	parked := run(t, f, model)

	f.Inbox.Steers = []string{"stop waiting and cancel the build"}
	done := resume(t, f, model, parked.Counters)

	if done.Status != agent.StatusCompleted || len(f.Inbox.Steers) != 0 {
		t.Fatalf("done = %+v, steers left %v", done, f.Inbox.Steers)
	}
	var user []string
	for _, message := range f.Journal.Messages {
		if message.Role == transcript.MessageRoleUser {
			user = append(user, message.Content)
		}
	}
	if len(user) != 2 || user[1] != "stop waiting and cancel the build" {
		t.Fatalf("user messages = %q", user)
	}
}

func TestAwaitTimesOutAndTellsTheModel(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["await"] = awaitTool(f, "task_a")
	model := agenttest.NewScriptedModel(calls(call("w1", "await")), text("Still building; I will check later."))
	parked := run(t, f, model)

	f.Clock = parked.Counters.Await.Until
	done := resume(t, f, model, parked.Counters)

	if done.Status != agent.StatusCompleted {
		t.Fatalf("done = %+v", done)
	}
	last := model.Requests()[1].Messages
	if got := last[len(last)-1].Content; got != "Stopped waiting: the await timed out before task_a finished." {
		t.Fatalf("the woken request ends with %q", got)
	}
}

func TestTimeParkedInAwaitIsNotActiveTime(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["await"] = awaitTool(f, "task_a")
	model := agenttest.NewScriptedModel(calls(call("w1", "await")), text("Done."))
	parked := run(t, f, model)
	if parked.Counters.Active != time.Minute {
		t.Fatalf("active before parking = %v", parked.Counters.Active)
	}

	f.Clock = f.Clock.Add(5 * time.Minute)
	f.Inbox.Events = []agent.Input{{Kind: agent.InputEvent, ID: "task_a", Text: "Background task task_a finished with exit code 0."}}
	done := resume(t, f, model, parked.Counters)

	if done.Counters.Active != time.Minute {
		t.Fatalf("active after five parked minutes = %v", done.Counters.Active)
	}
}
