package agent_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// signal fails the test instead of hanging when an expected signal never comes.
func signal(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case id := <-ch:
		return id
	case <-time.After(10 * time.Second):
		t.Fatal("a tool call never signalled")
		return ""
	}
}

// gatedTool announces each call on started, waits for its release channel and
// announces its return on returned.
type gatedTool struct {
	started, returned chan string
	release           map[string]chan struct{}
}

func newGatedTool(ids ...string) *gatedTool {
	g := &gatedTool{started: make(chan string), returned: make(chan string, len(ids)), release: map[string]chan struct{}{}}
	for _, id := range ids {
		g.release[id] = make(chan struct{})
	}
	return g
}

func (g *gatedTool) run(call tools.Call) tools.Result {
	g.started <- call.ToolCallID
	<-g.release[call.ToolCallID]
	g.returned <- call.ToolCallID
	return tools.Result{Content: "body of " + call.ToolCallID}
}

// start runs the fixture's engine on a goroutine of its own.
func start(f *agenttest.Fixture, model agent.Model) <-chan agent.Outcome {
	outcome := make(chan agent.Outcome, 1)
	engine, task := f.Engine(), f.Task(model)
	go func() {
		result, err := engine.Run(context.Background(), task)
		if err != nil {
			result = agent.Outcome{Status: agent.StatusFailed, Err: err}
		}
		outcome <- result
	}()
	return outcome
}

func resultOrder(f *agenttest.Fixture) string {
	var ids []string
	for _, message := range f.Journal.Messages {
		for _, part := range message.Parts {
			if part.ToolResult != nil {
				ids = append(ids, part.ToolResult.ToolCallID)
			}
		}
	}
	return strings.Join(ids, ",")
}

func eventOrder(f *agenttest.Fixture, kind agent.EventKind) string {
	var ids []string
	for _, event := range f.Sink.Events {
		if event.Kind == kind {
			ids = append(ids, event.ToolCallID)
		}
	}
	return strings.Join(ids, ",")
}

func TestReadsOfOneReplyRunAtOnceAndAreJournaledInCallOrder(t *testing.T) {
	f := agenttest.NewFixture()
	gate := newGatedTool("r1", "r2", "r3")
	f.Tools.Funcs["read"] = gate.run
	model := agenttest.NewScriptedModel(calls(call("r1", "read"), call("g1", "ghost"), call("r2", "read"), call("r3", "read")), text("Done."))
	outcome := start(f, model)

	for range 3 {
		signal(t, gate.started)
	}
	for _, id := range []string{"r3", "r2", "r1"} {
		close(gate.release[id])
		if got := signal(t, gate.returned); got != id {
			t.Fatalf("returned %s, want %s", got, id)
		}
	}

	if got := <-outcome; got.Status != agent.StatusCompleted {
		t.Fatalf("outcome = %+v", got)
	}
	if got := resultOrder(f); got != "r1,g1,r2,r3" {
		t.Fatalf("results journaled as %s, want call order", got)
	}
	if got := eventOrder(f, agent.EventToolFinished); got != "r1,r2,r3" {
		t.Fatalf("tool.finished order = %s", got)
	}
	if got := eventOrder(f, agent.EventToolRequested); got != "r1,r2,r3" {
		t.Fatalf("tool.requested order = %s", got)
	}
	request := model.Requests()[1]
	for _, id := range []string{"r1", "r2", "r3"} {
		if got := toolContent(request, id); got != "body of "+id {
			t.Fatalf("result of %s = %q", id, got)
		}
	}
	if split := agenttest.SplitToolPair(request); split != "" {
		t.Fatal(split)
	}
}

func TestCallsSharingAKeyRunOneAtATimeInCallOrder(t *testing.T) {
	f := agenttest.NewFixture()
	gate := newGatedTool("e1", "r1")
	var e1Done, e2SawE1 atomic.Bool
	f.Tools.Funcs["edit"] = func(call tools.Call) tools.Result {
		if call.ToolCallID == "e2" {
			e2SawE1.Store(e1Done.Load())
			return tools.Result{Content: "edited"}
		}
		result := gate.run(call)
		e1Done.Store(true)
		return result
	}
	f.Tools.Funcs["read"] = gate.run
	f.Tools.Keys = map[string]string{"edit": "dir:/work"}
	model := agenttest.NewScriptedModel(calls(call("e1", "edit"), call("e2", "edit"), call("r1", "read")), text("Done."))
	outcome := start(f, model)

	started := map[string]bool{signal(t, gate.started): true, signal(t, gate.started): true}
	if !started["e1"] || !started["r1"] {
		t.Fatalf("started %v, want the first edit and the read at once", started)
	}
	close(gate.release["r1"])
	close(gate.release["e1"])

	if got := <-outcome; got.Status != agent.StatusCompleted {
		t.Fatalf("outcome = %+v", got)
	}
	if !e2SawE1.Load() {
		t.Fatal("the second edit ran before the first one finished")
	}
	if got := resultOrder(f); got != "e1,e2,r1" {
		t.Fatalf("results = %s", got)
	}
}

func TestRunsSharingLocksTakeTurnsOnAKey(t *testing.T) {
	locks := toolsched.NewLocks()
	gate := newGatedTool("a1")
	var aDone, bSawA atomic.Bool
	first, second := agenttest.NewFixture(), agenttest.NewFixture()
	for _, f := range []*agenttest.Fixture{first, second} {
		f.Locks = locks
		f.Tools.Keys = map[string]string{"bash": "dir:/work"}
	}
	first.Tools.Funcs["bash"] = func(call tools.Call) tools.Result {
		result := gate.run(call)
		aDone.Store(true)
		return result
	}
	second.Tools.Funcs["bash"] = func(tools.Call) tools.Result {
		bSawA.Store(aDone.Load())
		return tools.Result{Content: "ran"}
	}
	firstOutcome := start(first, agenttest.NewScriptedModel(calls(call("a1", "bash")), text("A done.")))
	signal(t, gate.started)
	secondOutcome := start(second, agenttest.NewScriptedModel(calls(call("b1", "bash")), text("B done.")))

	close(gate.release["a1"])

	for _, outcome := range []<-chan agent.Outcome{firstOutcome, secondOutcome} {
		if got := <-outcome; got.Status != agent.StatusCompleted {
			t.Fatalf("outcome = %+v", got)
		}
	}
	if !bSawA.Load() {
		t.Fatal("the second run's call ran while the first run held the key")
	}
}

func TestBarrierHoldsLaterCallsUntilItSettles(t *testing.T) {
	f := agenttest.NewFixture()
	gate := newGatedTool("m1")
	var m1Done, r2SawM1 atomic.Bool
	f.Tools.Funcs["migrate"] = func(call tools.Call) tools.Result {
		result := gate.run(call)
		m1Done.Store(true)
		return result
	}
	f.Tools.Funcs["read"] = func(tools.Call) tools.Result {
		r2SawM1.Store(m1Done.Load())
		return tools.Result{Content: "file body"}
	}
	f.Tools.Mutating = map[string]bool{"migrate": true}
	model := agenttest.NewScriptedModel(calls(call("m1", "migrate"), call("r2", "read")), text("Done."))
	outcome := start(f, model)

	signal(t, gate.started)
	close(gate.release["m1"])

	if got := <-outcome; got.Status != agent.StatusCompleted {
		t.Fatalf("outcome = %+v", got)
	}
	if !r2SawM1.Load() {
		t.Fatal("a call behind a barrier ran before the barrier settled")
	}
	for _, event := range f.Sink.Events {
		if event.Kind == agent.EventMessageCreated && event.Message.ID == "r2" {
			if !event.Message.Parts[0].ToolCall.Deferred {
				t.Fatal("the held call was not journaled deferred first")
			}
			break
		}
	}
	if got := phases(f.Journal.States); got != "model,tool_batch:m1+r2/r2,tool_batch:m1+r2,model" {
		t.Fatalf("checkpoints = %s", got)
	}
}

func TestParallelResultsExtendTheLoopStreakInCallOrder(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["status"] = statusTool
	model := agenttest.NewScriptedModel(calls(call("s1", "status"), call("s2", "status"), call("s3", "status")), text("Clean."))

	run(t, f, model)

	if note := lastMessage(model.Requests()[1]); !strings.Contains(note.Content, "You are repeating status") {
		t.Fatalf("second request ends with %+v, want the loop warning", note)
	}
}

func TestPanickingToolBecomesAnErrorResult(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["crash"] = func(tools.Call) tools.Result { panic("boom") }
	model := agenttest.NewScriptedModel(calls(call("c1", "crash")), text("Recovered."))

	outcome := run(t, f, model)

	result, ok := f.Journal.Result("c1")
	if outcome.Status != agent.StatusCompleted || !ok || !result.Parts[0].ToolResult.IsError() {
		t.Fatalf("outcome = %+v result = %+v", outcome, result)
	}
}

// stoppedBatch runs [r1 read, w2 barrier blocking until the run stops, r3 read]
// and stops the run once r1's result is journaled.
func stoppedBatch(t *testing.T, canceled bool) (*agenttest.Fixture, agent.Outcome) {
	t.Helper()
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	f.Tools.Funcs["wait"] = func(tools.Call) tools.Result { return tools.Result{Content: "waited"} }
	f.Tools.Mutating = map[string]bool{"wait": true}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	f.Tools.OnExecute = func(ctx context.Context, name string, _ tools.Call) error {
		if name == "wait" {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	f.Journal.OnAppend = func(message transcript.Message) error {
		if message.Role == transcript.MessageRoleTool && message.Parts[0].ToolResult.ToolCallID == "r1" {
			cancel(stopCause(canceled))
		}
		return nil
	}
	model := agenttest.NewScriptedModel(calls(call("r1", "read"), call("w2", "wait"), call("r3", "read")))

	outcome, err := f.Engine().Run(ctx, f.Task(model))
	if err != nil {
		t.Fatal(err)
	}
	return f, outcome
}

func TestCanceledBatchKeepsFinishedResultsAndCancelsTheRest(t *testing.T) {
	f, outcome := stoppedBatch(t, true)

	if outcome.Status != agent.StatusCanceled {
		t.Fatalf("outcome = %+v", outcome)
	}
	want := map[string]string{"r1": "file body", "w2": "Canceled by user.", "r3": "Canceled by user."}
	for id, content := range want {
		if result, ok := f.Journal.Result(id); !ok || result.Content != content {
			t.Fatalf("result of %s = %+v, want %q", id, result, content)
		}
	}
	if got := resultOrder(f); got != "r1,w2,r3" {
		t.Fatalf("results = %s", got)
	}
	if got := eventOrder(f, agent.EventToolFinished); got != "r1,w2" {
		t.Fatalf("tool.finished = %s, want no event for the call that never started", got)
	}
	if message, _ := f.Journal.Message("r3"); message.Parts[0].ToolCall.Deferred {
		t.Fatal("the canceled deferred call is still marked deferred")
	}
	if got := executedIDs(f); got != "r1" {
		t.Fatalf("executed = %s, want only r1 to have returned", got)
	}
}

func TestInterruptedBatchLeavesUnfinishedCallsToRecovery(t *testing.T) {
	f, outcome := stoppedBatch(t, false)

	if outcome.Status != agent.StatusInterrupted {
		t.Fatalf("outcome = %+v", outcome)
	}
	if got := resultOrder(f); got != "r1" {
		t.Fatalf("results = %s, want only the finished call", got)
	}
	if message, _ := f.Journal.Message("w2"); message.Parts[0].ToolCall.Deferred {
		t.Fatal("the call in flight is marked deferred")
	}
	if message, _ := f.Journal.Message("r3"); !message.Parts[0].ToolCall.Deferred {
		t.Fatal("the call that never started lost its deferred mark")
	}
}

func TestCanceledBatchAnswersAnAskedCallBeforeARunningOne(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["lookup"] = lookupTool
	f.Tools.Asks["lookup"] = true
	f.Tools.Funcs["wait"] = func(tools.Call) tools.Result { return tools.Result{Content: "waited"} }
	f.Tools.OnExecute = func(ctx context.Context, name string, _ tools.Call) error {
		if name == "wait" {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	f.Approvals.OnRequest = func(agent.Pending) error {
		cancel(agent.ErrCanceled)
		return nil
	}
	model := agenttest.NewScriptedModel(calls(call("l1", "lookup"), call("w2", "wait")))

	outcome, err := f.Engine().Run(ctx, f.Task(model))
	if err != nil || outcome.Status != agent.StatusCanceled {
		t.Fatalf("outcome = %+v err = %v", outcome, err)
	}
	for _, id := range []string{"l1", "w2"} {
		if result, ok := f.Journal.Result(id); !ok || result.Content != "Canceled by user." {
			t.Fatalf("result of %s = %+v, want it canceled", id, result)
		}
	}
}

func TestRejectedCallIsJournaledTogetherWithItsResult(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(calls(call("r1", "read"), call("g2", "ghost")), text("Done."))

	run(t, f, model)

	for i, message := range f.Journal.Messages {
		if message.ID != "g2" {
			continue
		}
		if i+1 == len(f.Journal.Messages) || f.Journal.Messages[i+1].Role != transcript.MessageRoleTool || f.Journal.Messages[i+1].Parts[0].ToolResult.ToolCallID != "g2" {
			t.Fatal("the rejected call's result does not follow its call")
		}
		return
	}
	t.Fatal("the rejected call was not journaled")
}

func TestFailedBatchKeepsFinishedResultsAndAnswersTheRest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := agenttest.NewFixture()
		f.Tools.Funcs["read"] = readTool
		f.Tools.Funcs["wait"] = func(tools.Call) tools.Result { return tools.Result{Content: "waited"} }
		f.Tools.Funcs["fail"] = func(tools.Call) tools.Result { return tools.Result{Content: "unreachable"} }
		fail := make(chan struct{})
		f.Tools.OnExecute = func(ctx context.Context, name string, _ tools.Call) error {
			switch name {
			case "wait":
				<-ctx.Done()
				return ctx.Err()
			case "fail":
				<-fail
				return errors.New("tool backend unavailable")
			}
			return nil
		}
		model := agenttest.NewScriptedModel(calls(call("w1", "wait"), call("r2", "read"), call("f3", "fail")))
		outcome := start(f, model)
		synctest.Wait()
		close(fail)

		if got := <-outcome; got.Status != agent.StatusFailed {
			t.Fatalf("outcome = %+v", got)
		}
		if got := resultOrder(f); got != "w1,r2,f3" {
			t.Fatalf("results = %s, want every call answered in call order", got)
		}
		if result, _ := f.Journal.Result("r2"); result.Content != "file body" {
			t.Fatalf("result of the finished read = %q", result.Content)
		}
		for _, id := range []string{"w1", "f3"} {
			if result, _ := f.Journal.Result(id); !strings.Contains(result.Content, "run failed") {
				t.Fatalf("result of %s = %q, want the failure", id, result.Content)
			}
		}
	})
}

// stopCause is how a test stops a run: canceled by the user or interrupted.
func stopCause(canceled bool) error {
	if canceled {
		return agent.ErrCanceled
	}
	return context.Canceled
}
