package agent_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// pastTurn is an earlier run of the session: a question and a long answer.
func pastTurn(answerRunes int) []transcript.Message {
	answer := strings.Repeat("a", answerRunes)
	return []transcript.Message{
		{ID: "past_q", SessionID: agenttest.SessionID, RunID: "run_0", Role: transcript.MessageRoleUser, Content: "earlier question", Parts: transcript.NormalizeMessageParts("earlier question", nil)},
		{ID: "past_a", SessionID: agenttest.SessionID, RunID: "run_0", Role: transcript.MessageRoleAssistant, Content: answer, Parts: transcript.NormalizeMessageParts(answer, nil)},
	}
}

func boundaries(messages []transcript.Message) []transcript.Message {
	var out []transcript.Message
	for _, message := range messages {
		if message.Compaction != nil {
			out = append(out, message)
		}
	}
	return out
}

func isSummaryRequest(request providers.Request) bool {
	if strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") {
		return true
	}
	last := lastMessage(request)
	return last.Role == "user" && last.Content == agentcontext.SummaryInstruction
}

func summaryCount(requests []providers.Request) int {
	count := 0
	for _, request := range requests {
		if isSummaryRequest(request) {
			count++
		}
	}
	return count
}

func TestSummaryReplacesOlderHistoryBeforeTheModelCall(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(400_000)...)
	f.Window = 100_000
	model := agenttest.NewScriptedModel(text("SUMMARY"), text("Done."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 2 || !isSummaryRequest(requests[0]) {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	main := requests[1]
	if len(main.Messages) != 2 || main.Messages[0].Role != "user" || !strings.Contains(main.Messages[0].Content, "SUMMARY") || main.Messages[1].Content != "do the task" || strings.Contains(main.SystemPrompt, "SUMMARY") {
		t.Fatalf("main request = %q / %+v", main.SystemPrompt, main.Messages)
	}
	marks := boundaries(f.Journal.Messages)
	answer, _ := f.Journal.Message("past_a")
	if len(marks) != 1 || marks[0].Role != transcript.MessageRoleSystem || marks[0].RunID != "" {
		t.Fatalf("boundaries = %+v", marks)
	}
	if c := marks[0].Compaction; c.Summary != "SUMMARY" || c.CoversThroughSeq != answer.Seq || c.RunID != agenttest.RunID || c.TokensBefore <= c.TokensAfter || len(c.Kept) != 0 {
		t.Fatalf("compaction = %+v", c)
	}
	if len(f.Journal.Steps) != 2 || f.Journal.Steps[0].StopReason != "compact" {
		t.Fatalf("steps = %+v, want the summary recorded as compact first", f.Journal.Steps)
	}
}

func TestRequestOverTheThresholdIsSummarisedBeforeSending(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(100_000)...)
	f.Prompts.Text = strings.Repeat("s", 240_000)
	f.Window = 100_000
	model := agenttest.NewScriptedModel(text("SUMMARY"), text("Done."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 2 || !isSummaryRequest(requests[0]) {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	if first := requests[1].Messages[0]; !strings.Contains(first.Content, "SUMMARY") {
		t.Fatalf("main request starts with %+v", first)
	}
}

func TestContextLengthErrorSummarisesAndRetriesOnce(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(80_000)...)
	model := agenttest.NewScriptedModel(agenttest.Turn{Err: errors.New("context_length_exceeded")}, text("SUMMARY"), text("Recovered."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Recovered." || len(requests) != 3 || !isSummaryRequest(requests[1]) {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	if first := requests[2].Messages[0]; !strings.Contains(first.Content, "SUMMARY") {
		t.Fatalf("retry starts with %+v", first)
	}
	if len(boundaries(f.Journal.Messages)) != 1 {
		t.Fatalf("boundaries = %+v", boundaries(f.Journal.Messages))
	}
}

func TestSummaryRetriesATransientFailure(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(80_000)...)
	model := agenttest.NewScriptedModel(agenttest.Turn{Err: errors.New("context_length_exceeded")}, agenttest.Turn{Err: io.ErrUnexpectedEOF}, text("SUMMARY"), text("Recovered."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Recovered." || len(model.Requests()) != 4 || len(f.Slept) != 1 {
		t.Fatalf("outcome = %+v requests = %d slept = %v", outcome, len(model.Requests()), f.Slept)
	}
}

// talkingCall is a tool step whose reply also says replyRunes of text.
func talkingCall(replyRunes int, toolCall providers.ToolCall) agenttest.Turn {
	return agenttest.Turn{Response: providers.Response{Text: strings.Repeat("t", replyRunes), ToolCalls: []providers.ToolCall{toolCall}}}
}

func TestSummaryKeepsTheAssignmentStepsAndWholeToolSteps(t *testing.T) {
	f := agenttest.NewFixture()
	// A 20k window keeps a tail of ~2.5k tokens, the newest step; the usage
	// reported for the third step puts the fourth over the summary threshold.
	f.Window = 20_000
	big := strings.Repeat("b", 10_000)
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result { return tools.Result{Content: call.ToolCallID + big} }
	f.Inbox.Steers = []string{"focus on the parser"}
	third := talkingCall(400, call("r3", "read"))
	third.Response.Usage.PromptTokens = 7_000
	model := agenttest.NewScriptedModel(talkingCall(400, call("r1", "read")), talkingCall(400, call("r2", "read")), third, text("SUMMARY"), text("Done."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 5 || !isSummaryRequest(requests[3]) {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	marks := boundaries(f.Journal.Messages)
	r2, _ := f.Journal.Result("r2")
	if len(marks) != 1 || marks[0].Compaction.CoversThroughSeq != r2.Seq {
		t.Fatalf("boundaries = %+v", marks)
	}
	if kept := marks[0].Compaction.Kept; len(kept) != 2 || kept[0] != "User: do the task" || kept[1] != "User guidance: focus on the parser" {
		t.Fatalf("kept = %q", kept)
	}
	assertBoundaryAndLastToolStep(t, requests[4])

	again := agenttest.NewScriptedModel(text("Again."))
	run(t, f, again)
	assertBoundaryAndLastToolStep(t, again.Requests()[0])
}

// assertBoundaryAndLastToolStep checks a request that starts from the boundary:
// the summary with the kept texts, then the whole r3 step.
func assertBoundaryAndLastToolStep(t *testing.T, request providers.Request) {
	t.Helper()
	messages := request.Messages
	if len(messages) != 3 || !strings.Contains(messages[0].Content, "User: do the task") || !strings.Contains(messages[0].Content, "User guidance: focus on the parser") {
		t.Fatalf("request messages = %+v", messages)
	}
	if len(messages[1].ToolCalls) != 1 || messages[1].ToolCalls[0].ID != "r3" || messages[2].ToolCallID != "r3" {
		t.Fatalf("tail = %+v", messages[1:])
	}
}

func TestLowYieldSummariesEndTheRunAsContextExhausted(t *testing.T) {
	f := agenttest.NewFixture()
	// Each step says ~10k tokens and reports a prompt near the 63k threshold;
	// the summaries come back as long as what they replace, so neither saves a
	// tenth of the prompt.
	f.Window = 100_000
	f.Tools.Funcs["read"] = counterTool()
	step := func(id string, prompt int64) agenttest.Turn {
		turn := talkingCall(40_000, call(id, "read"))
		turn.Response.Usage.PromptTokens = prompt
		return turn
	}
	sum1, sum2 := text("SUM1 "+strings.Repeat("s", 40_000)), text("SUM2 "+strings.Repeat("s", 100_000))
	reply := "Stopped: the context is full; the parser is half done."
	model := agenttest.NewScriptedModel(step("c1", 50_000), step("c2", 60_000), sum1, step("c3", 70_000), sum2, step("c4", 90_000), text(reply))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusFailed || outcome.StopReason != agent.StopContextExhausted || !errors.Is(outcome.Err, agent.ErrContextExhausted) || !outcome.MarkErrored {
		t.Fatalf("outcome = %+v", outcome)
	}
	requests := model.Requests()
	if outcome.Assistant == nil || outcome.Assistant.Content != reply || len(requests) != 7 || summaryCount(requests) != 2 {
		t.Fatalf("assistant = %+v requests = %d summaries = %d", outcome.Assistant, len(requests), summaryCount(requests))
	}
	if final := requests[6]; final.ToolChoice != providers.ToolChoiceNone || !strings.Contains(lastMessage(final).Content, "no longer fits") {
		t.Fatalf("final turn = %q / %+v", final.ToolChoice, lastMessage(final))
	}
}

func TestSummaryIsSkippedWhenItWouldReplaceLittle(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	first := calls(call("c1", "read"))
	first.Response.Usage.PromptTokens = 90_000
	model := agenttest.NewScriptedModel(first, text("Done."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Done." || len(requests) != 2 || summaryCount(requests) != 0 {
		t.Fatalf("outcome = %+v requests = %d summaries = %d", outcome, len(requests), summaryCount(requests))
	}
	if len(boundaries(f.Journal.Messages)) != 0 || f.Journal.States[len(f.Journal.States)-1].Counters.LowYield != 0 {
		t.Fatalf("boundaries = %+v states = %+v", boundaries(f.Journal.Messages), f.Journal.States)
	}
}

func TestContinuationCarriesTheEarlierRunsKeptTexts(t *testing.T) {
	f := agenttest.NewFixture()
	history := pastTurn(400_000)
	earlier := transcript.Message{
		ID: "earlier_boundary", SessionID: agenttest.SessionID, Role: transcript.MessageRoleSystem, Content: "Context compacted.",
		Compaction: &transcript.Compaction{Summary: "EARLIER", Kept: []string{"User: first ask"}, RunID: "run_0", CoversThroughSeq: 1},
	}
	f.WithHistory(history[0], earlier, history[1])
	f.Window = 100_000
	model := agenttest.NewScriptedModel(text("SUMMARY"), text("Done."))
	task := f.Task(model)
	task.Continues = []string{"run_0"}

	outcome, err := f.Engine().Run(context.Background(), task)

	if err != nil || outcome.Status != agent.StatusCompleted {
		t.Fatalf("outcome = %+v err = %v", outcome, err)
	}
	marks := boundaries(f.Journal.Messages)
	if len(marks) != 2 || len(marks[1].Compaction.Kept) != 1 || marks[1].Compaction.Kept[0] != "User: first ask" {
		t.Fatalf("boundaries = %+v", marks)
	}
}
