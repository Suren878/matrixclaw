package gateway

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

type ariEventHub struct {
	ari *ariClient
	app string

	once sync.Once
	mu   sync.RWMutex

	ready       bool
	readyNotify chan struct{}

	nextSubscriberID int
	subscribers      map[int]chan ariEvent
}

func newARIEventHub(ari *ariClient, app string) *ariEventHub {
	return &ariEventHub{
		ari:         ari,
		app:         app,
		readyNotify: make(chan struct{}),
		subscribers: map[int]chan ariEvent{},
	}
}

func (h *ariEventHub) Start(ctx context.Context) {
	if h == nil {
		return
	}
	h.once.Do(func() {
		go h.run(ctx)
	})
}

func (h *ariEventHub) WaitReady(ctx context.Context) error {
	if h == nil || h.ari == nil {
		return errors.New("ARI event hub is not configured")
	}
	for {
		h.mu.RLock()
		if h.ready {
			h.mu.RUnlock()
			return nil
		}
		notify := h.readyNotify
		h.mu.RUnlock()

		select {
		case <-notify:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (h *ariEventHub) Subscribe(buffer int) (<-chan ariEvent, func()) {
	if h == nil {
		closed := make(chan ariEvent)
		close(closed)
		return closed, func() {}
	}
	if buffer <= 0 {
		buffer = 64
	}
	ch := make(chan ariEvent, buffer)
	h.mu.Lock()
	id := h.nextSubscriberID
	h.nextSubscriberID++
	h.subscribers[id] = ch
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subscribers, id)
		h.mu.Unlock()
	}
}

func (h *ariEventHub) run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			h.setReady(false)
			return
		}
		events, err := h.ari.events(ctx, h.app)
		if err != nil {
			h.setReady(false)
			log.Printf("telephony ARI event hub connect failed: %v", err)
			if !sleepContext(ctx, 2*time.Second) {
				return
			}
			continue
		}
		h.setReady(true)
		log.Printf("telephony ARI event hub registered app %s", h.app)
		pingCtx, stopPing := context.WithCancel(ctx)
		go h.keepAlive(pingCtx, events)
		for {
			event, err := events.read(ctx)
			if err != nil {
				stopPing()
				events.Close()
				h.setReady(false)
				if ctx.Err() != nil {
					return
				}
				log.Printf("telephony ARI event hub disconnected: %v", err)
				break
			}
			h.broadcast(event)
		}
		stopPing()
		if !sleepContext(ctx, time.Second) {
			return
		}
	}
}

func (h *ariEventHub) keepAlive(ctx context.Context, events *ariEvents) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := events.ping(pingCtx)
			cancel()
			if err != nil {
				log.Printf("telephony ARI event hub ping failed: %v", err)
				events.Close()
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (h *ariEventHub) setReady(ready bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ready {
		if !h.ready {
			h.ready = true
			close(h.readyNotify)
		}
		return
	}
	if h.ready {
		h.ready = false
		h.readyNotify = make(chan struct{})
	}
}

func (h *ariEventHub) broadcast(event ariEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func waitForARIEvent(ctx context.Context, events <-chan ariEvent, timeout time.Duration, match func(ariEvent) bool) (ariEvent, error) {
	waitCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return ariEvent{}, errors.New("ARI event subscription closed")
			}
			if match == nil || match(event) {
				return event, nil
			}
		case <-waitCtx.Done():
			return ariEvent{}, waitCtx.Err()
		}
	}
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		return true
	}
	select {
	case <-time.After(duration):
		return true
	case <-ctx.Done():
		return false
	}
}
