package toolsched

import (
	"context"
	"errors"

	"github.com/Suren878/matrixclaw/internal/safego"
)

// ErrPanicked reports a call that panicked; the batch goes on.
var ErrPanicked = errors.New("tool call panicked")

// Batch runs the calls of one model reply concurrently: calls sharing a key run
// one at a time in the order they were started, each holding its key in Locks,
// and at most limit calls run at once. Only the goroutine that created the batch
// may call its methods; Next reports every started call exactly once.
type Batch[T any] struct {
	ctx     context.Context
	locks   *Locks
	slots   *Semaphore
	tails   map[string]chan struct{}
	done    chan Done[T]
	running int
}

// Done is a finished call: Value is what it returned, or Err says why it did
// not run (its context stopped) or did not return (ErrPanicked).
type Done[T any] struct {
	Index int
	Value T
	Err   error
}

// NewBatch returns a batch whose calls run under ctx.
func NewBatch[T any](ctx context.Context, locks *Locks, limit int) *Batch[T] {
	return &Batch[T]{ctx: ctx, locks: locks, slots: NewSemaphore(limit), tails: map[string]chan struct{}{}, done: make(chan Done[T])}
}

// Go starts call index under key; run gets the batch's context.
func (b *Batch[T]) Go(index int, key string, run func(context.Context) T) {
	var after, finished chan struct{}
	if key != "" {
		after = b.tails[key]
		finished = make(chan struct{})
		b.tails[key] = finished
	}
	b.running++
	go func() {
		done := Done[T]{Index: index, Err: ErrPanicked}
		safego.Run("toolsched.call", func() {
			done.Value, done.Err = b.call(key, after, run)
		})
		if finished != nil {
			close(finished)
		}
		b.done <- done
	}()
}

func (b *Batch[T]) call(key string, after <-chan struct{}, run func(context.Context) T) (T, error) {
	var zero T
	if after != nil {
		select {
		case <-after:
		case <-b.ctx.Done():
			return zero, b.ctx.Err()
		}
	}
	unlock, err := b.locks.Lock(b.ctx, key)
	if err != nil {
		return zero, err
	}
	defer unlock()
	release, err := b.slots.Acquire(b.ctx)
	if err != nil {
		return zero, err
	}
	defer release()
	return run(b.ctx), nil
}

// Running is how many started calls Next has not reported yet.
func (b *Batch[T]) Running() int {
	return b.running
}

// Next waits until a started call finishes and reports it.
func (b *Batch[T]) Next() Done[T] {
	done := <-b.done
	b.running--
	return done
}
