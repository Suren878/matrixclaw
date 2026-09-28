// Package toolsched holds the daemon-wide scheduling primitives of native runs:
// a semaphore for model requests and keyed locks and batches for tool calls.
package toolsched

import "context"

// Semaphore lets at most n holders in at once; a nil Semaphore never blocks.
type Semaphore struct {
	slots chan struct{}
}

// NewSemaphore returns a semaphore with n slots, or nil when n <= 0.
func NewSemaphore(n int) *Semaphore {
	if n <= 0 {
		return nil
	}
	return &Semaphore{slots: make(chan struct{}, n)}
}

// Acquire waits for a free slot or until ctx stops; release gives the slot back.
func (s *Semaphore) Acquire(ctx context.Context) (release func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return func() {}, nil
	}
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
