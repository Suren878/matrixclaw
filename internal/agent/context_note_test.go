package agent_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func contextNotes(messages []transcript.Message) []transcript.Message {
	var notes []transcript.Message
	for _, message := range messages {
		if message.Origin == transcript.OriginEngineModel && strings.HasPrefix(message.Content, "Context update") {
			notes = append(notes, message)
		}
	}
	return notes
}

func mentions(request providers.Request, text string) int {
	count := 0
	for _, message := range request.Messages {
		count += strings.Count(message.Content, text)
	}
	return count
}

func TestSystemPromptAndToolsAreFixedForTheRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Prompts.ContextText = "Plan: step 0"
	steps := 0
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result {
		steps++
		f.Prompts.Text = "system as of step " + call.ToolCallID
		f.Prompts.ContextText = "Plan: step " + call.ToolCallID
		f.Tools.Funcs["write"] = writeTool
		f.Tools.Asks["write"] = true
		return tools.Result{Content: "read " + call.ToolCallID}
	}
	model := agenttest.NewScriptedModel(calls(call("c1", "read")), calls(call("c2", "read")), text("Done."))

	run(t, f, model)

	requests := model.Requests()
	if f.Prompts.SystemCalls != 1 || f.Tools.SpecReads != 1 || len(requests) != 3 || steps != 2 {
		t.Fatalf("system prompt built %d times, tools listed %d times over %d requests", f.Prompts.SystemCalls, f.Tools.SpecReads, len(requests))
	}
	for _, request := range requests[1:] {
		if request.SystemPrompt != requests[0].SystemPrompt || len(request.Tools) != len(requests[0].Tools) {
			t.Fatalf("request prefix changed: %q / %d tools", request.SystemPrompt, len(request.Tools))
		}
	}
	if requests[0].SystemPrompt != "system" || !strings.Contains(lastMessage(requests[2]).Content, "Plan: step c2") {
		t.Fatalf("system prompt %q, last message %+v", requests[0].SystemPrompt, lastMessage(requests[2]))
	}
}

func TestContextNoteIsJournaledOnlyWhenItChanges(t *testing.T) {
	f := agenttest.NewFixture()
	f.Prompts.ContextText = "Plan: step A"
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result {
		if call.ToolCallID == "c2" {
			f.Prompts.ContextText = "Plan: step B"
		}
		return tools.Result{Content: "read " + call.ToolCallID}
	}
	model := agenttest.NewScriptedModel(calls(call("c1", "read")), calls(call("c2", "read")), text("Done."))

	run(t, f, model)

	notes := contextNotes(f.Journal.Messages)
	if len(notes) != 2 || !strings.Contains(notes[0].Content, "Plan: step A") || !strings.Contains(notes[1].Content, "Plan: step B") || notes[0].RunID != agenttest.RunID {
		t.Fatalf("context notes = %+v", notes)
	}
	requests := model.Requests()
	if last := lastMessage(requests[0]); last.Role != "user" || !strings.Contains(last.Content, "Plan: step A") {
		t.Fatalf("first request ends with %+v", last)
	}
	if mentions(requests[1], "Plan: step") != 1 {
		t.Fatalf("an unchanged context was sent again: %+v", requests[1].Messages)
	}
	if last := lastMessage(requests[2]); !strings.Contains(last.Content, "Plan: step B") {
		t.Fatalf("third request ends with %+v", last)
	}
}

func TestEmptyContextSendsNoNote(t *testing.T) {
	f := agenttest.NewFixture()

	run(t, f, agenttest.NewScriptedModel(text("Done.")))

	if notes := contextNotes(f.Journal.Messages); len(notes) != 0 {
		t.Fatalf("context notes = %+v", notes)
	}
}

func TestContextThatEmptiesIsWithdrawn(t *testing.T) {
	f := agenttest.NewFixture()
	f.Prompts.ContextText = "Plan: step A"
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result {
		f.Prompts.ContextText = ""
		return tools.Result{Content: "read"}
	}

	run(t, f, agenttest.NewScriptedModel(calls(call("c1", "read")), text("Done.")))

	notes := contextNotes(f.Journal.Messages)
	if len(notes) != 2 || strings.Contains(notes[1].Content, "Plan: step A") {
		t.Fatalf("context notes = %+v", notes)
	}
}

func TestResumedRunDoesNotRepeatAnUnchangedContextNote(t *testing.T) {
	f := agenttest.NewFixture()
	f.Prompts.ContextText = "Recovery notice: the daemon restarted."
	f.Tools.Funcs["read"] = readTool
	run(t, f, agenttest.NewScriptedModel(calls(call("c1", "read")), text("First.")))
	task := f.Task(agenttest.NewScriptedModel(text("Second.")))
	task.Resume = f.Journal.States[len(f.Journal.States)-1].Counters

	runTask(t, f, task)

	if notes := contextNotes(f.Journal.Messages); len(notes) != 1 {
		t.Fatalf("context notes = %d, want 1", len(notes))
	}
}

func TestRestartedRunWithoutItsCheckpointDoesNotRepeatTheContextNote(t *testing.T) {
	f := agenttest.NewFixture()
	f.Prompts.ContextText = "Plan: step A"
	run(t, f, agenttest.NewScriptedModel(text("First.")))

	runTask(t, f, f.Task(agenttest.NewScriptedModel(text("Second."))))

	if notes := contextNotes(f.Journal.Messages); len(notes) != 1 {
		t.Fatalf("context notes = %d, want 1", len(notes))
	}
}

func TestContextNoteIsSentAgainWhenASummaryCoversIt(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 20_000
	f.Prompts.ContextText = "Plan: parser"
	// Results under 1k tokens are never elided; the usage reported for the
	// fourth step puts the fifth over the summary threshold.
	big := strings.Repeat("b", 3_800)
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result { return tools.Result{Content: call.ToolCallID + big} }
	steps := toolSteps(4, "read")
	steps[3].Response.Usage.PromptTokens = 8_500
	model := agenttest.NewScriptedModel(append(steps, text("SUMMARY"), text("Done."))...)

	run(t, f, model)

	requests := model.Requests()
	if len(requests) != 6 || !isSummaryRequest(requests[4]) {
		t.Fatalf("requests = %d", len(requests))
	}
	last := requests[5]
	if notes := contextNotes(f.Journal.Messages); len(notes) != 2 || mentions(last, "Plan: parser") != 1 || !strings.Contains(lastMessage(last).Content, "Plan: parser") {
		t.Fatalf("notes = %d, last request = %+v", len(notes), last.Messages)
	}
}

func TestContextNoteIsSentAgainWhenAnOverflowSummaryCoversIt(t *testing.T) {
	f := agenttest.NewFixture()
	f.Prompts.ContextText = "Plan: parser"
	big := strings.Repeat("b", 40_000)
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result { return tools.Result{Content: call.ToolCallID + big} }
	turns := append(toolSteps(4, "read"), agenttest.Turn{Err: errors.New("context_length_exceeded")}, text("SUMMARY"), text("Done."))
	model := agenttest.NewScriptedModel(turns...)

	run(t, f, model)

	requests := model.Requests()
	if len(requests) != 7 || !isSummaryRequest(requests[5]) {
		t.Fatalf("requests = %d", len(requests))
	}
	retry := requests[6]
	if notes := contextNotes(f.Journal.Messages); len(notes) != 2 || mentions(retry, "Plan: parser") != 1 || !strings.Contains(lastMessage(retry).Content, "Plan: parser") {
		t.Fatalf("notes = %d, retry = %+v", len(notes), retry.Messages)
	}
}
