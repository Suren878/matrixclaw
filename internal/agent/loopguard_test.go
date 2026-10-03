package agent_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func statusTool(tools.Call) tools.Result {
	return tools.Result{Content: "working tree clean"}
}

func TestRepeatingACallWithoutProgressWarnsThenStops(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["status"] = statusTool
	layouts := []string{`{"path":"a","deep":true}`, `{"deep":true, "path":"a"}`}
	turns := make([]agenttest.Turn, 0, 6)
	for i := 1; i <= 5; i++ {
		turns = append(turns, calls(providers.ToolCall{ID: fmt.Sprintf("s%d", i), Name: "status", Arguments: []byte(layouts[i%2])}))
	}
	model := agenttest.NewScriptedModel(append(turns, text("Nothing changes; stopping here."))...)

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopLoopDetected || len(requests) != 6 || len(f.Tools.Calls) != 5 {
		t.Fatalf("outcome = %+v requests = %d tool calls = %d", outcome, len(requests), len(f.Tools.Calls))
	}
	if note := lastMessage(requests[3]); note.Role != "user" || !strings.Contains(note.Content, "You are repeating status") {
		t.Fatalf("request 4 ends with %+v, want the loop warning", note)
	}
	if strings.Contains(lastMessage(requests[4]).Content, "You are repeating") {
		t.Fatal("loop warning repeated within one streak")
	}
	final := requests[5]
	if final.ToolChoice != providers.ToolChoiceNone || !strings.Contains(lastMessage(final).Content, "5 times among your recent calls") {
		t.Fatalf("final turn = %q / %+v", final.ToolChoice, lastMessage(final))
	}
	if notes := engineNotes(f.Journal.Messages); len(notes) != 2 || notes[0].Origin != transcript.OriginEngine || notes[1].Origin != transcript.OriginEngineModel {
		t.Fatalf("engine notes = %+v, want the shown warning and the model-only final-turn note", notes)
	}
}

func TestCyclingThroughTheSameCallsWarnsThenStops(t *testing.T) {
	f := agenttest.NewFixture()
	for _, name := range []string{"read", "check", "inspect"} {
		f.Tools.Funcs[name] = statusTool
	}
	cycle := []string{"read", "check", "inspect"}
	turns := make([]agenttest.Turn, 0, 14)
	for i := 0; i < 13; i++ {
		turns = append(turns, calls(call(fmt.Sprintf("c%d", i), cycle[i%3])))
	}
	model := agenttest.NewScriptedModel(append(turns, text("Going in circles; stopping."))...)

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.StopReason != agent.StopLoopDetected || len(f.Tools.Calls) != 13 || len(requests) != 14 {
		t.Fatalf("outcome = %+v requests = %d tool calls = %d", outcome, len(requests), len(f.Tools.Calls))
	}
	if note := lastMessage(requests[7]); !strings.Contains(note.Content, "You are repeating read") {
		t.Fatalf("request 8 ends with %+v, want the loop warning", note)
	}
	if final := requests[13]; final.ToolChoice != providers.ToolChoiceNone {
		t.Fatalf("final turn tool choice = %q", final.ToolChoice)
	}
}

func TestEmptyFinalTurnAfterALoopFallsBackToANeutralStopNote(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["status"] = statusTool
	turns := make([]agenttest.Turn, 0, 6)
	for i := 1; i <= 5; i++ {
		turns = append(turns, calls(providers.ToolCall{ID: fmt.Sprintf("s%d", i), Name: "status", Arguments: []byte(`{}`)}))
	}
	model := agenttest.NewScriptedModel(append(turns, text(""))...)

	outcome := run(t, f, model)

	if outcome.StopReason != agent.StopLoopDetected || outcome.Assistant.Content != "This run stopped: it repeated the same action without progress." {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestPollingThatReturnsNewOutputIsNotALoop(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["task_output"] = counterTool()
	model := agenttest.NewScriptedModel(append(toolSteps(6, "task_output"), text("Build finished."))...)

	outcome := run(t, f, model)

	if outcome.StopReason != agent.StopDone || len(engineNotes(f.Journal.Messages)) != 0 {
		t.Fatalf("outcome = %+v notes = %+v", outcome, engineNotes(f.Journal.Messages))
	}
}

func TestLoopStreakIsCheckpointed(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["status"] = statusTool
	model := agenttest.NewScriptedModel(append(toolSteps(3, "status"), text("Clean."))...)

	run(t, f, model)

	last := f.Journal.States[len(f.Journal.States)-1].Counters
	if last.LoopTool != "status" || last.LoopRepeats != 3 || !last.LoopWarned {
		t.Fatalf("last checkpoint counters = %+v", last)
	}
}

func TestRejectedCallsExtendTheStreak(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(append(toolSteps(3, "ghost"), text("That tool does not exist."))...)

	run(t, f, model)

	if note := lastMessage(model.Requests()[3]); !strings.Contains(note.Content, "You are repeating ghost") {
		t.Fatalf("request 4 ends with %+v, want the loop warning", note)
	}
}

func TestRejectedCallStreakSurvivesAnApprovalPark(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	model := agenttest.NewScriptedModel(calls(call("w1", "write"), call("g1", "ghost")))

	outcome := run(t, f, model)

	last := f.Journal.States[len(f.Journal.States)-1].Counters
	if outcome.Status != agent.StatusWaitingApproval || last.LoopTool != "ghost" || last.LoopRepeats != 1 {
		t.Fatalf("outcome = %+v last checkpoint counters = %+v", outcome, last)
	}
}

func TestRepeatedAwaitsOnALongTaskAreNotALoop(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["await"] = awaitTool(f, "task_a")
	model := agenttest.NewScriptedModel(append(toolSteps(6, "await"), text("Still building."))...)

	outcome := run(t, f, model)
	for outcome.Status == agent.StatusWaitingEvents {
		f.Clock = outcome.Counters.Await.Until
		outcome = resume(t, f, model, outcome.Counters)
	}

	if outcome.StopReason != agent.StopDone || outcome.Assistant.Content != "Still building." {
		t.Fatalf("outcome = %+v", outcome)
	}
	for _, note := range engineNotes(f.Journal.Messages) {
		if strings.Contains(note.Content, "repeating") {
			t.Fatalf("a loop warning after awaits: %q", note.Content)
		}
	}
}

func TestQuietWaitsForARunningTaskAreNotALoop(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["task_output"] = func(tools.Call) tools.Result {
		return tools.Result{Content: "(no new output)\n\n(task running)", Waiting: true}
	}
	model := agenttest.NewScriptedModel(append(toolSteps(6, "task_output"), text("Still building."))...)

	outcome := run(t, f, model)

	if outcome.StopReason != agent.StopDone || len(engineNotes(f.Journal.Messages)) != 0 {
		t.Fatalf("outcome = %+v notes = %+v", outcome, engineNotes(f.Journal.Messages))
	}
}
