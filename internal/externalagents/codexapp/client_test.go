package codexapp

import (
	"context"
	"fmt"
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
	waitForBacklog(t, client, key, backlogCount)

	turn, unsubscribe := client.SubscribeTurn(context.Background(), key.threadID, key.turnID)
	defer unsubscribe()

	for i := range backlogCount {
		got := receiveNotification(t, turn)
		params, ok := got.Params.(AgentMessageDelta)
		if !ok {
			t.Fatalf("notification %d params = %#v, want AgentMessageDelta", i, got.Params)
		}
		if want := fmt.Sprintf("early-%03d", i); params.Delta != want {
			t.Fatalf("notification %d delta = %q, want %q", i, params.Delta, want)
		}
	}
	assertNoNotification(t, turn)
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

func waitForBacklog(t *testing.T, client *Client, key turnKey, want int) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		client.routeMu.Lock()
		got := len(client.turnBacklog[key])
		client.routeMu.Unlock()
		if got >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("backlog size = %d, want %d", got, want)
		case <-ticker.C:
		}
	}
}
