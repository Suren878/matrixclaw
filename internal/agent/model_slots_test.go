package agent_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestModelSlotsBoundGenerationsAcrossRuns(t *testing.T) {
	slots := toolsched.NewSemaphore(1)
	var inFlight, most atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	model := agenttest.ModelFunc(func(context.Context, providers.Request) (providers.Response, error) {
		n := inFlight.Add(1)
		for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
		}
		entered <- struct{}{}
		<-release
		inFlight.Add(-1)
		return providers.Response{Text: "Done."}, nil
	})
	outcomes := make(chan agent.Outcome, 2)
	for range 2 {
		f := agenttest.NewFixture()
		f.ModelSlots = slots
		engine, task := f.Engine(), f.Task(model)
		go func() {
			outcome, _ := engine.Run(context.Background(), task)
			outcomes <- outcome
		}()
	}

	for range 2 {
		<-entered
		release <- struct{}{}
	}

	for range 2 {
		if outcome := <-outcomes; outcome.Status != agent.StatusCompleted {
			t.Fatalf("outcome = %+v", outcome)
		}
	}
	if most.Load() != 1 {
		t.Fatalf("%d generations ran at once, want 1", most.Load())
	}
}

func TestPanickingGenerationGivesItsModelSlotBack(t *testing.T) {
	slots := toolsched.NewSemaphore(1)
	crash := agenttest.NewFixture()
	crash.ModelSlots = slots
	func() {
		defer func() { _ = recover() }()
		_, _ = crash.Engine().Run(context.Background(), crash.Task(agenttest.ModelFunc(func(context.Context, providers.Request) (providers.Response, error) {
			panic("provider bug")
		})))
	}()

	for range 4 {
		f := agenttest.NewFixture()
		f.ModelSlots = slots
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		outcome, err := f.Engine().Run(ctx, f.Task(agenttest.NewScriptedModel(text("Done."))))
		cancel()
		if err != nil || outcome.Status != agent.StatusCompleted {
			t.Fatalf("outcome = %+v err = %v, want the slot free again", outcome, err)
		}
	}
}
