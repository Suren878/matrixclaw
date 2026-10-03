package agent_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func text(value string) agenttest.Turn {
	return agenttest.Turn{Response: providers.Response{Text: value}}
}

func calls(toolCalls ...providers.ToolCall) agenttest.Turn {
	return agenttest.Turn{Response: providers.Response{ToolCalls: toolCalls}}
}

func call(id, name string) providers.ToolCall {
	return providers.ToolCall{ID: id, Name: name, Arguments: []byte(`{}`)}
}

func run(t *testing.T, f *agenttest.Fixture, model agent.Model) agent.Outcome {
	t.Helper()
	outcome, err := f.Engine().Run(context.Background(), f.Task(model))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	return outcome
}

// phases renders checkpoints as phase[:call+call[/deferred+deferred]].
func phases(states []agent.State) string {
	out := make([]string, 0, len(states))
	for _, state := range states {
		phase := string(state.Phase)
		if state.Batch != nil {
			phase += ":" + strings.Join(state.Batch.CallIDs, "+")
			if len(state.Batch.DeferredIDs) > 0 {
				phase += "/" + strings.Join(state.Batch.DeferredIDs, "+")
			}
		}
		out = append(out, phase)
	}
	return strings.Join(out, ",")
}

func hasFinish(message transcript.Message, reason string) bool {
	return transcript.HasFinishReason(message, reason)
}

func toolContent(request providers.Request, callID string) string {
	for _, message := range request.Messages {
		if message.ToolCallID == callID {
			return message.Content
		}
	}
	return ""
}

func writeTool(call tools.Call) tools.Result {
	if !call.Approved {
		return tools.Result{Approval: &tools.ApprovalRequest{ToolID: "write", ToolCallID: call.ToolCallID, Action: "write"}}
	}
	return tools.Result{Content: "written"}
}

func readTool(tools.Call) tools.Result {
	return tools.Result{Content: "file body"}
}

func TestTextReplyCompletesWithFinalMessage(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(agenttest.Turn{Response: providers.Response{
		Text: "Hello.", Model: "m1", Provider: "p1", Usage: providers.Usage{PromptTokens: 5, OutputTokens: 1},
	}})

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopDone || outcome.Assistant == nil || outcome.AssistantSaved {
		t.Fatalf("outcome = %+v", outcome)
	}
	reply := *outcome.Assistant
	if reply.Content != "Hello." || reply.Model != "m1" || reply.Provider != "p1" || !hasFinish(reply, "end_turn") {
		t.Fatalf("reply = %+v", reply)
	}
	if len(f.Journal.Steps) != 1 || f.Journal.Steps[0].Usage.PromptTokens != 5 || f.Journal.Steps[0].Model != "m1" || f.Journal.Steps[0].RunID != agenttest.RunID {
		t.Fatalf("steps = %+v", f.Journal.Steps)
	}
	if got := phases(f.Journal.States); got != "model" {
		t.Fatalf("checkpoints = %s", got)
	}
}

func TestToolRoundTripIsJournaledInOrder(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{Text: "Reading.", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "read", Arguments: []byte(`{"path":"a"}`)}}}},
		text("Done."),
	)

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Done." {
		t.Fatalf("outcome = %+v", outcome)
	}
	messages := f.Journal.Messages
	if len(messages) != 4 || messages[1].Content != "Reading." || !hasFinish(messages[1], "tool_calls") {
		t.Fatalf("messages = %+v", messages)
	}
	if part := messages[2].Parts[0].ToolCall; messages[2].ID != "c1" || part == nil || !part.Finished || part.Input != `{"path":"a"}` {
		t.Fatalf("call message = %+v", messages[2])
	}
	if messages[3].Content != "file body" || messages[3].Parts[0].ToolResult.ToolCallID != "c1" {
		t.Fatalf("result message = %+v", messages[3])
	}
	wantKinds := []agent.EventKind{agent.EventMessageCreated, agent.EventMessageCreated, agent.EventToolRequested, agent.EventMessageUpdated, agent.EventMessageCreated, agent.EventToolFinished}
	if fmt.Sprint(f.Sink.Kinds()) != fmt.Sprint(wantKinds) {
		t.Fatalf("events = %v, want %v", f.Sink.Kinds(), wantKinds)
	}
	if got := phases(f.Journal.States); got != "model,tool_batch:c1,model" {
		t.Fatalf("checkpoints = %s", got)
	}
	executed := f.Tools.Calls[0]
	if executed.ToolCallID != "c1" || executed.WorkingDir != "/work" || executed.Approved || executed.RunID != agenttest.RunID {
		t.Fatalf("executed call = %+v", executed)
	}
	if got := toolContent(model.Requests()[1], "c1"); got != "file body" {
		t.Fatalf("second request tool content = %q", got)
	}
	if fmt.Sprint(f.Tools.Finished) != "[c1]" {
		t.Fatalf("finished = %v", f.Tools.Finished)
	}
}

func TestUpdatedMessageEventsCarryTheStoredSeq(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(calls(call("c1", "read")), text("Done."))

	run(t, f, model)

	updates := 0
	for _, event := range f.Sink.Events {
		if event.Kind != agent.EventMessageUpdated {
			continue
		}
		updates++
		stored, ok := f.Journal.Message(event.Message.ID)
		if !ok || event.Message.Seq == 0 || event.Message.Seq != stored.Seq {
			t.Fatalf("updated %s seq = %d, stored %d", event.Message.ID, event.Message.Seq, stored.Seq)
		}
	}
	if updates == 0 {
		t.Fatal("no message.updated events")
	}
}

func lookupTool(call tools.Call) tools.Result {
	if !call.Approved {
		return tools.Result{Approval: &tools.ApprovalRequest{ToolID: "lookup", ToolCallID: call.ToolCallID, Action: "lookup"}}
	}
	return tools.Result{Content: "looked up"}
}

// executedIDs lists the executed calls sorted, as calls of a batch run in any order.
func executedIDs(f *agenttest.Fixture) string {
	ids := make([]string, 0, len(f.Tools.Calls))
	for _, call := range f.Tools.Calls {
		ids = append(ids, call.ToolCallID)
	}
	slices.Sort(ids)
	return strings.Join(ids, ",")
}

func TestApprovalThatIsNotABarrierLetsTheBatchRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["lookup"] = lookupTool
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(calls(call("l1", "lookup"), call("r1", "read")))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusWaitingApproval {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(f.Approvals.Requests) != 1 || f.Approvals.Requests[0].ToolCallID != "l1" || f.Approvals.Requests[0].Request.Action != "lookup" {
		t.Fatalf("approval requests = %+v", f.Approvals.Requests)
	}
	if _, ok := f.Journal.Result("r1"); !ok {
		t.Fatal("the read after a non-barrier approval did not run")
	}
	if _, ok := f.Journal.Result("l1"); ok {
		t.Fatal("unapproved lookup has a result")
	}
	if message, _ := f.Journal.Message("l1"); message.Parts[0].ToolCall.Finished {
		t.Fatal("pending call marked finished")
	}
}

func TestMutatingApprovalDefersTheRestOfTheBatch(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	f.Tools.Funcs["read"] = readTool
	f.Tools.Mutating = map[string]bool{"write": true}
	model := agenttest.NewScriptedModel(calls(call("r1", "read"), call("w1", "write"), call("r2", "read"), call("w2", "write")), text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusWaitingApproval || len(f.Approvals.Requests) != 1 || f.Approvals.Requests[0].ToolCallID != "w1" {
		t.Fatalf("outcome = %+v requests = %+v", outcome, f.Approvals.Requests)
	}
	if got := executedIDs(f); got != "r1,w1" {
		t.Fatalf("executed = %s, want r1,w1", got)
	}
	for _, id := range []string{"r2", "w2"} {
		if message, ok := f.Journal.Message(id); !ok || !message.Parts[0].ToolCall.Deferred {
			t.Fatalf("%s not journaled deferred: %+v", id, message)
		}
		if _, ok := f.Journal.Result(id); ok {
			t.Fatalf("%s has a result before the decision", id)
		}
	}
	for _, event := range f.Sink.Events {
		if event.Kind == agent.EventToolRequested && (event.ToolCallID == "r2" || event.ToolCallID == "w2") {
			t.Fatalf("deferred call %s announced as requested", event.ToolCallID)
		}
	}

	f.Approvals.Open = false
	f.Inbox.Decided = []agent.Input{{Kind: agent.InputDecided, ToolCallID: "w1", ToolName: "write", WorkingDir: "/work", Args: []byte(`{}`)}}
	outcome = run(t, f, model)

	if outcome.Status != agent.StatusWaitingApproval || len(f.Approvals.Requests) != 2 || f.Approvals.Requests[1].ToolCallID != "w2" {
		t.Fatalf("after the grant: outcome = %+v requests = %+v", outcome, f.Approvals.Requests)
	}
	if got := executedIDs(f); got != "r1,r2,w1,w1,w2" {
		t.Fatalf("executed = %s, want r1,r2,w1,w1,w2", got)
	}
	if message, _ := f.Journal.Message("w2"); message.Parts[0].ToolCall.Deferred {
		t.Fatal("the new barrier is still marked deferred")
	}
	if len(model.Requests()) != 1 {
		t.Fatalf("model called %d times while a barrier was open", len(model.Requests()))
	}

	f.Approvals.Open = false
	f.Inbox.Decided = append(f.Inbox.Decided, agent.Input{Kind: agent.InputDecided, ToolCallID: "w2", ToolName: "write", WorkingDir: "/work", Args: []byte(`{}`)})
	outcome = run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 2 {
		t.Fatalf("after the second grant: outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
	request := model.Requests()[1]
	for id, want := range map[string]string{"r1": "file body", "w1": "written", "r2": "file body", "w2": "written"} {
		if got := toolContent(request, id); got != want {
			t.Fatalf("result of %s = %q, want %q", id, got, want)
		}
	}
	if split := agenttest.SplitToolPair(request); split != "" {
		t.Fatal(split)
	}
}

func TestBatchCheckpointsNameTheCallsAndTheDeferredOnes(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	f.Tools.Funcs["read"] = readTool
	f.Tools.Mutating = map[string]bool{"write": true}
	model := agenttest.NewScriptedModel(calls(call("r1", "read"), call("w1", "write"), call("r2", "read")), text("Done."))

	run(t, f, model)

	if got := phases(f.Journal.States); got != "model,tool_batch:r1+w1+r2/r2,model" {
		t.Fatalf("checkpoints = %s", got)
	}

	f.Approvals.Open = false
	f.Inbox.Decided = []agent.Input{{Kind: agent.InputDecided, ToolCallID: "w1", ToolName: "write", WorkingDir: "/work", Args: []byte(`{}`)}}
	run(t, f, model)

	if got := phases(f.Journal.States[3:]); got != "tool_batch:w1,tool_batch:r2,model" {
		t.Fatalf("checkpoints after the grant = %s", got)
	}
}

func TestDeniedBarrierAnswersTheCallsItHeldBack(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	f.Tools.Funcs["read"] = readTool
	f.Tools.Mutating = map[string]bool{"write": true}
	model := agenttest.NewScriptedModel(calls(call("r1", "read"), call("w1", "write"), call("r2", "read"), call("w2", "write")), text("Done."))
	run(t, f, model)

	f.Approvals.Open = false
	f.Inbox.Decided = []agent.Input{{Kind: agent.InputDecided, ToolCallID: "w1", ToolName: "write", WorkingDir: "/work", Args: []byte(`{}`), Denied: true, Reason: "not yet"}}
	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 2 {
		t.Fatalf("after the denial: outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
	if got := executedIDs(f); got != "r1,w1" {
		t.Fatalf("executed = %s, want r1,w1", got)
	}
	request := model.Requests()[1]
	notRun := "Not run: an earlier call in this batch was denied (write)."
	for id, want := range map[string]string{"r1": "file body", "w1": "User denied: not yet", "r2": notRun, "w2": notRun} {
		if got := toolContent(request, id); got != want {
			t.Fatalf("result of %s = %q, want %q", id, got, want)
		}
	}
	if split := agenttest.SplitToolPair(request); split != "" {
		t.Fatal(split)
	}
}

func TestResolvedApprovalContinuesTheSameRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	f.Approvals.Grant = func(p agent.Pending) {
		f.Inbox.Decided = append(f.Inbox.Decided, agent.Input{Kind: agent.InputDecided, ToolCallID: p.ToolCallID, ToolName: p.ToolName, WorkingDir: "/work", Args: []byte(`{}`)})
	}
	model := agenttest.NewScriptedModel(calls(call("w1", "write")), text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 2 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
	if len(f.Tools.Calls) != 2 || f.Tools.Calls[0].Approved || !f.Tools.Calls[1].Approved {
		t.Fatalf("calls = %+v", f.Tools.Calls)
	}
	if result, ok := f.Journal.Result("w1"); !ok || result.Content != "written" {
		t.Fatalf("result = %+v", result)
	}
}

func TestGrantedApprovalIsExecutedBeforeTheNextModelCall(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	f.Journal.Seed(agent.ToolCallMessage("w1", agenttest.SessionID, agenttest.RunID, "write", []byte(`{}`), false, f.Clock))
	f.Inbox.Decided = []agent.Input{{Kind: agent.InputDecided, ToolCallID: "w1", ToolName: "write", WorkingDir: "/work", Args: []byte(`{}`)}}
	model := agenttest.NewScriptedModel(text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(f.Tools.Calls) != 1 || !f.Tools.Calls[0].Approved {
		t.Fatalf("outcome = %+v calls = %+v", outcome, f.Tools.Calls)
	}
	if message, _ := f.Journal.Message("w1"); !message.Parts[len(message.Parts)-1].ToolCall.Finished {
		t.Fatal("approved call not marked finished")
	}
	if got := toolContent(model.Requests()[0], "w1"); got != "written" {
		t.Fatalf("request tool content = %q", got)
	}
}

func TestUnknownToolIsReturnedAsErrorResult(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(calls(call("x1", "missing")), text("Fixed."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(f.Tools.Calls) != 0 {
		t.Fatalf("outcome = %+v calls = %+v", outcome, f.Tools.Calls)
	}
	if message, _ := f.Journal.Message("x1"); !message.Parts[0].ToolCall.Finished {
		t.Fatal("rejected call not marked finished")
	}
	result, ok := f.Journal.Result("x1")
	if !ok || !result.Parts[0].ToolResult.IsError || !strings.Contains(result.Content, `unknown tool "missing"`) {
		t.Fatalf("result = %+v", result)
	}
	for _, kind := range f.Sink.Kinds() {
		if kind == agent.EventToolRequested {
			t.Fatal("rejected call emitted tool.requested")
		}
	}
}

func TestSteerIsAppendedToTheNextToolResult(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	f.Inbox.Steers = []string{"check the logs"}
	model := agenttest.NewScriptedModel(calls(call("r1", "read")), text("Done."))

	run(t, f, model)

	result, _ := f.Journal.Result("r1")
	if result.Content != "file body\n\nUser guidance: check the logs" || len(f.Inbox.Steers) != 0 {
		t.Fatalf("result = %q steers left = %v", result.Content, f.Inbox.Steers)
	}
	if guidance := result.Parts[0].ToolResult.Guidance; len(guidance) != 1 || guidance[0] != "check the logs" {
		t.Fatalf("recorded guidance = %q", guidance)
	}
	if got := toolContent(model.Requests()[1], "r1"); !strings.Contains(got, "User guidance: check the logs") {
		t.Fatalf("request tool content = %q", got)
	}
}

func TestSteerStaysPendingWhenItsToolResultIsNotWritten(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	f.Inbox.Steers = []string{"check the logs"}
	f.Journal.OnAppend = func(msg transcript.Message) error {
		if msg.Role == transcript.MessageRoleTool {
			return errors.New("disk full")
		}
		return nil
	}

	outcome := run(t, f, agenttest.NewScriptedModel(calls(call("r1", "read"))))

	if outcome.Status != agent.StatusFailed {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(f.Inbox.Steers) != 1 || f.Inbox.Steers[0] != "check the logs" {
		t.Fatalf("steers left = %v, want the unwritten steer still pending", f.Inbox.Steers)
	}
}

func TestEmptyRepliesAreRetriedTwiceAndRecorded(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(agenttest.Turn{}, agenttest.Turn{}, text("ok"))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 3 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
	if len(f.Journal.Steps) != 3 {
		t.Fatalf("recorded steps = %d, want every generation (3)", len(f.Journal.Steps))
	}
	if fmt.Sprint(f.Slept) != "[200ms 750ms]" {
		t.Fatalf("backoffs = %v", f.Slept)
	}
}

func TestPartialOutputIsNotRetried(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(agenttest.Turn{Stream: []string{"Unfinished"}, Err: providers.ErrIncompleteResponse})

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusFailed || !outcome.MarkErrored || !outcome.AssistantSaved || outcome.Assistant.Content != "Unfinished" || !errors.Is(outcome.Err, providers.ErrIncompleteResponse) {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(model.Requests()) != 1 {
		t.Fatalf("requests = %d", len(model.Requests()))
	}
}

func TestStreamingProgressIsBatched(t *testing.T) {
	f := agenttest.NewFixture()
	deltas := make([]string, 4096)
	for i := range deltas {
		deltas[i] = "x"
	}
	model := agenttest.NewScriptedModel(agenttest.Turn{Stream: deltas, Response: providers.Response{Text: strings.Repeat("x", 4096)}})

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || !outcome.AssistantSaved {
		t.Fatalf("outcome = %+v", outcome)
	}
	if f.Journal.Begins != 1 || f.Journal.Streams != 1 {
		t.Fatalf("progress writes = %d begins + %d streams, want 1 + 1", f.Journal.Begins, f.Journal.Streams)
	}
}

func TestCancellationSeenAfterGenerationSealsTheReply(t *testing.T) {
	f := agenttest.NewFixture()
	f.Inbox.Cancel = true

	outcome := run(t, f, agenttest.NewScriptedModel(text("Hi")))

	if outcome.Status != agent.StatusCanceled || outcome.Assistant == nil {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestStoppedContextReturnsInterruptedWithTheReachedReply(t *testing.T) {
	f := agenttest.NewFixture()
	ctx, cancel := context.WithCancel(context.Background())
	model := agenttest.ModelFunc(func(context.Context, providers.Request) (providers.Response, error) {
		cancel()
		return providers.Response{Text: "late answer"}, nil
	})

	outcome, err := f.Engine().Run(ctx, f.Task(model))

	if err != nil || outcome.Status != agent.StatusInterrupted || outcome.Reached != agent.StatusCompleted || outcome.Assistant.Content != "late answer" {
		t.Fatalf("outcome = %+v err = %v", outcome, err)
	}
}

func TestLoadFailureFailsTheRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Journal.LoadErr = errors.New("disk gone")

	outcome := run(t, f, agenttest.NewScriptedModel())

	if outcome.Status != agent.StatusFailed || outcome.Err == nil || outcome.Err.Error() != "disk gone" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestCancelAfterToolCallsGenerationKeepsTheStreamedPreview(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	ctx, cancel := context.WithCancel(context.Background())
	model := agenttest.ModelFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
		if err := providers.StreamText(ctx, "Checking"); err != nil {
			return providers.Response{}, err
		}
		cancel()
		return providers.Response{Text: "Checking the file.", ToolCalls: []providers.ToolCall{call("c1", "read")}}, nil
	})

	outcome, err := f.Engine().Run(ctx, f.Task(model))

	if err != nil || outcome.Status != agent.StatusInterrupted || outcome.Assistant == nil || !outcome.AssistantSaved || outcome.Assistant.Content != "Checking" {
		t.Fatalf("outcome = %+v err = %v, want the streamed preview to seal", outcome, err)
	}
	if len(f.Tools.Calls) != 0 {
		t.Fatalf("tools ran after the stop: %+v", f.Tools.Calls)
	}
}

func TestReusedToolCallIDsFailTheRun(t *testing.T) {
	cases := []struct {
		name  string
		seed  []transcript.Message
		calls []providers.ToolCall
		want  string
	}{
		{
			name:  "same response",
			calls: []providers.ToolCall{call("c1", "read"), {ID: "c1", Name: "read", Arguments: []byte(`{"path":"b"}`)}},
			want:  `tool call ID "c1" reused with different arguments`,
		},
		{
			name:  "earlier step",
			seed:  []transcript.Message{agent.ToolCallMessage("c1", agenttest.SessionID, agenttest.RunID, "read", []byte(`{"path":"a"}`), true, time.Time{})},
			calls: []providers.ToolCall{call("c1", "read")},
			want:  `tool call ID "c1" reused with different arguments`,
		},
		{
			name:  "another run",
			seed:  []transcript.Message{agent.ToolCallMessage("c1", agenttest.SessionID, "run_0", "read", []byte(`{}`), true, time.Time{})},
			calls: []providers.ToolCall{call("c1", "read")},
			want:  `tool call ID "c1" belongs to another run`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := agenttest.NewFixture()
			f.Tools.Funcs["read"] = readTool
			f.Journal.Seed(tc.seed...)

			outcome := run(t, f, agenttest.NewScriptedModel(calls(tc.calls...)))

			if outcome.Status != agent.StatusFailed || outcome.Err == nil || outcome.Err.Error() != tc.want {
				t.Fatalf("outcome = %+v, want error %q", outcome, tc.want)
			}
		})
	}
}

func TestRetryableErrorBeforeOutputIsRetried(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(agenttest.Turn{Err: providers.ErrIncompleteResponse}, text("ok"))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "ok" || len(model.Requests()) != 2 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
	if len(f.Journal.Steps) != 1 {
		t.Fatalf("recorded steps = %d, want only the successful generation", len(f.Journal.Steps))
	}
}

func TestCustomInstructionsFollowTheSystemPromptUnderOneLabel(t *testing.T) {
	f := agenttest.NewFixture()
	f.Prompts.Custom = "  Answer in French.  "
	model := agenttest.NewScriptedModel(text("ok"))

	run(t, f, model)

	if got := model.Requests()[0].SystemPrompt; got != "system\n\nUser custom instructions:\nAnswer in French." {
		t.Fatalf("system prompt = %q", got)
	}
}

func TestRateLimitWaitsForRetryAfterWithinTheCap(t *testing.T) {
	for _, tc := range []struct {
		retryAfter time.Duration
		requests   int
	}{{5 * time.Second, 2}, {10 * time.Minute, 1}} {
		f := agenttest.NewFixture()
		limited := &providers.APIError{Provider: "p", Status: 429, Kind: providers.ErrorRateLimit, RetryAfter: tc.retryAfter}
		model := agenttest.NewScriptedModel(agenttest.Turn{Err: limited}, text("ok"))

		run(t, f, model)

		if len(model.Requests()) != tc.requests {
			t.Fatalf("retry after %s: requests = %d, want %d", tc.retryAfter, len(model.Requests()), tc.requests)
		}
		if tc.requests == 2 && (len(f.Slept) != 1 || f.Slept[0] != tc.retryAfter) {
			t.Fatalf("retry after %s: slept %v", tc.retryAfter, f.Slept)
		}
	}
}

func TestRateLimitWithoutRetryAfterBacksOffForSeconds(t *testing.T) {
	f := agenttest.NewFixture()
	limited := &providers.APIError{Provider: "p", Status: 429, Kind: providers.ErrorRateLimit}
	model := agenttest.NewScriptedModel(agenttest.Turn{Err: limited}, agenttest.Turn{Err: limited}, text("ok"))

	run(t, f, model)

	if len(model.Requests()) != 3 || len(f.Slept) != 2 || f.Slept[0] < time.Second || f.Slept[1] <= f.Slept[0] {
		t.Fatalf("requests = %d slept = %v", len(model.Requests()), f.Slept)
	}
}

func TestStopDuringRetryBackoffEndsTheWait(t *testing.T) {
	f := agenttest.NewFixture()
	f.RealSleep = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	model := agenttest.ModelFunc(func(context.Context, providers.Request) (providers.Response, error) {
		attempts++
		time.AfterFunc(20*time.Millisecond, cancel)
		return providers.Response{}, providers.ErrIncompleteResponse
	})

	started := time.Now()
	outcome, err := f.Engine().Run(ctx, f.Task(model))

	if err != nil || outcome.Status != agent.StatusInterrupted || attempts != 1 {
		t.Fatalf("outcome = %+v err = %v attempts = %d", outcome, err, attempts)
	}
	if elapsed := time.Since(started); elapsed >= 200*time.Millisecond {
		t.Fatalf("waited %v, want the backoff cut short", elapsed)
	}
}

func TestCancelWhileStreamingSealsThePartialReply(t *testing.T) {
	f := agenttest.NewFixture()
	var streamErr error
	attempts := 0
	model := agenttest.ModelFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
		attempts++
		if err := providers.StreamText(ctx, "Partial"); err != nil {
			return providers.Response{}, err
		}
		f.Inbox.Cancel = true
		f.Clock = f.Clock.Add(time.Second)
		streamErr = providers.StreamText(ctx, " more")
		return providers.Response{}, streamErr
	})

	outcome := run(t, f, model)

	if streamErr == nil || streamErr.Error() != "run canceled" || attempts != 1 {
		t.Fatalf("stream error = %v attempts = %d", streamErr, attempts)
	}
	if outcome.Status != agent.StatusCanceled || !outcome.AssistantSaved || outcome.Assistant.Content != "Partial" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestStoppedContextWithOpenApprovalReportsTheParkedState(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	ctx, cancel := context.WithCancel(context.Background())
	f.Approvals.OnRequest = func(agent.Pending) error {
		cancel()
		return nil
	}

	outcome, err := f.Engine().Run(ctx, f.Task(agenttest.NewScriptedModel(calls(call("w1", "write")))))

	if err != nil || outcome.Status != agent.StatusInterrupted || outcome.Reached != agent.StatusWaitingApproval || len(f.Approvals.Requests) != 1 {
		t.Fatalf("outcome = %+v err = %v requests = %+v", outcome, err, f.Approvals.Requests)
	}
}

func TestApprovalRequestFailureFailsTheRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	f.Approvals.OnRequest = func(agent.Pending) error { return errors.New("approvals unavailable") }

	outcome := run(t, f, agenttest.NewScriptedModel(calls(call("w1", "write"))))

	if outcome.Status != agent.StatusFailed || outcome.Err == nil || outcome.Err.Error() != "approvals unavailable" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if result, _ := f.Journal.Result("w1"); !strings.Contains(result.Content, "run failed") || len(f.Tools.Finished) != 1 {
		t.Fatalf("result of the call = %q, want the run's failure", result.Content)
	}
}

func TestDeniedCallGetsTheDenialAsItsResult(t *testing.T) {
	for _, tc := range []struct{ reason, want string }{
		{"use the staging database", "User denied: use the staging database"},
		{"", "User denied."},
	} {
		f := agenttest.NewFixture()
		f.Tools.Funcs["write"] = writeTool
		f.Journal.Seed(agent.ToolCallMessage("w1", agenttest.SessionID, agenttest.RunID, "write", []byte(`{}`), false, f.Clock))
		f.Inbox.Decided = []agent.Input{{Kind: agent.InputDecided, ToolCallID: "w1", ToolName: "write", WorkingDir: "/work", Args: []byte(`{}`), Denied: true, Reason: tc.reason}}
		model := agenttest.NewScriptedModel(text("Understood."))

		outcome := run(t, f, model)

		if outcome.Status != agent.StatusCompleted || len(f.Tools.Calls) != 0 {
			t.Fatalf("outcome = %+v calls = %+v", outcome, f.Tools.Calls)
		}
		if result, ok := f.Journal.Result("w1"); !ok || !result.Parts[0].ToolResult.IsError || result.Content != tc.want {
			t.Fatalf("result = %+v", result)
		}
		if message, _ := f.Journal.Message("w1"); !message.Parts[0].ToolCall.Finished {
			t.Fatal("denied call not marked finished")
		}
		if got := toolContent(model.Requests()[0], "w1"); got != tc.want {
			t.Fatalf("request tool content = %q, want %q", got, tc.want)
		}
	}
}
