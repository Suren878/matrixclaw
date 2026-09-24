package agent_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func runTask(t *testing.T, f *agenttest.Fixture, task agent.Task) agent.Outcome {
	t.Helper()
	outcome, err := f.Engine().Run(context.Background(), task)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	return outcome
}

// counterTool returns new output on every call, so the loop guard never fires.
func counterTool() agenttest.ToolFunc {
	calls := 0
	return func(tools.Call) tools.Result {
		calls++
		return tools.Result{Content: fmt.Sprintf("output %d", calls)}
	}
}

func toolSteps(n int, name string) []agenttest.Turn {
	turns := make([]agenttest.Turn, 0, n)
	for i := 1; i <= n; i++ {
		turns = append(turns, calls(call(fmt.Sprintf("%s%d", name, i), name)))
	}
	return turns
}

func engineNotes(messages []transcript.Message) []transcript.Message {
	var notes []transcript.Message
	for _, message := range messages {
		if message.Origin == transcript.OriginEngine {
			notes = append(notes, message)
		}
	}
	return notes
}

func lastMessage(request providers.Request) providers.Message {
	if len(request.Messages) == 0 {
		return providers.Message{}
	}
	return request.Messages[len(request.Messages)-1]
}

func TestStepBudgetEndsWithAToolLessFinalTurn(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	model := agenttest.NewScriptedModel(append(toolSteps(3, "read"), text("Read three files; /continue reads the rest."))...)
	task := f.Task(model)
	task.Budget = agent.Budget{Steps: 3}

	outcome := runTask(t, f, task)

	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopBudgetExhausted || outcome.Assistant.Content != "Read three files; /continue reads the rest." {
		t.Fatalf("outcome = %+v", outcome)
	}
	requests := model.Requests()
	if len(requests) != 4 || len(f.Tools.Calls) != 3 {
		t.Fatalf("requests = %d tool calls = %d, want 4 and 3", len(requests), len(f.Tools.Calls))
	}
	for i, request := range requests[:3] {
		if request.ToolChoice != providers.ToolChoiceAuto {
			t.Fatalf("request %d tool choice = %q", i, request.ToolChoice)
		}
	}
	final := requests[3]
	if final.ToolChoice != providers.ToolChoiceNone || len(final.Tools) == 0 {
		t.Fatalf("final turn tool choice = %q with %d tools, want none with tools still defined", final.ToolChoice, len(final.Tools))
	}
	if note := lastMessage(final); note.Role != "user" || !strings.Contains(note.Content, "reached its budget (3 steps)") {
		t.Fatalf("final turn instruction = %+v", note)
	}
	notes := engineNotes(f.Journal.Messages)
	if len(notes) != 1 || notes[0].Role != transcript.MessageRoleSystem || notes[0].RunID != agenttest.RunID {
		t.Fatalf("engine notes = %+v", notes)
	}
}

func TestWrapUpNoteArrivesOnceAtEightyPercent(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	model := agenttest.NewScriptedModel(append(toolSteps(10, "read"), text("Summary."))...)
	task := f.Task(model)
	task.Budget = agent.Budget{Steps: 10}

	outcome := runTask(t, f, task)

	requests := model.Requests()
	if outcome.StopReason != agent.StopBudgetExhausted || len(requests) != 11 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	if strings.Contains(lastMessage(requests[7]).Content, "Budget note") {
		t.Fatal("wrap-up note arrived before 80% of the steps were used")
	}
	if note := lastMessage(requests[8]); note.Role != "user" || !strings.Contains(note.Content, "about 2 steps left") {
		t.Fatalf("request 9 ends with %+v, want the wrap-up note", note)
	}
	wrapUps := 0
	for _, note := range engineNotes(f.Journal.Messages) {
		if strings.Contains(note.Content, "Budget note") {
			wrapUps++
		}
	}
	if wrapUps != 1 {
		t.Fatalf("wrap-up notes = %d, want 1", wrapUps)
	}
}

func TestActiveTimeBudgetCountsTheRunsWorkingTime(t *testing.T) {
	f := agenttest.NewFixture()
	waits := 0
	f.Tools.Funcs["wait"] = func(tools.Call) tools.Result {
		waits++
		f.Clock = f.Clock.Add(10 * time.Minute)
		return tools.Result{Content: fmt.Sprintf("waited %d", waits)}
	}
	model := agenttest.NewScriptedModel(append(toolSteps(3, "wait"), text("Out of time."))...)
	task := f.Task(model)
	task.Budget = agent.Budget{ActiveTime: 25 * time.Minute}

	outcome := runTask(t, f, task)

	requests := model.Requests()
	if outcome.StopReason != agent.StopBudgetExhausted || len(requests) != 4 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	if note := lastMessage(requests[2]); !strings.Contains(note.Content, "about 5 minutes left") {
		t.Fatalf("request 3 ends with %+v, want the wrap-up note", note)
	}
	if final := requests[3]; final.ToolChoice != providers.ToolChoiceNone || !strings.Contains(lastMessage(final).Content, "active time of 25 minutes") {
		t.Fatalf("final turn = %q / %+v", final.ToolChoice, lastMessage(final))
	}
}

func TestResumedCountersKeepTheBudget(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["wait"] = func(tools.Call) tools.Result {
		f.Clock = f.Clock.Add(10 * time.Minute)
		return tools.Result{Content: "waited"}
	}
	model := agenttest.NewScriptedModel(calls(call("w1", "wait")), text("Out of time."))
	task := f.Task(model)
	task.Budget = agent.Budget{Steps: 10, ActiveTime: 25 * time.Minute}
	task.Resume = agent.Counters{Steps: 4, Active: 20 * time.Minute}

	outcome := runTask(t, f, task)

	requests := model.Requests()
	if outcome.StopReason != agent.StopBudgetExhausted || len(requests) != 2 || requests[1].ToolChoice != providers.ToolChoiceNone {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	last := f.Journal.States[len(f.Journal.States)-1].Counters
	if last.Steps != 5 || last.Active != 30*time.Minute || !last.WrapUpSent {
		t.Fatalf("last checkpoint counters = %+v", last)
	}
}

func TestTokenBudgetCountsEveryGeneration(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	usage := providers.Usage{PromptTokens: 40, OutputTokens: 10}
	model := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("r1", "read")}, Usage: usage}},
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("r2", "read")}, Usage: usage}},
		text("Token budget used."),
	)
	task := f.Task(model)
	task.Budget = agent.Budget{Tokens: 100}

	outcome := runTask(t, f, task)

	requests := model.Requests()
	if outcome.StopReason != agent.StopBudgetExhausted || len(requests) != 3 || !strings.Contains(lastMessage(requests[2]).Content, "reached its budget (100 tokens)") {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
}

func TestFinalTurnDropsToolCallsAndFallsBackToAStopNote(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	model := agenttest.NewScriptedModel(calls(call("r1", "read")), calls(call("r2", "read")))
	task := f.Task(model)
	task.Budget = agent.Budget{Steps: 1}

	outcome := runTask(t, f, task)

	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopBudgetExhausted || len(f.Tools.Calls) != 1 {
		t.Fatalf("outcome = %+v tool calls = %d", outcome, len(f.Tools.Calls))
	}
	if !strings.HasPrefix(outcome.Assistant.Content, "Stopped: the run reached its budget.") {
		t.Fatalf("final reply = %q", outcome.Assistant.Content)
	}
}

func TestEmptyFinalTurnEndsWithTheStopNote(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	model := agenttest.NewScriptedModel(calls(call("r1", "read")), text(""), text(""), text(""))
	task := f.Task(model)
	task.Budget = agent.Budget{Steps: 1}

	outcome := runTask(t, f, task)

	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopBudgetExhausted || !strings.HasPrefix(outcome.Assistant.Content, "Stopped: the run reached its budget.") {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestCheckpointsCarryTheRunCounters(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("c1", "read")}, Usage: providers.Usage{PromptTokens: 10, OutputTokens: 5}}},
		text("Done."),
	)

	run(t, f, model)

	if got := phases(f.Journal.States); got != "model,tool:c1,model,model" {
		t.Fatalf("checkpoints = %s", got)
	}
	if tool := f.Journal.States[1].Counters; tool.Steps != 1 || tool.Tokens != 15 {
		t.Fatalf("tool checkpoint counters = %+v", tool)
	}
}
