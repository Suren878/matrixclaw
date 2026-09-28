package agent_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
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
	if outcome.Status != agent.StatusCompleted || !ok || !result.Parts[0].ToolResult.IsError {
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Tools.OnExecute = func(ctx context.Context, name string, _ tools.Call) error {
		if name == "wait" {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	f.Journal.OnAppend = func(message transcript.Message) error {
		if message.Role == transcript.MessageRoleTool && message.Parts[0].ToolResult.ToolCallID == "r1" {
			f.Inbox.Cancel = canceled
			cancel()
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
