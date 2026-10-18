package core

import (
	"context"
	"testing"
)

func TestEventBusClosesASubscriberThatFallsBehind(t *testing.T) {
	bus := newEventBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slow := bus.Subscribe(ctx, "s")
	other := bus.Subscribe(ctx, "other")

	for range eventChannelBuffer + 1 {
		bus.Publish(Event{Type: EventRunUpdated, SessionID: "s"})
	}

	received := 0
	for range slow {
		received++
	}
	if received != eventChannelBuffer {
		t.Fatalf("received %d events before the close, want %d", received, eventChannelBuffer)
	}
	bus.Publish(Event{Type: EventRunUpdated, SessionID: "other"})
	if event, ok := <-other; !ok || event.ID != eventChannelBuffer+2 {
		t.Fatalf("other subscriber got %+v (open %v)", event, ok)
	}
}

func TestEventBusReplaysTheHistoryInOrder(t *testing.T) {
	bus := newEventBus()
	for range eventHistoryLimit + 10 {
		bus.Publish(Event{Type: EventRunUpdated, SessionID: "s"})
	}
	ctx, cancel := context.WithCancel(context.Background())
	events := bus.SubscribeAfter(ctx, "s", 1)

	want := uint64(11) // the oldest event the ring still holds
	for want <= bus.LastID() {
		if event := <-events; event.ID != want {
			t.Fatalf("replayed event %d, want %d", event.ID, want)
		}
		want++
	}
	cancel()
	for range events {
		t.Fatal("event after the replay")
	}
}
