// Package events is the daemon's live event stream: numbered events fanned
// out to subscribers in publish order, with a short history to resume from.
package events

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
)

// Type names what an event reports, such as "run.updated".
type Type string

// Event is the daemon-owned envelope for live fan-out.
type Event struct {
	ID        uint64    `json:"id,omitempty"`
	Type      Type      `json:"type"`
	SessionID string    `json:"session_id"`
	RunID     string    `json:"run_id,omitempty"`
	Payload   any       `json:"payload,omitempty"`
	At        time.Time `json:"at"`
}

const (
	historyLimit  = 256
	channelBuffer = 512
)

// Bus fans events out to subscribers in publish order. A subscriber that
// falls a full buffer behind is closed rather than skipped, so its client
// reconnects and reloads instead of missing events silently.
type Bus struct {
	mu          sync.Mutex
	nextSub     int
	lastEventID uint64
	subscribers map[int]subscription
	// history is a ring: the event with ID n is at n % historyLimit.
	history [historyLimit]Event
}

type subscription struct {
	sessionID string
	ch        chan Event
}

func (s subscription) wants(event Event) bool {
	return s.sessionID == "" || s.sessionID == event.SessionID
}

func NewBus() *Bus {
	return &Bus{subscribers: map[int]subscription{}}
}

// Subscribe streams the session's events (every session's for an empty
// sessionID), first those after afterID still in the history; afterID 0
// replays none. The channel closes when ctx ends or the subscriber falls
// behind.
func (b *Bus) Subscribe(ctx context.Context, sessionID string, afterID uint64) <-chan Event {
	sub := subscription{sessionID: strings.TrimSpace(sessionID), ch: make(chan Event, channelBuffer)}

	b.mu.Lock()
	id := b.nextSub
	b.nextSub++
	if afterID > 0 {
		oldest := uint64(1)
		if b.lastEventID > historyLimit {
			oldest = b.lastEventID - historyLimit + 1
		}
		for n := max(afterID+1, oldest); n <= b.lastEventID; n++ {
			if event := b.history[n%historyLimit]; sub.wants(event) {
				sub.ch <- event
			}
		}
	}
	b.subscribers[id] = sub
	b.mu.Unlock()

	safego.Go("events.Bus.cleanup", func() {
		<-ctx.Done()
		b.mu.Lock()
		b.drop(id)
		b.mu.Unlock()
	})
	return sub.ch
}

// Publish numbers the event and hands it to every subscriber under one lock,
// so each sees events in ID order.
func (b *Bus) Publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastEventID++
	event.ID = b.lastEventID
	b.history[event.ID%historyLimit] = event
	for id, sub := range b.subscribers {
		if !sub.wants(event) {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			log.Printf("events: subscriber of session %q fell %d events behind; closing it", sub.sessionID, channelBuffer)
			b.drop(id)
		}
	}
}

// drop closes a subscription once; the caller holds b.mu.
func (b *Bus) drop(id int) {
	if sub, ok := b.subscribers[id]; ok {
		delete(b.subscribers, id)
		close(sub.ch)
	}
}

// LastID is the ID of the newest published event, 0 before the first.
func (b *Bus) LastID() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastEventID
}
