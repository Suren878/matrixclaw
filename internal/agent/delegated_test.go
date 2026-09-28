package agent_test

import (
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
