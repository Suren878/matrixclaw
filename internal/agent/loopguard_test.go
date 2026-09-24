package agent_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
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
	if final.ToolChoice != providers.ToolChoiceNone || !strings.Contains(lastMessage(final).Content, "5 times in a row") {
		t.Fatalf("final turn = %q / %+v", final.ToolChoice, lastMessage(final))
	}
	if notes := engineNotes(f.Journal.Messages); len(notes) != 2 {
		t.Fatalf("engine notes = %d, want the warning and the final-turn note", len(notes))
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
