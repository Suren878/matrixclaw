package agent_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
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

func TestElisionAdvancesOncePerFiveNewRoundsBelowTheSummaryThreshold(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 100_000
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result {
		return tools.Result{Content: call.ToolCallID + " " + strings.Repeat("x", 5_000)}
	}
	var turns []agenttest.Turn
	for i := 1; i <= 14; i++ {
		turns = append(turns, agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call(fmt.Sprintf("read%d", i), "read")}, Usage: providers.Usage{PromptTokens: 51_000}}})
	}
	model := agenttest.NewScriptedModel(append(turns, text("Done."))...)

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 15 {
		t.Fatalf("outcome = %+v requests = %d, want no summary", outcome, len(requests))
	}
	var advanced []int
	for i := 1; i < len(requests); i++ {
		previous := requests[i-1].Messages
		if !reflect.DeepEqual(requests[i].Messages[:len(previous)], previous) {
			advanced = append(advanced, i+1)
		}
	}
	if !reflect.DeepEqual(advanced, []int{7, 12}) {
		t.Fatalf("the request prefix changed at steps %v, want 7 and 12 only", advanced)
	}
	if got := toolContent(requests[10], "read2"); !strings.HasPrefix(got, "read2 x") {
		t.Fatalf("read2 at step 11 = %.40q, want it in full until five more rounds are eligible", got)
	}
	if got := toolContent(requests[11], "read6"); !strings.HasPrefix(got, "[output of read() hidden") {
		t.Fatalf("read6 at step 12 = %.40q, want it hidden", got)
	}
}

func TestSummaryReusesTheStepsRequestUnchanged(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(100_000)...)
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("c1", "read")}, Usage: providers.Usage{PromptTokens: 70_000}}},
		text("SUMMARY"),
		text("Done."),
	)

	run(t, f, model)

	requests := model.Requests()
	summary := requests[1]
	if summary.SystemPrompt != requests[0].SystemPrompt || len(summary.Tools) == 0 || len(summary.Tools) != len(requests[0].Tools) || summary.ToolChoice != requests[0].ToolChoice || summary.CacheKey != agenttest.SessionID {
		t.Fatalf("summary request does not reuse the step's prefix: %+v", summary)
	}
	if !reflect.DeepEqual(summary.Messages[:len(requests[0].Messages)], requests[0].Messages) || lastMessage(summary).Content != agentcontext.SummaryInstruction {
		t.Fatalf("summary messages = %+v", summary.Messages)
	}
	if first := requests[2].Messages[0]; !strings.Contains(first.Content, "SUMMARY") {
		t.Fatalf("next request starts with %+v", first)
	}
}

func TestRequestOverTheWindowIsSummarisedOnItsOwn(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(400_000)...)
	f.Window = 100_000
	model := agenttest.NewScriptedModel(text("SUMMARY"), text("Done."))

	run(t, f, model)

	if summary := model.Requests()[0]; !strings.HasPrefix(summary.SystemPrompt, "You compact matrixclaw chat histories") || len(summary.Tools) != 0 {
		t.Fatalf("summary request = %q with %d tools, want a standalone summary", summary.SystemPrompt, len(summary.Tools))
	}
}

func TestSecondOverflowExhaustsTheContext(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(80_000)...)
	overflow := agenttest.Turn{Err: errors.New("context_length_exceeded")}
	model := agenttest.NewScriptedModel(overflow, text("SUMMARY"), overflow)

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusFailed || outcome.StopReason != agent.StopContextExhausted || !errors.Is(outcome.Err, agent.ErrContextExhausted) || len(model.Requests()) != 3 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
}

func TestOverflowWithNothingToSummariseExhaustsTheContext(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(agenttest.Turn{Err: errors.New("context_length_exceeded")})

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusFailed || outcome.StopReason != agent.StopContextExhausted || len(model.Requests()) != 1 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
}

func TestToolCallsInAReusedRequestSummaryAreDropped(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(100_000)...)
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	first := agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("c1", "read")}, Usage: providers.Usage{PromptTokens: 70_000}}}
	summary := agenttest.Turn{Response: providers.Response{Text: "SUMMARY", ToolCalls: []providers.ToolCall{call("c2", "read")}, StopReason: providers.StopToolUse}}
	model := agenttest.NewScriptedModel(first, summary, text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 3 || len(f.Tools.Calls) != 1 {
		t.Fatalf("outcome = %+v requests = %d calls = %d", outcome, len(model.Requests()), len(f.Tools.Calls))
	}
	if marks := boundaries(f.Journal.Messages); len(marks) != 1 || marks[0].Compaction.Summary != "SUMMARY" {
		t.Fatalf("boundaries = %+v", marks)
	}
}

func TestReusedRequestSummaryWithOnlyToolCallsFallsBackToChunks(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(100_000)...)
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	first := agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("c1", "read")}, Usage: providers.Usage{PromptTokens: 70_000}}}
	model := agenttest.NewScriptedModel(first, calls(call("c2", "read")), text("SUMMARY"), text("Done."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 4 || !isStandaloneSummary(requests[2]) || len(f.Tools.Calls) != 1 {
		t.Fatalf("outcome = %+v requests = %d calls = %d", outcome, len(requests), len(f.Tools.Calls))
	}
	if marks := boundaries(f.Journal.Messages); len(marks) != 1 || marks[0].Compaction.Summary != "SUMMARY" {
		t.Fatalf("boundaries = %+v", marks)
	}
}

// signedSteps are n tool steps whose replies carry plain and signed reasoning.
func signedSteps(n int, name string) []agenttest.Turn {
	turns := toolSteps(n, name)
	for i := range turns {
		plain := fmt.Sprintf("plain%d", i+1)
		turns[i].Response.ReasoningContent = &plain
		turns[i].Response.Reasoning = []providers.ReasoningBlock{{Text: fmt.Sprintf("think%d", i+1), Signature: fmt.Sprintf("sig%d", i+1)}}
	}
	return turns
}

// signatures lists the signed reasoning and counts the plain reasoning of the
// assistant messages of request.
func signatures(request providers.Request) (signed []string, plain int) {
	for _, message := range request.Messages {
		for _, block := range message.Reasoning {
			signed = append(signed, block.Signature)
		}
		if message.ReasoningContent != nil {
			plain++
		}
	}
	return signed, plain
}

func TestSignedReasoningBeforeAHistoryEditIsNotReplayed(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 100_000
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result {
		return tools.Result{Content: call.ToolCallID + " " + strings.Repeat("x", 20_000)}
	}
	model := agenttest.NewScriptedModel(append(signedSteps(11, "read"), text("Done."))...)

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 12 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	edited := 0
	for i, request := range requests {
		if strings.HasPrefix(toolContent(request, "read1"), "[output of read() hidden") {
			edited = i
			break
		}
	}
	if edited == 0 {
		t.Fatal("no request elided read1")
	}
	if signed, plain := signatures(requests[edited-1]); len(signed) != edited-1 || plain != edited-1 {
		t.Fatalf("before the edit: signed = %v plain = %d, want all %d replayed", signed, plain, edited-1)
	}
	if signed, plain := signatures(requests[edited]); len(signed) != 0 || plain != edited {
		t.Fatalf("at the edit: signed = %v plain = %d, want only the %d plain texts", signed, plain, edited)
	}
	if signed, _ := signatures(requests[edited+1]); !reflect.DeepEqual(signed, []string{fmt.Sprintf("sig%d", edited+1)}) {
		t.Fatalf("after the edit: signed = %v, want only the newer step's", signed)
	}
}
