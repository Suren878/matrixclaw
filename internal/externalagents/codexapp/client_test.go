package codexapp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
)

func TestClientSubscribeTurnRoutesInterleavedEvents(t *testing.T) {
	client := newRoutingClientForTest(t)

	turn1, unsubscribe1 := client.SubscribeTurn(context.Background(), "thread-1", "turn-1")
	defer unsubscribe1()
	turn2, unsubscribe2 := client.SubscribeTurn(context.Background(), "thread-1", "turn-2")
	defer unsubscribe2()
	waitForSubscriptions(t, client, turnKey{threadID: "thread-1", turnID: "turn-1"}, turnKey{threadID: "thread-1", turnID: "turn-2"})

	client.events <- turnDeltaNotification(turnKey{threadID: "thread-1", turnID: "turn-2"}, "two")
	client.events <- turnDeltaNotification(turnKey{threadID: "thread-1", turnID: "turn-1"}, "one")

	got2 := receiveNotification(t, turn2)
	if params, ok := got2.Params.(AgentMessageDelta); !ok || params.TurnID != "turn-2" || params.Delta != "two" {
		t.Fatalf("turn2 notification = %#v, want turn-2 delta", got2)
	}
	got1 := receiveNotification(t, turn1)
	if params, ok := got1.Params.(AgentMessageDelta); !ok || params.TurnID != "turn-1" || params.Delta != "one" {
		t.Fatalf("turn1 notification = %#v, want turn-1 delta", got1)
	}
	assertNoNotification(t, turn1)
	assertNoNotification(t, turn2)
}

func TestClientSubscribeTurnReplaysBacklogBeyondSubscriptionBuffer(t *testing.T) {
	client := newRoutingClientForTest(t)
	key := turnKey{threadID: "thread-1", turnID: "turn-1"}
	backlogCount := turnSubscriptionBuffer + 1

	for i := range backlogCount {
		client.events <- turnDeltaNotification(key, fmt.Sprintf("early-%03d", i))
	}
	client.events <- Notification{
		Method: "turn/completed",
		Params: TurnCompleted{
			ThreadID: key.threadID,
			Turn:     Turn{ID: key.turnID, Status: TurnStatusCompleted},
		},
	}
	waitForBacklogAtMost(t, client, key, 2)

	turn, unsubscribe := client.SubscribeTurn(context.Background(), key.threadID, key.turnID)
	defer unsubscribe()

	var gotText strings.Builder
	for {
		got := receiveNotification(t, turn)
		switch params := got.Params.(type) {
		case AgentMessageDelta:
			gotText.WriteString(params.Delta)
		case TurnCompleted:
			var want strings.Builder
			for i := range backlogCount {
				_, _ = fmt.Fprintf(&want, "early-%03d", i)
			}
			if gotText.String() != want.String() {
				t.Fatalf("combined backlog delta = %q, want %q", gotText.String(), want.String())
			}
			assertNoNotification(t, turn)
			return
		}
	}
}

func TestClientSlowTurnSubscriberKeepsAllTextAndTerminalEvent(t *testing.T) {
	client := newRoutingClientForTest(t)
	key := turnKey{threadID: "thread-1", turnID: "turn-1"}
	turn, unsubscribe := client.SubscribeTurn(context.Background(), key.threadID, key.turnID)
	defer unsubscribe()
	waitForSubscriptions(t, client, key)

	const deltaCount = 2048
	for i := 0; i < deltaCount; i++ {
		client.events <- turnDeltaNotification(key, "x")
	}
	client.events <- Notification{
		Method: "turn/completed",
		Params: TurnCompleted{
			ThreadID: key.threadID,
			Turn:     Turn{ID: key.turnID, Status: TurnStatusCompleted},
		},
	}

	var text strings.Builder
	terminal := false
	deadline := time.After(2 * time.Second)
	for !terminal {
		select {
		case notification, ok := <-turn:
			if !ok {
				t.Fatalf("subscription closed before terminal event; text bytes = %d", text.Len())
			}
			switch params := notification.Params.(type) {
			case AgentMessageDelta:
				text.WriteString(params.Delta)
			case TurnCompleted:
				terminal = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for terminal event; text bytes = %d", text.Len())
		}
	}
	if got := text.Len(); got != deltaCount {
		t.Fatalf("combined delta bytes = %d, want %d", got, deltaCount)
	}
	if err := client.Err(); err != nil {
		t.Fatalf("client error after burst = %v", err)
	}
}

func TestClientBacklogCoalescesBurstWithoutLosingTerminalEvent(t *testing.T) {
	client := newRoutingClientForTest(t)
	key := turnKey{threadID: "thread-1", turnID: "turn-1"}

	const deltaCount = 2048
	for i := 0; i < deltaCount; i++ {
		client.events <- turnDeltaNotification(key, "x")
	}
	client.events <- Notification{
		Method: "turn/completed",
		Params: TurnCompleted{
			ThreadID: key.threadID,
			Turn:     Turn{ID: key.turnID, Status: TurnStatusCompleted},
		},
	}
	waitForBacklogAtMost(t, client, key, turnBacklogLimit+1)

	turn, unsubscribe := client.SubscribeTurn(context.Background(), key.threadID, key.turnID)
	defer unsubscribe()
	var text strings.Builder
	for {
		notification := receiveNotification(t, turn)
		switch params := notification.Params.(type) {
		case AgentMessageDelta:
			text.WriteString(params.Delta)
		case TurnCompleted:
			if got := text.Len(); got != deltaCount {
				t.Fatalf("combined backlog delta bytes = %d, want %d", got, deltaCount)
			}
			return
		}
	}
}

func newRoutingClientForTest(t *testing.T) *Client {
	t.Helper()
	client := &Client{
		events:      make(chan Notification, turnBacklogLimit+16),
		done:        make(chan struct{}),
		turnSubs:    map[turnKey]map[*turnSubscription]struct{}{},
		turnBacklog: map[turnKey][]Notification{},
		routeDone:   make(chan struct{}),
	}
	safego.Go("codexapp.testEventRouter", client.routeEvents)
	t.Cleanup(func() {
		close(client.events)
		select {
		case <-client.routeDone:
		case <-time.After(time.Second):
			t.Fatalf("event router did not stop")
		}
	})
	return client
}

func turnDeltaNotification(key turnKey, delta string) Notification {
	return Notification{
		Method: "item/agentMessage/delta",
		Params: AgentMessageDelta{
			ThreadID: key.threadID,
			TurnID:   key.turnID,
			ItemID:   "item-1",
			Delta:    delta,
		},
	}
}

func receiveNotification(t *testing.T, ch <-chan Notification) Notification {
	t.Helper()
	select {
	case notification, ok := <-ch:
		if !ok {
			t.Fatalf("subscription closed before notification")
		}
		return notification
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for notification")
		return Notification{}
	}
}

func assertNoNotification(t *testing.T, ch <-chan Notification) {
	t.Helper()
	select {
	case notification, ok := <-ch:
		t.Fatalf("unexpected notification %#v, ok=%v", notification, ok)
	case <-time.After(25 * time.Millisecond):
	}
}

func waitForSubscriptions(t *testing.T, client *Client, keys ...turnKey) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if hasSubscriptions(client, keys...) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("subscriptions were not registered")
		case <-ticker.C:
		}
	}
}

func hasSubscriptions(client *Client, keys ...turnKey) bool {
	client.routeMu.Lock()
	defer client.routeMu.Unlock()
	for _, key := range keys {
		if len(client.turnSubs[key]) == 0 {
			return false
		}
	}
	return true
}

func waitForBacklogAtMost(t *testing.T, client *Client, key turnKey, max int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		client.routeMu.Lock()
		backlog := client.turnBacklog[key]
		got := len(backlog)
		terminal := got > 0 && backlog[got-1].Method == "turn/completed"
		client.routeMu.Unlock()
		if terminal {
			if got > max {
				t.Fatalf("backlog size = %d, want at most %d", got, max)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("backlog did not receive terminal event; size = %d", got)
		case <-ticker.C:
		}
	}
}
