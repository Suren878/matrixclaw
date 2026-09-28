package agent_test

import (
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestTimeSpentOnlyInDelegatedCallsIsNotActiveTime(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Delegated = map[string]bool{"agent": true}
	f.Tools.Funcs["agent"] = func(tools.Call) tools.Result {
		f.Clock = f.Clock.Add(10 * time.Minute)
		return tools.Result{Content: "child result"}
	}
	f.Tools.Funcs["bash"] = func(tools.Call) tools.Result {
		f.Clock = f.Clock.Add(time.Minute)
		return tools.Result{Content: "ok"}
	}
	model := agenttest.NewScriptedModel(calls(call("a1", "agent")), calls(call("b1", "bash")), text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Counters.Active != time.Minute {
		t.Fatalf("outcome = %+v, active %v", outcome, outcome.Counters.Active)
	}
}

func TestTimeADelegatedCallRunsAlongsideTheRunsOwnCallsIsActiveTime(t *testing.T) {
	f := agenttest.NewFixture()
	var mu sync.Mutex
	now := f.Clock
	advance := func(d time.Duration) {
		mu.Lock()
		now = now.Add(d)
		mu.Unlock()
	}
	f.Now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	bashJournaled := make(chan struct{})
	f.Tools.OnFinish = func(name string, _ tools.Call) error {
		if name == "bash" {
			close(bashJournaled)
		}
		return nil
	}
	f.Tools.Delegated = map[string]bool{"agent": true}
	f.Tools.Funcs["bash"] = func(tools.Call) tools.Result {
		advance(time.Minute)
		return tools.Result{Content: "ok"}
	}
	f.Tools.Funcs["agent"] = func(tools.Call) tools.Result {
		<-bashJournaled
		advance(10 * time.Minute)
		return tools.Result{Content: "child result"}
	}
	model := agenttest.NewScriptedModel(calls(call("b1", "bash"), call("a1", "agent")), text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Counters.Active != time.Minute {
		t.Fatalf("outcome = %+v, active %v", outcome, outcome.Counters.Active)
	}
}
