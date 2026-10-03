package agent_test

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// seedCall journals a call of the run that a restart left without a result.
func seedCall(f *agenttest.Fixture, id string, name string) {
	f.Journal.Seed(agent.ToolCallMessage(id, agenttest.SessionID, agenttest.RunID, name, []byte(`{}`), false, f.Clock))
}

func TestRecoveryRerunsAsksAndAnswersTheInterruptedCalls(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	f.Tools.Funcs["write"] = writeTool
	f.Tools.Asks["write"] = true
	seedCall(f, "r1", "read")
	seedCall(f, "w1", "write")
	seedCall(f, "u1", "gone")
	task := f.Task(agenttest.NewScriptedModel(text("never asked")))
	task.Recovering = true
	task.Interrupted = []agent.InterruptedCall{
		{ToolCallID: "r1", Settle: agent.SettleRerun},
		{ToolCallID: "w1", Settle: agent.SettleAsk, Request: tools.ApprovalRequest{Description: "retry after the restart"}},
		{ToolCallID: "u1", Settle: agent.SettleAnswer, Result: tools.Result{Content: "unknown after restart", Status: tools.ResultStatusError}},
	}

	outcome, err := f.Engine().Run(t.Context(), task)

	if err != nil || outcome.Status != agent.StatusWaitingApproval {
		t.Fatalf("outcome = %+v err = %v", outcome, err)
	}
	if len(f.Approvals.Requests) != 1 || f.Approvals.Requests[0].ToolCallID != "w1" || f.Approvals.Requests[0].Request.Description != "retry after the restart" {
		t.Fatalf("requests = %+v", f.Approvals.Requests)
	}
	if result, ok := f.Journal.Result("u1"); !ok || result.Content != "unknown after restart" {
		t.Fatalf("u1 result = %+v", result)
	}
	if _, ok := f.Journal.Result("r1"); ok || len(f.Tools.Calls) != 0 {
		t.Fatal("the read ran while the run waits for approval")
	}
	if message, _ := f.Journal.Message("r1"); !message.Parts[0].ToolCall.Deferred {
		t.Fatal("the read to run again is not deferred")
	}
}

func TestRecoveryRerunsTheDeferredCallsWhenNothingIsAsked(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	seedCall(f, "r1", "read")
	model := agenttest.NewScriptedModel(text("Read it."))
	task := f.Task(model)
	task.Recovering = true
	task.Interrupted = []agent.InterruptedCall{{ToolCallID: "r1", Settle: agent.SettleRerun}}

	outcome, err := f.Engine().Run(t.Context(), task)

	if err != nil || outcome.Status != agent.StatusCompleted {
		t.Fatalf("outcome = %+v err = %v", outcome, err)
	}
	if result, ok := f.Journal.Result("r1"); !ok || result.Content != "file body" || len(f.Tools.Calls) != 1 {
		t.Fatalf("r1 result = %+v, calls = %d", result, len(f.Tools.Calls))
	}
}

func TestRecoverySealsTheReplyARestartCutOff(t *testing.T) {
	f := agenttest.NewFixture()
	f.Journal.Seed(transcript.Message{ID: "partial", SessionID: agenttest.SessionID, RunID: agenttest.RunID, Role: transcript.MessageRoleAssistant, Content: "half an answer", CreatedAt: f.Clock})
	model := agenttest.NewScriptedModel(text("Whole answer."))
	task := f.Task(model)
	task.Recovering = true

	outcome := runTask(t, f, task)

	if outcome.Status != agent.StatusCompleted {
		t.Fatalf("outcome = %+v", outcome)
	}
	partial, _ := f.Journal.Message("partial")
	if !transcript.HasFinishReason(partial, transcript.FinishReasonDaemonRestart) {
		t.Fatalf("partial reply = %+v", partial)
	}
	for _, message := range model.Requests()[0].Messages {
		if message.Content == "half an answer" {
			t.Fatal("the model read the interrupted reply")
		}
	}
}
