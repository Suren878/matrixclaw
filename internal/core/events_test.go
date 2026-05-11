package core

import (
	"context"
	"testing"
	"time"
)

func TestEventBusSubscribeAfterReplaysMissedSessionEvents(t *testing.T) {
	t.Parallel()

	bus := newEventBus()
	bus.Publish(Event{Type: EventMessageCreated, SessionID: "session-1"})
	bus.Publish(Event{Type: EventRunUpdated, SessionID: "session-1"})
	bus.Publish(Event{Type: EventToolUpdated, SessionID: "session-2"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := bus.SubscribeAfter(ctx, "session-1", 1)

	select {
	case event := <-events:
		if event.ID != 2 {
			t.Fatalf("event.ID = %d, want 2", event.ID)
		}
		if event.Type != EventRunUpdated {
			t.Fatalf("event.Type = %q, want %q", event.Type, EventRunUpdated)
		}
		if event.SessionID != "session-1" {
			t.Fatalf("event.SessionID = %q, want %q", event.SessionID, "session-1")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for replayed event")
	}

	select {
	case unexpected := <-events:
		t.Fatalf("unexpected extra replayed event: %#v", unexpected)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEventBusPublishDoesNotBlockOnLaggingSubscriber(t *testing.T) {
	t.Parallel()

	bus := newEventBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = bus.Subscribe(ctx, "session-1")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < eventChannelBuffer+8; i++ {
			bus.Publish(Event{Type: EventMessageUpdated, SessionID: "session-1"})
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a lagging subscriber")
	}
}
