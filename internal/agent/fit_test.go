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

func TestReportedPromptTokensDecideWhenToSummarise(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(100_000)...)
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("c1", "read")}, Usage: providers.Usage{PromptTokens: 70_000}}},
		text("SUMMARY"),
		text("Done."),
	)

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 3 || !isSummaryRequest(requests[1]) {
		t.Fatalf("outcome = %+v requests = %d, want a summary before the second step", outcome, len(requests))
	}
	marks := boundaries(f.Journal.Messages)
	if len(marks) != 1 || marks[0].Compaction.TokensBefore < 70_000 {
		t.Fatalf("boundaries = %+v, want tokens before from the reported usage", marks)
	}
}

func TestSmallEstimateWithoutReportedUsageDoesNotSummarise(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(100_000)...)
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(calls(call("c1", "read")), text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 2 || len(boundaries(f.Journal.Messages)) != 0 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
}

func TestOldBulkyResultsAreElidedAtSixtyPercentAndStayElided(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 100_000
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result {
		return tools.Result{Content: call.ToolCallID + " " + strings.Repeat("x", 20_000)}
	}
	model := agenttest.NewScriptedModel(append(toolSteps(11, "read"), text("Done."))...)

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 12 {
		t.Fatalf("outcome = %+v requests = %d, want no summary", outcome, len(requests))
	}
	eleventh, twelfth := requests[10], requests[11]
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("read%d", i)
		if got := toolContent(eleventh, id); !strings.HasPrefix(got, "[output of read() hidden") || toolContent(twelfth, id) != got {
			t.Fatalf("%s in steps 11/12 = %.60q / %.60q", id, got, toolContent(twelfth, id))
		}
	}
	if got := toolContent(eleventh, "read6"); !strings.HasPrefix(got, "read6 x") {
		t.Fatalf("read6 = %.40q, want it in full", got)
	}
	if stored, _ := f.Journal.Result("read1"); !strings.HasPrefix(stored.Content, "read1 x") {
		t.Fatalf("journal result = %.40q, want the full output kept", stored.Content)
	}
	if last := f.Journal.States[len(f.Journal.States)-1].Counters; last.ElidedResults == 0 {
		t.Fatalf("counters = %+v, want the elision checkpointed", last)
	}
}
