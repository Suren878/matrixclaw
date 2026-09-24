package agent_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type limitedModel struct {
	*agenttest.ScriptedModel
	current, ceiling int64
}

func (m limitedModel) OutputLimits() (int64, int64) { return m.current, m.ceiling }

func cut(text string) agenttest.Turn {
	return agenttest.Turn{Response: providers.Response{Text: text, StopReason: providers.StopMaxTokens}}
}

func TestCutReplyIsKeptAndContinued(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(cut("First half"), text("second half."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopDone || outcome.Assistant.Content != "second half." {
		t.Fatalf("outcome = %+v", outcome)
	}
	partial := f.Journal.Messages[1]
	if partial.Role != transcript.MessageRoleAssistant || partial.Content != "First half" || !hasFinish(partial, "max_tokens") {
		t.Fatalf("partial reply = %+v", partial)
	}
	if notes := engineNotes(f.Journal.Messages); len(notes) != 1 || !strings.Contains(notes[0].Content, "cut by the output limit") || notes[0].Origin != transcript.OriginEngineModel {
		t.Fatalf("engine notes = %+v", notes)
	}
	second := model.Requests()[1]
	n := len(second.Messages)
	if second.Messages[n-2].Role != "assistant" || second.Messages[n-2].Content != "First half" || !strings.Contains(second.Messages[n-1].Content, "Continue exactly where you stopped") {
		t.Fatalf("second request tail = %+v", second.Messages[n-2:])
	}
}

func TestCutReplyKeepsTheWhitespaceAtTheCut(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(cut("  Counted two "), cut(" files and"), text(" one folder.\n"))

	outcome := run(t, f, model)

	var cuts []string
	for _, message := range f.Journal.Messages {
		if hasFinish(message, "max_tokens") {
			cuts = append(cuts, message.Content)
		}
	}
	if len(cuts) != 2 || cuts[0] != "Counted two " || cuts[1] != " files and" {
		t.Fatalf("cut replies = %q, want the start of the first trimmed and the rest kept", cuts)
	}
	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != " one folder." {
		t.Fatalf("final reply = %q, want its leading space kept after a cut", outcome.Assistant.Content)
	}
	if got := transcript.RunReply(append(f.Journal.Messages, *outcome.Assistant), agenttest.RunID); got != "Counted two  files and one folder." {
		t.Fatalf("RunReply = %q", got)
	}
}

func TestFourthCutInARowFailsTheRun(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(cut("part 1"), cut("part 2"), cut("part 3"), cut("part 4"))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusFailed || !outcome.MarkErrored || outcome.Assistant == nil || outcome.Assistant.Content != "part 4" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if outcome.Err == nil || outcome.Err.Error() != "reply cut by the output limit 4 times in a row" {
		t.Fatalf("error = %v", outcome.Err)
	}
	if len(model.Requests()) != 4 || len(engineNotes(f.Journal.Messages)) != 3 {
		t.Fatalf("requests = %d notes = %d", len(model.Requests()), len(engineNotes(f.Journal.Messages)))
	}
}

func TestContinuationsResetAfterAToolStep(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	model := agenttest.NewScriptedModel(cut("a"), cut("b"), cut("c"), calls(call("r1", "read")), cut("d"), text("end."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "end." {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestToolCallsCutByTheLimitStillRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("r1", "read")}, StopReason: providers.StopMaxTokens}},
		text("Done."),
	)

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(f.Tools.Calls) != 1 {
		t.Fatalf("outcome = %+v tool calls = %d", outcome, len(f.Tools.Calls))
	}
}

func TestEmptyCutRaisesTheOutputLimitOnce(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		current, ceiling, want int64
	}{
		{"capped by the model", 4000, 6000, 6000},
		{"doubled", 4000, 0, 8000},
		{"capped when the model's ceiling is unknown", 20000, 0, 32768},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := agenttest.NewFixture()
			model := limitedModel{ScriptedModel: agenttest.NewScriptedModel(cut(""), text("Done.")), current: tc.current, ceiling: tc.ceiling}

			outcome := run(t, f, model)

			requests := model.Requests()
			if outcome.Status != agent.StatusCompleted || len(requests) != 2 {
				t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
			}
			if requests[0].MaxOutputTokens != 0 || int64(requests[1].MaxOutputTokens) != tc.want {
				t.Fatalf("output limits = %d then %d, want 0 then %d", requests[0].MaxOutputTokens, requests[1].MaxOutputTokens, tc.want)
			}
			if len(engineNotes(f.Journal.Messages)) != 0 {
				t.Fatal("a raised limit must not journal a note")
			}
		})
	}
}

func TestCutReplyWithTextRaisesTheLimitBeforeContinuing(t *testing.T) {
	f := agenttest.NewFixture()
	model := limitedModel{ScriptedModel: agenttest.NewScriptedModel(cut("I'll write the file"), cut("and here it"), text("is.")), current: 4000}

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 3 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	if requests[1].MaxOutputTokens != 8000 || requests[2].MaxOutputTokens != 8000 {
		t.Fatalf("output limits = %d then %d, want the raised 8000 kept", requests[1].MaxOutputTokens, requests[2].MaxOutputTokens)
	}
	if !strings.Contains(lastMessage(requests[1]).Content, "Continue exactly where you stopped") {
		t.Fatalf("second request ends with %+v, want the continuation note", lastMessage(requests[1]))
	}
}

func TestEmptyCutFailsWhenTheLimitCannotBeRaised(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model func() agent.Model
		want  int
	}{
		{"second empty cut", func() agent.Model {
			return limitedModel{ScriptedModel: agenttest.NewScriptedModel(cut(""), cut("")), current: 4000}
		}, 2},
		{"no output limit", func() agent.Model { return agenttest.NewScriptedModel(cut("")) }, 1},
		{"unknown ceiling already reached", func() agent.Model {
			return limitedModel{ScriptedModel: agenttest.NewScriptedModel(cut("")), current: 32768}
		}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := agenttest.NewFixture()
			model := tc.model()

			outcome := run(t, f, model)

			if outcome.Status != agent.StatusFailed || outcome.Err == nil || outcome.Err.Error() != "reply cut by the output limit before any text" {
				t.Fatalf("outcome = %+v", outcome)
			}
			requests := len(model.(interface{ Requests() []providers.Request }).Requests())
			if requests != tc.want {
				t.Fatalf("requests = %d, want %d", requests, tc.want)
			}
		})
	}
}

func TestProviderStopsCompleteTheRun(t *testing.T) {
	for _, tc := range []struct {
		reason providers.StopReason
		text   string
		want   string
	}{
		{providers.StopRefusal, "I can't help with that.", "I can't help with that."},
		{providers.StopContentFilter, "", fmt.Sprintf("The provider stopped this reply (%s).", providers.StopContentFilter)},
	} {
		t.Run(string(tc.reason), func(t *testing.T) {
			f := agenttest.NewFixture()
			model := agenttest.NewScriptedModel(agenttest.Turn{Response: providers.Response{Text: tc.text, StopReason: tc.reason}})

			outcome := run(t, f, model)

			if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopDone || outcome.Assistant.Content != tc.want || len(model.Requests()) != 1 {
				t.Fatalf("outcome = %+v", outcome)
			}
		})
	}
}

func TestCutOrFilteredFinalTurnCompletesWithItsStopReason(t *testing.T) {
	for _, tc := range []struct {
		name  string
		final providers.Response
		want  string
	}{
		{"cut with text", providers.Response{Text: "Read one file; the rest", StopReason: providers.StopMaxTokens}, "Read one file; the rest"},
		{"cut before any text", providers.Response{StopReason: providers.StopMaxTokens}, "This run stopped: it reached its budget."},
		{"filtered", providers.Response{StopReason: providers.StopContentFilter}, "This run stopped: it reached its budget."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := agenttest.NewFixture()
			f.Tools.Funcs["read"] = counterTool()
			model := limitedModel{ScriptedModel: agenttest.NewScriptedModel(calls(call("r1", "read")), agenttest.Turn{Response: tc.final}), current: 4000}
			task := f.Task(model)
			task.Budget = agent.Budget{Steps: 1}

			outcome := runTask(t, f, task)

			if outcome.Status != agent.StatusCompleted || outcome.MarkErrored || outcome.StopReason != agent.StopBudgetExhausted || outcome.Assistant.Content != tc.want {
				t.Fatalf("outcome = %+v", outcome)
			}
			if requests := len(model.Requests()); requests != 2 {
				t.Fatalf("requests = %d, want the final turn taken once", requests)
			}
		})
	}
}

func TestCancelAfterACutReplyKeepsTheStreamedPreview(t *testing.T) {
	f := agenttest.NewFixture()
	ctx, cancel := context.WithCancel(context.Background())
	model := agenttest.ModelFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
		if err := providers.StreamText(ctx, "Writing"); err != nil {
			return providers.Response{}, err
		}
		cancel()
		return providers.Response{Text: "Writing the", StopReason: providers.StopMaxTokens}, nil
	})

	outcome, err := f.Engine().Run(ctx, f.Task(model))

	if err != nil || outcome.Status != agent.StatusInterrupted || outcome.Assistant == nil || !outcome.AssistantSaved || outcome.Assistant.Content != "Writing" {
		t.Fatalf("outcome = %+v err = %v, want the streamed preview to seal", outcome, err)
	}
}
