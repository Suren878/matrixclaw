package codexapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/safego"
)

func TestTurnStartParamsIncludesSessionModel(t *testing.T) {
	params := turnStartParams(externalagents.ExternalSession{
		ExternalThreadID: "thread-1",
		Model:            " gpt-5.4-mini ",
		ApprovalPolicy:   "on-request",
	}, "hello")

	if params.ThreadID != "thread-1" {
		t.Fatalf("thread id = %q, want thread-1", params.ThreadID)
	}
	if params.Model != "gpt-5.4-mini" {
		t.Fatalf("model = %q, want gpt-5.4-mini", params.Model)
	}
	if params.ApprovalPolicy != "on-request" {
		t.Fatalf("approval policy = %q, want on-request", params.ApprovalPolicy)
	}
	if len(params.Input) != 1 || params.Input[0].Text != "hello" {
		t.Fatalf("input = %#v, want hello text input", params.Input)
	}
}

func TestForwardTurnEventsNormalizesEventsAndCloses(t *testing.T) {
	client := newRoutingClientForTest(t)
	client.events <- Notification{
		Method: "item/agentMessage/delta",
		Params: AgentMessageDelta{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "item-1",
			Delta:    "hello",
		},
	}
	client.events <- Notification{
		Method: "turn/completed",
		Params: TurnCompleted{
			ThreadID: "thread-1",
			Turn:     Turn{ID: "turn-1"},
		},
	}
	waitForBacklog(t, client, turnKey{threadID: "thread-1", turnID: "turn-1"}, 2)

	runtime := NewRuntime(RuntimeOptions{Client: client})
	out := make(chan externalagents.Event, 64)
	runtime.forwardTurnEvents(context.Background(), out, "thread-1", "turn-1")

	events := collectEvents(out)
	if len(events) != 2 {
		t.Fatalf("events = %#v, want message delta and completion", events)
	}
	if events[0].Kind != externalagents.EventMessageDelta || events[0].Text != "hello" {
		t.Fatalf("first event = %#v, want message delta", events[0])
	}
	if events[1].Kind != externalagents.EventTurnCompleted {
		t.Fatalf("last event = %#v, want turn completed", events[1])
	}
}

func TestForwardTurnEventsCompletionClosesOnlyMatchingTurn(t *testing.T) {
	client := newRoutingClientForTest(t)
	runtime := NewRuntime(RuntimeOptions{Client: client})
	out1 := make(chan externalagents.Event, 64)
	out2 := make(chan externalagents.Event, 64)

	safego.Go("codexapp.testForwardTurn1", func() {
		runtime.forwardTurnEvents(context.Background(), out1, "thread-1", "turn-1")
	})
	safego.Go("codexapp.testForwardTurn2", func() {
		runtime.forwardTurnEvents(context.Background(), out2, "thread-1", "turn-2")
	})
	waitForSubscriptions(t, client, turnKey{threadID: "thread-1", turnID: "turn-1"}, turnKey{threadID: "thread-1", turnID: "turn-2"})

	client.events <- Notification{
		Method: "turn/completed",
		Params: TurnCompleted{ThreadID: "thread-1", Turn: Turn{ID: "turn-1"}},
	}

	events1 := collectEventsUntilClosed(t, out1)
	if len(events1) != 1 || events1[0].Kind != externalagents.EventTurnCompleted || events1[0].ExternalTurnID != "turn-1" {
		t.Fatalf("turn1 events = %#v, want turn-1 completion", events1)
	}
	assertEventStreamOpen(t, out2)

	client.events <- Notification{
		Method: "turn/completed",
		Params: TurnCompleted{ThreadID: "thread-1", Turn: Turn{ID: "turn-2"}},
	}
	events2 := collectEventsUntilClosed(t, out2)
	if len(events2) != 1 || events2[0].Kind != externalagents.EventTurnCompleted || events2[0].ExternalTurnID != "turn-2" {
		t.Fatalf("turn2 events = %#v, want turn-2 completion", events2)
	}
}

func TestForwardTurnEventsEmitsFailureOnMalformedClientMessage(t *testing.T) {
	conn := newControllableConn()
	client := NewClient(conn)
	t.Cleanup(func() {
		_ = client.Close()
	})
	runtime := NewRuntime(RuntimeOptions{Client: client})
	out := make(chan externalagents.Event, 64)

	safego.Go("codexapp.testForwardMalformed", func() {
		runtime.forwardTurnEvents(context.Background(), out, "thread-1", "turn-1")
	})
	waitForSubscriptions(t, client, turnKey{threadID: "thread-1", turnID: "turn-1"})

	conn.sendLine("{not json\n")

	events := collectEventsUntilClosed(t, out)
	if len(events) != 1 {
		t.Fatalf("events = %#v, want one turn failure", events)
	}
	if events[0].Kind != externalagents.EventTurnFailed {
		t.Fatalf("event kind = %q, want %q", events[0].Kind, externalagents.EventTurnFailed)
	}
	if !strings.Contains(events[0].Error, "decode codex app-server message") {
		t.Fatalf("event error = %q, want decode failure", events[0].Error)
	}
}

func TestForwardTurnEventsPanicEmitsTurnFailedAndCloses(t *testing.T) {
	runtime := NewRuntime(RuntimeOptions{})
	out := make(chan externalagents.Event, 64)

	runtime.forwardTurnEvents(context.Background(), out, "thread-1", "turn-1")

	events := collectEvents(out)
	if len(events) != 1 {
		t.Fatalf("events = %#v, want one turn failure", events)
	}
	event := events[0]
	if event.Kind != externalagents.EventTurnFailed {
		t.Fatalf("event kind = %q, want %q", event.Kind, externalagents.EventTurnFailed)
	}
	if event.ExternalThreadID != "thread-1" || event.ExternalTurnID != "turn-1" {
		t.Fatalf("event ids = (%q, %q), want thread-1/turn-1", event.ExternalThreadID, event.ExternalTurnID)
	}
	if !strings.Contains(event.Error, "codex app-server event worker panicked") {
		t.Fatalf("event error = %q, want panic failure", event.Error)
	}
}

func collectEvents(events <-chan externalagents.Event) []externalagents.Event {
	var out []externalagents.Event
	for event := range events {
		out = append(out, event)
	}
	return out
}

func collectEventsUntilClosed(t *testing.T, events <-chan externalagents.Event) []externalagents.Event {
	t.Helper()
	var out []externalagents.Event
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return out
			}
			out = append(out, event)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for event stream to close")
			return out
		}
	}
}

func assertEventStreamOpen(t *testing.T, events <-chan externalagents.Event) {
	t.Helper()
	select {
	case event, ok := <-events:
		t.Fatalf("event stream delivered %#v, ok=%v; want still open with no event", event, ok)
	case <-time.After(25 * time.Millisecond):
	}
}
