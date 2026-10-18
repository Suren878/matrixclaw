package core

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
)

type EventType string

const (
	EventRunUpdated      EventType = "run.updated"
	EventMessageCreated  EventType = "message.created"
	EventMessageUpdated  EventType = "message.updated"
	EventTodoUpdated     EventType = "todo.updated"
	EventToolUpdated     EventType = "tool.updated"
	EventApprovalRequest EventType = "approval.requested"
	EventApprovalResult  EventType = "approval.resolved"
	EventInputUpdated    EventType = "input.updated"
	EventTaskUpdated     EventType = "task.updated"
	EventContextUpdated  EventType = "context.updated"
)

// ContextUsage is how much of the model's window a session's conversation
// fills, as measured after a run's latest step.
type ContextUsage struct {
	SessionID     string `json:"session_id"`
	TokenEstimate int    `json:"token_estimate"`
	WindowTokens  int    `json:"window_tokens,omitempty"`
}

// Event is the daemon-owned envelope for live fan-out.
type Event struct {
	ID        uint64    `json:"id,omitempty"`
	Type      EventType `json:"type"`
	SessionID string    `json:"session_id"`
	RunID     string    `json:"run_id,omitempty"`
	Payload   any       `json:"payload,omitempty"`
	At        time.Time `json:"at"`
}

// eventBus fans events out to subscribers in publish order. A subscriber
// that falls a full buffer behind is closed rather than skipped, so its client
// reconnects and reloads instead of missing events silently.
type eventBus struct {
	mu          sync.Mutex
	nextSub     int
	lastEventID uint64
	subscribers map[int]eventSubscription
	// history is a ring: the event with ID n is at n % eventHistoryLimit.
	history [eventHistoryLimit]Event
}

type eventSubscription struct {
	sessionID string
	ch        chan Event
}

func (s eventSubscription) wants(event Event) bool {
	return s.sessionID == "" || s.sessionID == event.SessionID
}

const eventHistoryLimit = 256
const eventChannelBuffer = 512

func newEventBus() *eventBus {
	return &eventBus{subscribers: map[int]eventSubscription{}}
}

func (b *eventBus) Subscribe(ctx context.Context, sessionID string) <-chan Event {
	return b.SubscribeAfter(ctx, sessionID, 0)
}

// SubscribeAfter streams the session's events (every session's for an empty
// sessionID), first those after afterID still in the history. The channel
// closes when ctx ends or the subscriber falls behind.
func (b *eventBus) SubscribeAfter(ctx context.Context, sessionID string, afterID uint64) <-chan Event {
	sub := eventSubscription{sessionID: strings.TrimSpace(sessionID), ch: make(chan Event, eventChannelBuffer)}

	b.mu.Lock()
	id := b.nextSub
	b.nextSub++
	if afterID > 0 {
		oldest := uint64(1)
		if b.lastEventID > eventHistoryLimit {
			oldest = b.lastEventID - eventHistoryLimit + 1
		}
		for n := max(afterID+1, oldest); n <= b.lastEventID; n++ {
			if event := b.history[n%eventHistoryLimit]; sub.wants(event) {
				sub.ch <- event
			}
		}
	}
	b.subscribers[id] = sub
	b.mu.Unlock()

	safego.Go("core.eventBus.cleanup", func() {
		<-ctx.Done()
		b.mu.Lock()
		b.drop(id)
		b.mu.Unlock()
	})
	return sub.ch
}

// Publish numbers the event and hands it to every subscriber under one lock,
// so each sees events in ID order.
func (b *eventBus) Publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastEventID++
	event.ID = b.lastEventID
	b.history[event.ID%eventHistoryLimit] = event
	for id, sub := range b.subscribers {
		if !sub.wants(event) {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			log.Printf("core: event subscriber of session %q fell %d events behind; closing it", sub.sessionID, eventChannelBuffer)
			b.drop(id)
		}
	}
}

// drop closes a subscription once; the caller holds b.mu.
func (b *eventBus) drop(id int) {
	if sub, ok := b.subscribers[id]; ok {
		delete(b.subscribers, id)
		close(sub.ch)
	}
}

// LastID is the ID of the newest published event, 0 before the first.
func (b *eventBus) LastID() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastEventID
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
