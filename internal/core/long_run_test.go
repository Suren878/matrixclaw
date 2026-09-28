package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestLongNativeRunCompactsKeepsLargeOutputsAndCompletes(t *testing.T) {
	t.Parallel()
	const steps = 120
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	now := runRecoveryTestTime()
	app.WithClock(func() time.Time { return now })
	defaults := core.DefaultRunBudgets()
	app.WithRunBudgets(core.RunBudgets{User: agent.Budget{Steps: steps + 10}, Subagent: defaults.Subagent, Automation: defaults.Automation})
	files := t.TempDir()
	app.WithSessionFiles(files)
	// Outputs grow to ~6k tokens; every 40th is ~15k tokens and goes to a file.
	// Each step also says ~300 tokens, which elision cannot hide, so the history
	// still needs summaries.
	outputs := map[int]string{}
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("probe", tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		n := len(outputs) + 1
		size := min(300*n, 24_000)
		if n%40 == 0 {
			size = 60_000
		}
		outputs[n] = fmt.Sprintf("probe %d\n%s", n, strings.Repeat("y", size))
		return tools.Result{Content: outputs[n]}, nil
	}}))
	limit := agentcontext.EffectiveWindow(60_000, int(providers.DefaultMaxOutputTokens))
	var mainRequests, summaries, largest int
	var problems []string
	app.WithSessionLLMs(windowLLMs{window: 60_000, recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		tokens := agentcontext.EstimateRequestTokens(request)
		largest = max(largest, tokens)
		usage := providers.Usage{PromptTokens: int64(tokens), OutputTokens: 10}
		if last := request.Messages[len(request.Messages)-1]; strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") || last.Content == agentcontext.SummaryInstruction {
			summaries++
			return providers.Response{Text: "SUMMARY: probing", Usage: usage}, nil
		}
		mainRequests++
		if split := agenttest.SplitToolPair(request); split != "" {
			problems = append(problems, fmt.Sprintf("request %d: %s", mainRequests, split))
		}
		if copies := agenttest.AssignmentCopies(request, "original task long"); copies != 1 {
			problems = append(problems, fmt.Sprintf("request %d carries the assignment %d times", mainRequests, copies))
		}
		if mainRequests > steps {
			return providers.Response{Text: "Probed everything.", Usage: usage}, nil
		}
		arguments := json.RawMessage(fmt.Sprintf(`{"n":%d}`, mainRequests))
		return providers.Response{Text: fmt.Sprintf("Probing %d. %s", mainRequests, strings.Repeat("p", 1200)), ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("probe_%d", mainRequests), Name: "probe", Arguments: arguments}}, Usage: usage}, nil
	})}})
	session, run := saveCrashRecoveryRun(t, db, "long", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if len(problems) > 0 {
		t.Fatalf("%d bad requests, the first: %s", len(problems), problems[0])
	}
	if mainRequests != steps+1 || len(outputs) != steps || largest > limit || summaries == 0 || summaries > steps/20 {
		t.Fatalf("main = %d outputs = %d largest = %d of %d summaries = %d", mainRequests, len(outputs), largest, limit, summaries)
	}
	messages := sessionMessages(t, db, session.ID)
	if split := agenttest.SplitBoundary(messages); split != "" {
		t.Fatal(split)
	}
	assertLongRunBoundaries(t, db, session.ID, run.ID, messages, summaries)
	assertLongRunSteps(t, db, run.ID, steps, summaries)
	assertLongRunOutputs(t, messages, outputs, filepath.Join(files, session.ID, "tool-output"))
	if last := messages[len(messages)-1]; last.Role != transcript.MessageRoleAssistant || last.Content != "Probed everything." {
		t.Fatalf("last message = %+v", last)
	}
}

// assertLongRunBoundaries checks that each boundary covers more than the one
// before it, keeps the assignment and at least halves the prompt, and that the
// newest one is what a run loads.
func assertLongRunBoundaries(t *testing.T, db *store.SQLiteStore, sessionID, runID string, messages []transcript.Message, want int) {
	t.Helper()
	var boundaries []transcript.Message
	var covered int64
	for _, message := range messages {
		c := message.Compaction
		if c == nil {
			continue
		}
		boundaries = append(boundaries, message)
		if message.Role != transcript.MessageRoleSystem || message.RunID != "" || message.Content != agentcontext.BoundaryLabel(*c) || c.RunID != runID ||
			c.CoversThroughSeq <= covered || c.CoversThroughSeq >= message.Seq || len(c.Kept) != 1 || c.Kept[0] != "User: original task long" || c.TokensAfter*2 > c.TokensBefore {
			t.Fatalf("boundary %d = %+v / %+v after one covering through %d", len(boundaries), message, c, covered)
		}
		covered = c.CoversThroughSeq
	}
	latest, err := db.LatestCompaction(context.Background(), sessionID)
	if err != nil || len(boundaries) != want || latest.ID != boundaries[len(boundaries)-1].ID {
		t.Fatalf("boundaries = %d, want one per summary (%d); latest = %q, %v", len(boundaries), want, latest.ID, err)
	}
}

// assertLongRunSteps checks the run's recorded steps: every model call and
// every summary.
func assertLongRunSteps(t *testing.T, db *store.SQLiteStore, runID string, steps, summaries int) {
	t.Helper()
	recorded, err := db.ListRunSteps(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	compact := 0
	for _, step := range recorded {
		if step.StopReason == "compact" {
			compact++
		}
	}
	if compact != summaries || len(recorded)-compact != steps+1 {
		t.Fatalf("run steps = %d with %d compact, want %d and %d", len(recorded), compact, steps+1+summaries, summaries)
	}
}

// assertLongRunOutputs checks that every call has one result and that exactly
// the outputs over the large-output size are kept whole in files under dir.
func assertLongRunOutputs(t *testing.T, messages []transcript.Message, outputs map[int]string, dir string) {
	t.Helper()
	calls, results, kept := map[string]int{}, map[string]int{}, 0
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolCall != nil {
				calls[part.ToolCall.ID]++
			}
			result := part.ToolResult
			if result == nil {
				continue
			}
			results[result.ToolCallID]++
			var n int
			if _, err := fmt.Sscanf(result.ToolCallID, "probe_%d", &n); err != nil {
				t.Fatalf("result for %q", result.ToolCallID)
			}
			large := agentcontext.EstimateTextTokens(outputs[n]) > agentcontext.LargeOutputTokens
			if !large {
				if result.OutputPath != "" || result.Content != outputs[n] {
					t.Fatalf("result %d kept in %q or changed", n, result.OutputPath)
				}
				continue
			}
			kept++
			full, err := os.ReadFile(result.OutputPath)
			if err != nil || filepath.Dir(result.OutputPath) != dir || string(full) != outputs[n] || !strings.HasPrefix(result.Content, "Output is ~") {
				t.Fatalf("large result %d: path %q (%v), content %.60q", n, result.OutputPath, err, result.Content)
			}
		}
	}
	if len(calls) != len(outputs) || len(results) != len(outputs) || kept != len(outputs)/40 {
		t.Fatalf("calls = %d results = %d kept in files = %d, want %d, %d and %d", len(calls), len(results), kept, len(outputs), len(outputs), len(outputs)/40)
	}
	for id, count := range calls {
		if count != 1 || results[id] != 1 {
			t.Fatalf("call %q made %d times with %d results", id, count, results[id])
		}
	}
}
