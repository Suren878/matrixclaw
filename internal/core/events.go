package core

import (
	"context"
	"sync"
	"time"
)

type EventType string

const (
	EventRunUpdated      EventType = "run.updated"
	EventMessageCreated  EventType = "message.created"
	EventMessageUpdated  EventType = "message.updated"
	EventPlanUpdated     EventType = "plan.updated"
	EventToolUpdated     EventType = "tool.updated"
	EventApprovalRequest EventType = "approval.requested"
	EventApprovalResult  EventType = "approval.resolved"
	EventFileVersioned   EventType = "file.versioned"
)

// Event is the daemon-owned envelope for live fan-out.
type Event struct {
	ID        uint64    `json:"id,omitempty"`
	Type      EventType `json:"type"`
	SessionID string    `json:"session_id"`
	RunID     string    `json:"run_id,omitempty"`
	Payload   any       `json:"payload,omitempty"`
	At        time.Time `json:"at"`
}

type eventBus struct {
	mu          sync.RWMutex
	nextID      int
	nextEventID uint64
	subscribers map[int]eventSubscription
	history     []Event
}

type eventSubscription struct {
	sessionID string
	ch        chan Event
	done      <-chan struct{}
}

const eventHistoryLimit = 256
const eventChannelBuffer = 512

func newEventBus() *eventBus {
	return &eventBus{
		subscribers: map[int]eventSubscription{},
	}
}

func (b *eventBus) Subscribe(ctx context.Context, sessionID string) <-chan Event {
	return b.SubscribeAfter(ctx, sessionID, 0)
}

func (b *eventBus) SubscribeAfter(ctx context.Context, sessionID string, afterID uint64) <-chan Event {
	out := make(chan Event, eventChannelBuffer)
	normalizedSessionID := normalizeText(sessionID)

	b.mu.Lock()
	id := b.nextID
	b.nextID++
	var replay []Event
	if afterID > 0 {
		for _, event := range b.history {
			if event.ID <= afterID {
				continue
			}
			if normalizedSessionID != "" && normalizedSessionID != normalizeText(event.SessionID) {
				continue
			}
			replay = append(replay, event)
		}
	}
	b.subscribers[id] = eventSubscription{
		sessionID: normalizedSessionID,
		ch:        out,
		done:      ctx.Done(),
	}
	for _, event := range replay {
		out <- event
	}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.subscribers, id)
		b.mu.Unlock()
	}()

	return out
}

func (b *eventBus) Publish(event Event) {
	b.mu.Lock()
	b.nextEventID++
	event.ID = b.nextEventID
	b.history = append(b.history, event)
	if len(b.history) > eventHistoryLimit {
		b.history = append([]Event(nil), b.history[len(b.history)-eventHistoryLimit:]...)
	}
	subs := make([]eventSubscription, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		if sub.sessionID != "" && sub.sessionID != normalizeText(event.SessionID) {
			continue
		}
		subs = append(subs, sub)
	}
	b.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub.ch <- event:
		case <-sub.done:
		default:
		}
	}
}

func (c *Core) SubscribeEvents(ctx context.Context, sessionID string) <-chan Event {
	return c.events.Subscribe(ctx, sessionID)
}

func (c *Core) SubscribeEventsAfter(ctx context.Context, sessionID string, afterID uint64) <-chan Event {
	return c.events.SubscribeAfter(ctx, sessionID, afterID)
}

func (c *Core) publishEvent(event Event) {
	if c.events == nil {
		return
	}
	if event.At.IsZero() {
		event.At = c.now().UTC()
	}
	c.events.Publish(event)
}
