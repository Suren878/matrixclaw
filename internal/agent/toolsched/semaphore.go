// Package toolsched holds the daemon-wide scheduling primitives of native runs:
// a semaphore for model requests and keyed locks and batches for tool calls.
package toolsched

import (
	"context"
	"sync"
)

// Semaphore lets at most n holders in at once; a nil Semaphore never blocks.
type Semaphore struct {
	mu    sync.Mutex
	limit int
	held  int
	// freed is closed and replaced whenever a slot frees or the limit grows.
	freed chan struct{}
}

// NewSemaphore returns a semaphore with n slots, or nil when n <= 0.
func NewSemaphore(n int) *Semaphore {
	if n <= 0 {
		return nil
	}
	return &Semaphore{limit: n, freed: make(chan struct{})}
}

// Resize sets the number of slots to n > 0; holders over a smaller limit
// keep their slots until they release them.
func (s *Semaphore) Resize(n int) {
	if s == nil || n <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > s.limit {
		s.wakeLocked()
	}
	s.limit = n
}

// Acquire waits for a free slot or until ctx stops; release gives the slot back.
func (s *Semaphore) Acquire(ctx context.Context) (release func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return func() {}, nil
	}
	for {
		s.mu.Lock()
		if s.held < s.limit {
			s.held++
			s.mu.Unlock()
			return sync.OnceFunc(s.release), nil
		}
		freed := s.freed
		s.mu.Unlock()
		select {
		case <-freed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *Semaphore) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held--
	s.wakeLocked()
}

func (s *Semaphore) wakeLocked() {
	close(s.freed)
	s.freed = make(chan struct{})
}
