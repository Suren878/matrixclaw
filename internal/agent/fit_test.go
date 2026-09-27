package agent_test

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
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
