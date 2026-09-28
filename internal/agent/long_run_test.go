package agent_test

import (
	"context"
	"fmt"
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
	// a call up to their cap and every step also says a line.
	cases := []struct {
		name         string
		outputRunes  int
		maxSummaries int
	}{
		// Five kept rounds of ~5k tokens fit under the summary threshold, so
		// elision carries the load and only the growing talk needs summaries.
		{name: "elision holds", outputRunes: 20_000, maxSummaries: steps / 40},
		// Five kept rounds of ~7.5k tokens are over the threshold, so a summary
		// is due about every fifth step.
		{name: "outputs outgrow elision", outputRunes: 30_000, maxSummaries: steps / 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := agenttest.NewFixture()
			f.Window = 60_000
			limit := agentcontext.EffectiveWindow(f.Window, int(providers.DefaultMaxOutputTokens))
			outputs := 0
			f.Tools.Funcs["read"] = func(tools.Call) tools.Result {
				outputs++
				return tools.Result{Content: fmt.Sprintf("output %d\n%s", outputs, strings.Repeat("x", min(400*outputs, tc.outputRunes)))}
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
			if largest > limit || summaries == 0 || summaries != len(marks) || summaries > tc.maxSummaries {
				t.Fatalf("largest request = %d of %d, summaries = %d (at most %d), boundaries = %d", largest, limit, summaries, tc.maxSummaries, len(marks))
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
		})
	}
}
