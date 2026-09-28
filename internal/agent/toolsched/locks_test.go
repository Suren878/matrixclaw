package toolsched

import (
	"context"
	"errors"
	"testing"
)

func TestLocksHoldOneKeyAtATime(t *testing.T) {
	locks := NewLocks()
	unlock, err := locks.Lock(context.Background(), "dir:/work")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locks.Lock(context.Background(), "dir:/other"); err != nil {
		t.Fatalf("another key waited: %v", err)
	}
	if _, err := locks.Lock(context.Background(), ""); err != nil {
		t.Fatalf("the empty key waited: %v", err)
	}
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := locks.Lock(stopped, "dir:/work"); !errors.Is(err, context.Canceled) {
		t.Fatalf("second holder err = %v, want it kept out", err)
	}
	unlock()
	again, err := locks.Lock(context.Background(), "dir:/work")
	if err != nil {
		t.Fatalf("a freed key stayed locked: %v", err)
	}
	again()
}

func TestLocksWaiterGetsTheKeyOnceItIsFreed(t *testing.T) {
	locks := NewLocks()
	unlock, err := locks.Lock(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan error)
	go func() {
		release, err := locks.Lock(context.Background(), "k")
		if err == nil {
			release()
		}
		got <- err
	}()
	unlock()
	if err := <-got; err != nil {
		t.Fatal(err)
	}
	locks.mu.Lock()
	defer locks.mu.Unlock()
	if len(locks.held) != 0 {
		t.Fatalf("free keys kept: %v", locks.held)
	}
}
