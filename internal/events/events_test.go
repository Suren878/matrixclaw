package events

import (
	"context"
	"testing"
)

func TestBusClosesASubscriberThatFallsBehind(t *testing.T) {
	bus := NewBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slow := bus.Subscribe(ctx, "s", 0)
	other := bus.Subscribe(ctx, "other", 0)

	for range channelBuffer + 1 {
		bus.Publish(Event{Type: "run.updated", SessionID: "s"})
	}

	received := 0
	for range slow {
		received++
	}
	if received != channelBuffer {
		t.Fatalf("received %d events before the close, want %d", received, channelBuffer)
	}
	bus.Publish(Event{Type: "run.updated", SessionID: "other"})
	if event, ok := <-other; !ok || event.ID != channelBuffer+2 {
		t.Fatalf("other subscriber got %+v (open %v)", event, ok)
	}
}

func TestBusReplaysTheHistoryInOrder(t *testing.T) {
	bus := NewBus()
	for range historyLimit + 10 {
		bus.Publish(Event{Type: "run.updated", SessionID: "s"})
	}
	ctx, cancel := context.WithCancel(context.Background())
	events := bus.Subscribe(ctx, "s", 1)

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
