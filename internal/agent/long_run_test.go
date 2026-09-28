package agent_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestTwoHundredStepRunWithGrowingOutputsStaysWithinTheWindow(t *testing.T) {
	const steps = 200
	// The window leaves ~40.6k tokens of prompt room; outputs grow by 100 tokens
	// a call up to ~7.5k, so five rounds outgrow the 30% kept whole, and every
	// step also says a line.
	f := agenttest.NewFixture()
	f.Window = 60_000
	limit := agentcontext.EffectiveWindow(f.Window, int(providers.DefaultMaxOutputTokens))
	outputs := 0
	f.Tools.Funcs["read"] = func(tools.Call) tools.Result {
		outputs++
		return tools.Result{Content: fmt.Sprintf("output %d\n%s", outputs, strings.Repeat("x", min(400*outputs, 30_000)))}
	}
	var mainRequests, summaries, largest int
	var problems []string
	elided := false
	model := agenttest.ModelFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		tokens := agentcontext.EstimateRequestTokens(request)
		largest = max(largest, tokens)
		usage := providers.Usage{PromptTokens: int64(tokens), OutputTokens: 20}
		if isSummaryRequest(request) {
			summaries++
			return providers.Response{Text: fmt.Sprintf("SUMMARY %d: reading outputs", summaries), Usage: usage}, nil
		}
		mainRequests++
		if split := agenttest.SplitToolPair(request); split != "" {
			problems = append(problems, fmt.Sprintf("request %d: %s", mainRequests, split))
		}
		if copies := agenttest.AssignmentCopies(request, "do the task"); copies != 1 {
			problems = append(problems, fmt.Sprintf("request %d carries the assignment %d times", mainRequests, copies))
		}
		for _, message := range request.Messages {
			elided = elided || strings.HasPrefix(message.Content, "[output of read(")
		}
		if mainRequests > steps {
			return providers.Response{Text: "All outputs read.", Usage: usage}, nil
		}
		talk := fmt.Sprintf("Reading part %d. %s", mainRequests, strings.Repeat("t", 400))
		arguments := []byte(fmt.Sprintf(`{"n":%d}`, mainRequests))
		return providers.Response{Text: talk, ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("c%d", mainRequests), Name: "read", Arguments: arguments}}, Usage: usage}, nil
	})
	task := f.Task(model)
	task.Budget = agent.Budget{Steps: steps + 10}

	outcome := runTask(t, f, task)

	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopDone || outcome.Assistant.Content != "All outputs read." {
		t.Fatalf("outcome = %+v", outcome)
	}
	if mainRequests != steps+1 || len(f.Tools.Calls) != steps || !elided {
		t.Fatalf("main requests = %d tool calls = %d elided = %v", mainRequests, len(f.Tools.Calls), elided)
	}
	if len(problems) > 0 {
		t.Fatalf("%d bad requests, the first: %s", len(problems), problems[0])
	}
	marks := boundaries(f.Journal.Messages)
	if largest > limit || summaries == 0 || summaries != len(marks) || summaries > steps/40 {
		t.Fatalf("largest request = %d of %d, summaries = %d (at most %d), boundaries = %d", largest, limit, summaries, steps/40, len(marks))
	}
	if split := agenttest.SplitBoundary(f.Journal.Messages); split != "" {
		t.Fatal(split)
	}
	var covered int64
	for i, mark := range marks {
		c := mark.Compaction
		if len(c.Kept) != 1 || c.Kept[0] != "User: do the task" || c.CoversThroughSeq <= covered || c.TokensAfter*2 > c.TokensBefore {
			t.Fatalf("boundary %d = %+v after one covering through %d", i, c, covered)
		}
		covered = c.CoversThroughSeq
	}
}

func TestProviderOverflowTeachesTheRunTheRealWindow(t *testing.T) {
	// The window is unknown, so ~105k tokens of prompt room are assumed, but the
	// provider rejects prompts over 30k.
	const real = 30_000
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result {
		return tools.Result{Content: fmt.Sprintf("output %s\n%s", call.ToolCallID, strings.Repeat("x", 6_000))}
	}
	var mainRequests, overflows, largestAfter int
	var chunkLimits []int
	model := agenttest.ModelFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		tokens := agentcontext.EstimateRequestTokens(request)
		if overflows > 0 {
			largestAfter = max(largestAfter, tokens)
		}
		if tokens > real {
			overflows++
			return providers.Response{}, fmt.Errorf("provider: context_length_exceeded (%d tokens)", tokens)
		}
		usage := providers.Usage{PromptTokens: int64(tokens), OutputTokens: 20}
		if strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") {
			chunkLimits = append(chunkLimits, request.MaxOutputTokens)
		}
		if isSummaryRequest(request) {
			return providers.Response{Text: "SUMMARY: reading outputs", Usage: usage}, nil
		}
		mainRequests++
		if mainRequests > 60 {
			return providers.Response{Text: "All outputs read.", Usage: usage}, nil
		}
		arguments := []byte(fmt.Sprintf(`{"n":%d}`, mainRequests))
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("c%d", mainRequests), Name: "read", Arguments: arguments}}, Usage: usage}, nil
	})

	outcome := runTask(t, f, f.Task(model))

	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "All outputs read." {
		t.Fatalf("outcome = %+v", outcome)
	}
	if overflows != 1 || largestAfter > real*9/10 {
		t.Fatalf("overflows = %d, largest request after the first = %d", overflows, largestAfter)
	}
	if len(chunkLimits) == 0 || slices.Contains(chunkLimits, 0) {
		t.Fatalf("chunked summary output limits = %v", chunkLimits)
	}
	learned := f.Journal.States[len(f.Journal.States)-1].Counters.LearnedLimit
	if learned <= 0 || learned >= real {
		t.Fatalf("checkpointed learned limit = %d", learned)
	}
}
