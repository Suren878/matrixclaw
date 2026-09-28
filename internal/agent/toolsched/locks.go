package toolsched

import (
	"context"
	"sync"
)

// Locks is a keyed mutex shared by every run of the daemon: holders of one key
// run one at a time; the empty key is never locked.
type Locks struct {
	mu   sync.Mutex
	held map[string]*keyLock
}

type keyLock struct {
	slot  chan struct{}
	users int
}

// NewLocks returns an empty set of keyed locks.
func NewLocks() *Locks {
	return &Locks{held: map[string]*keyLock{}}
}

// Lock waits until key is free or ctx stops; unlock frees the key.
func (l *Locks) Lock(ctx context.Context, key string) (unlock func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if key == "" {
		return func() {}, nil
	}
	l.mu.Lock()
	lock := l.held[key]
	if lock == nil {
		lock = &keyLock{slot: make(chan struct{}, 1)}
		l.held[key] = lock
	}
	lock.users++
	l.mu.Unlock()
	select {
	case lock.slot <- struct{}{}:
		return func() {
			<-lock.slot
			l.leave(key, lock)
		}, nil
	case <-ctx.Done():
		l.leave(key, lock)
		return nil, ctx.Err()
	}
}

// leave forgets a key nobody holds or waits for.
func (l *Locks) leave(key string, lock *keyLock) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lock.users--; lock.users == 0 {
		delete(l.held, key)
	}
}
