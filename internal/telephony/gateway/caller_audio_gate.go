package gateway

import (
	"strings"
	"sync"
	"time"
)

type callerAudioGate struct {
	mu       sync.RWMutex
	open     bool
	reason   string
	onOpen   func(string)
	done     chan struct{}
	timer    *time.Timer
	stopOnce sync.Once
}

func newCallerAudioGate(enabled bool, timeout time.Duration) *callerAudioGate {
	gate := &callerAudioGate{
		open: !enabled,
		done: make(chan struct{}),
	}
	if !enabled {
		close(gate.done)
		return gate
	}
	if timeout <= 0 {
		timeout = 6 * time.Second
	}
	gate.mu.Lock()
	gate.timer = time.AfterFunc(timeout, func() {
		gate.Open("timeout")
	})
	gate.mu.Unlock()
	return gate
}

func (g *callerAudioGate) Allow() bool {
	if g == nil {
		return true
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.open
}

func (g *callerAudioGate) Done() <-chan struct{} {
	if g == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return g.done
}

func (g *callerAudioGate) Open(reason string) bool {
	if g == nil {
		return false
	}
	reason = strings.TrimSpace(reason)
	g.mu.Lock()
	if g.open {
		g.mu.Unlock()
		return false
	}
	g.open = true
	g.reason = reason
	onOpen := g.onOpen
	if g.timer != nil {
		g.timer.Stop()
	}
	close(g.done)
	g.mu.Unlock()
	if onOpen != nil {
		onOpen(reason)
	}
	return true
}

func (g *callerAudioGate) SetOnOpen(onOpen func(string)) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.onOpen = onOpen
	open := g.open
	reason := g.reason
	g.mu.Unlock()
	if open && onOpen != nil && reason != "" {
		onOpen(reason)
	}
}

func (g *callerAudioGate) Stop() {
	if g == nil {
		return
	}
	g.stopOnce.Do(func() {
		if g.timer != nil {
			g.timer.Stop()
		}
		g.Open("stop")
	})
}

func callerAudioGatePlaybackDelay(samples8k int) time.Duration {
	if samples8k <= 0 {
		return 0
	}
	delay := time.Duration(samples8k) * time.Second / debugAudioSampleRateHz
	delay += 300 * time.Millisecond
	if delay > 7*time.Second {
		return 7 * time.Second
	}
	return delay
}
