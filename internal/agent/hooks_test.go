package agent_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// fakeHooks answers every Stop with stop and every SessionStart with context.
type fakeHooks struct {
	stop    string
	context string
	actives []bool
	replies []string
	sources []string
}

func (h *fakeHooks) SessionStart(_ context.Context, source string) string {
	h.sources = append(h.sources, source)
	return h.context
}

func (h *fakeHooks) Stop(_ context.Context, reply string, active bool) string {
	h.actives, h.replies = append(h.actives, active), append(h.replies, reply)
	return h.stop
}

func (h *fakeHooks) PreCompact(context.Context, string) {}

func TestStopHookKeepsTheRunGoingAFewTimesAtMost(t *testing.T) {
	f := agenttest.NewFixture()
	hooks := &fakeHooks{stop: "go test ./... fails: TestParse"}
	f.Hooks = hooks
	turns := make([]agenttest.Turn, 8)
	for i := range turns {
		turns[i] = text("Done.")
	}
	model := agenttest.NewScriptedModel(turns...)

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopDone || outcome.Assistant.Content != "Done." {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(model.Requests()) != 6 || !reflect.DeepEqual(hooks.actives, []bool{false, true, true, true, true}) || hooks.replies[0] != "Done." {
		t.Fatalf("requests = %d, stop hooks asked with active = %v", len(model.Requests()), hooks.actives)
	}
	if note := lastMessage(model.Requests()[1]); note.Role != "user" || !strings.Contains(note.Content, "go test ./... fails: TestParse") {
		t.Fatalf("note = %+v", note)
	}
	if outcome.RunState.StopHooks != 5 {
		t.Fatalf("state = %+v", outcome.RunState)
	}
}

func TestStopHookThatLetsGoCompletesTheRun(t *testing.T) {
	f := agenttest.NewFixture()
	hooks := &fakeHooks{}
	f.Hooks = hooks
	model := agenttest.NewScriptedModel(text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 1 || len(hooks.actives) != 1 {
		t.Fatalf("outcome = %+v, requests = %d, stop hooks asked %d times", outcome, len(model.Requests()), len(hooks.actives))
	}
}

func TestSessionStartContextReachesTheModel(t *testing.T) {
	for name, tc := range map[string]struct {
		history bool
		source  string
	}{
		"first run": {source: "startup"},
		"later run": {history: true, source: "resume"},
	} {
		t.Run(name, func(t *testing.T) {
			f := agenttest.NewFixture()
			if tc.history {
				f.WithHistory(transcript.Message{ID: "msg_old", SessionID: agenttest.SessionID, RunID: "run_old", Role: transcript.MessageRoleUser, Content: "earlier"})
			}
			hooks := &fakeHooks{context: "branch: main"}
			f.Hooks = hooks
			model := agenttest.NewScriptedModel(text("Done."))

			run(t, f, model)

			if !reflect.DeepEqual(hooks.sources, []string{tc.source}) {
				t.Fatalf("sources = %v", hooks.sources)
			}
			if note := lastMessage(model.Requests()[0]); !strings.Contains(note.Content, "branch: main") {
				t.Fatalf("first request ends with %+v", note)
			}
		})
	}
}

func TestSessionStartRunsOnlyWhenARunBegins(t *testing.T) {
	f := agenttest.NewFixture()
	hooks := &fakeHooks{context: "branch: main"}
	f.Hooks = hooks
	task := f.Task(agenttest.NewScriptedModel(text("Done.")))
	task.Resume = agent.RunState{Steps: 2}

	runTask(t, f, task)

	if len(hooks.sources) != 0 {
		t.Fatalf("a resumed run ran SessionStart hooks: %v", hooks.sources)
	}
}
