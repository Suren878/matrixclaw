package agent_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
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
	// A 20k window keeps a tail of ~2.5k tokens, the newest step; results under
	// 1k tokens are never elided, and the usage reported for the third step puts
	// the fourth over the summary threshold.
	f.Window = 20_000
	big := strings.Repeat("b", 3_800)
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result { return tools.Result{Content: call.ToolCallID + big} }
	f.Inbox.Steers = []string{"focus on the parser"}
	third := talkingCall(1_600, call("r3", "read"))
	third.Response.Usage.PromptTokens = 7_000
	model := agenttest.NewScriptedModel(talkingCall(1_600, call("r1", "read")), talkingCall(1_600, call("r2", "read")), third, text("SUMMARY"), text("Done."))

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

func TestSummariesUseTheCompactModel(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(100_000)...)
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	main := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("c1", "read")}, Usage: providers.Usage{PromptTokens: 70_000}}},
		text("Done."),
	)
	cheap := agenttest.NewScriptedModel(agenttest.Turn{Response: providers.Response{Text: "SUMMARY", Model: "cheap-model"}})
	task := f.Task(main)
	task.CompactModel = cheap

	outcome := runTask(t, f, task)

	if outcome.Status != agent.StatusCompleted || len(main.Requests()) != 2 || len(cheap.Requests()) != 1 {
		t.Fatalf("outcome = %+v main = %d cheap = %d", outcome, len(main.Requests()), len(cheap.Requests()))
	}
	if summary := cheap.Requests()[0]; !strings.HasPrefix(summary.SystemPrompt, "You compact matrixclaw chat histories") || len(summary.Tools) != 0 {
		t.Fatalf("compact model got %q with %d tools, want a standalone summary", summary.SystemPrompt, len(summary.Tools))
	}
	if !strings.Contains(main.Requests()[1].Messages[0].Content, "SUMMARY") {
		t.Fatalf("main request starts with %+v", main.Requests()[1].Messages[0])
	}
	if steps := f.Journal.Steps; len(steps) != 3 || steps[1].StopReason != "compact" || steps[1].Model != "cheap-model" {
		t.Fatalf("steps = %+v", steps)
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

// pastTurns is n earlier runs of the session, each a question and an answer
// of answerRunes.
func pastTurns(n int, answerRunes int) []transcript.Message {
	var history []transcript.Message
	for i := range n {
		for _, message := range pastTurn(answerRunes) {
			message.ID = fmt.Sprintf("%s_%d", message.ID, i)
			history = append(history, message)
		}
	}
	return history
}

// overThreshold is a tool step reporting a prompt of 70k tokens, over the
// summary threshold of a 100k window.
func overThreshold() agenttest.Turn {
	return agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("c1", "read")}, Usage: providers.Usage{PromptTokens: 70_000}}}
}

func isStandaloneSummary(request providers.Request) bool {
	return strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories")
}

func TestFailingCompactModelFallsBackToTheRunModel(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(100_000)...)
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	main := agenttest.NewScriptedModel(overThreshold(), text("RUN MODEL SUMMARY"), text("Done."))
	cheap := agenttest.NewScriptedModel(agenttest.Turn{Err: errors.New("cheap: status 404: model not found")})
	task := f.Task(main)
	task.CompactModel = cheap

	outcome := runTask(t, f, task)

	requests := main.Requests()
	if outcome.Status != agent.StatusCompleted || len(cheap.Requests()) != 1 || len(requests) != 3 || !isStandaloneSummary(requests[1]) {
		t.Fatalf("outcome = %+v cheap = %d main = %d", outcome, len(cheap.Requests()), len(requests))
	}
	if marks := boundaries(f.Journal.Messages); len(marks) != 1 || marks[0].Compaction.Summary != "RUN MODEL SUMMARY" {
		t.Fatalf("boundaries = %+v", marks)
	}
}

func TestEmptyReusedRequestSummaryFallsBackToChunks(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(100_000)...)
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(overThreshold(), text(""), text("SUMMARY"), text("Done."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 4 || lastMessage(requests[1]).Content != agentcontext.SummaryInstruction || !isStandaloneSummary(requests[2]) {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	if marks := boundaries(f.Journal.Messages); len(marks) != 1 || marks[0].Compaction.Summary != "SUMMARY" {
		t.Fatalf("boundaries = %+v", marks)
	}
}

func TestCompactModelChunksBySizeOfItsOwnWindow(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurns(12, 40_000)...)
	f.Window = 100_000
	main := agenttest.NewScriptedModel(text("Done."))
	var chunks []int
	cheap := agenttest.ModelFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		chunks = append(chunks, agentcontext.EstimateRequestTokens(request))
		return providers.Response{Text: "PART"}, nil
	})
	task := f.Task(main)
	task.CompactModel = cheap
	task.CompactWindowTokens = 20_000

	outcome := runTask(t, f, task)

	if outcome.Status != agent.StatusCompleted || len(chunks) < 4 {
		t.Fatalf("outcome = %+v chunk requests = %v, want one per covered answer", outcome, chunks)
	}
	for _, tokens := range chunks {
		if tokens > 10_000 {
			t.Fatalf("chunk requests = %v tokens, want each within the compact model's 20k window", chunks)
		}
	}
}

// seededRounds journals n tool rounds of the run, each result resultRunes long,
// and returns the seq of each round's reply.
func seededRounds(f *agenttest.Fixture, n int, resultRunes int) []int64 {
	var replies []int64
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("seeded%d", i)
		reply := transcript.Message{ID: id + "_reply", SessionID: agenttest.SessionID, RunID: agenttest.RunID, Role: transcript.MessageRoleAssistant, Content: "step",
			Parts: append(transcript.NormalizeMessageParts("step", nil), transcript.MessagePart{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: "tool_calls"}})}
		result, _ := agent.ToolResultMessage(id+"_result", agenttest.SessionID, agenttest.RunID, id, "read", tools.Result{Content: strings.Repeat("r", resultRunes)}, f.Clock)
		f.Journal.Seed(reply, agent.ToolCallMessage(id, agenttest.SessionID, agenttest.RunID, "read", []byte(`{}`), true, f.Clock), result)
		replies = append(replies, f.Journal.Messages[len(f.Journal.Messages)-3].Seq)
	}
	return replies
}

func TestSummarySavingsCountOnlyWhatTheElidedRequestHeld(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	replies := seededRounds(f, 8, 20_000)
	model := agenttest.NewScriptedModel(agenttest.Turn{Err: errors.New("context_length_exceeded")}, text("SUMMARY"), text("Done."))
	task := f.Task(model)
	task.Resume = agent.Counters{ElidedResults: replies[3] - 1}

	outcome := runTask(t, f, task)

	marks := boundaries(f.Journal.Messages)
	if outcome.Status != agent.StatusCompleted || len(marks) != 1 {
		t.Fatalf("outcome = %+v boundaries = %d", outcome, len(marks))
	}
	// The newest round, ~5k tokens, stays after the summary.
	if c := marks[0].Compaction; c.TokensAfter < 5_000 || c.TokensAfter >= c.TokensBefore {
		t.Fatalf("tokens %d -> %d, want the kept round counted", c.TokensBefore, c.TokensAfter)
	}
}

// extendsPrefix reports whether next starts with every message of previous.
func extendsPrefix(previous, next providers.Request) bool {
	return len(next.Messages) >= len(previous.Messages) && reflect.DeepEqual(next.Messages[:len(previous.Messages)], previous.Messages)
}

func TestElisionThenSummaryKeepsTheRequestOrderAndPrefix(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 100_000
	f.Prompts.ContextText = "plan: write the parser"
	f.Inbox.Steers = []string{"focus on the parser"}
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result {
		return tools.Result{Content: call.ToolCallID + " " + strings.Repeat("x", 8_000)}
	}
	// Six steps of ~2k tokens each; the sixth reports a prompt over the
	// elision threshold, so the seventh request hides read1. That request
	// overflows, and the retry starts from a summary of read1 and read2.
	turns := toolSteps(6, "read")
	turns[5].Response.Usage.PromptTokens = 50_000
	overflow := agenttest.Turn{Err: errors.New("anthropic: status 400: invalid_request_error: prompt is too long: 210000 tokens > 200000 maximum")}
	model := agenttest.NewScriptedModel(append(turns, overflow, text("SUMMARY"), calls(call("read7", "read")), text("Done."))...)

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 10 || !isStandaloneSummary(requests[7]) {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	for i := 1; i < 6; i++ {
		if !extendsPrefix(requests[i-1], requests[i]) {
			t.Fatalf("request %d does not extend request %d", i+1, i)
		}
	}
	elided := requests[6]
	if extendsPrefix(requests[5], elided) || !strings.HasPrefix(toolContent(elided, "read1"), "[output of read() hidden") || !strings.HasSuffix(toolContent(elided, "read1"), "User guidance: focus on the parser") {
		t.Fatalf("elided request read1 = %q", toolContent(elided, "read1"))
	}

	retry := requests[8].Messages
	if len(retry) != 10 {
		t.Fatalf("retry messages = %d, want summary, four steps and the context note", len(retry))
	}
	if summary := retry[0].Content; retry[0].Role != "user" || !strings.Contains(summary, "SUMMARY\n\nKept verbatim from that part:\n\nUser: do the task\n\nUser guidance: focus on the parser") {
		t.Fatalf("retry starts with %q", summary)
	}
	for i, id := range []string{"read3", "read4", "read5", "read6"} {
		step, result := retry[1+2*i], retry[2+2*i]
		if len(step.ToolCalls) != 1 || step.ToolCalls[0].ID != id || result.ToolCallID != id || !strings.HasPrefix(result.Content, id+" x") {
			t.Fatalf("retry step %d = %+v / %.40q", i+1, step.ToolCalls, result.Content)
		}
	}
	if note := retry[9]; note.Role != "user" || !strings.HasPrefix(note.Content, "Context update") || !strings.Contains(note.Content, "plan: write the parser") {
		t.Fatalf("retry ends with %+v", note)
	}
	if !extendsPrefix(requests[8], requests[9]) || len(requests[9].Messages) != 12 || requests[9].Messages[10].ToolCalls[0].ID != "read7" {
		t.Fatalf("the request after the retry does not extend it: %d messages", len(requests[9].Messages))
	}
	for _, request := range []providers.Request{requests[6], requests[8], requests[9]} {
		if request.SystemPrompt != requests[0].SystemPrompt || !reflect.DeepEqual(request.Tools, requests[0].Tools) {
			t.Fatal("the system prompt or tools changed during the run")
		}
	}
}
