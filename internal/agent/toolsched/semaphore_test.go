package toolsched

import (
	"context"
	"errors"
	"testing"
)

func TestSemaphoreLetsInAtMostNHolders(t *testing.T) {
	s := NewSemaphore(2)
	first, err := s.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	full, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Acquire(full); !errors.Is(err, context.Canceled) {
		t.Fatalf("third holder err = %v, want it kept out", err)
	}
	first()
	if _, err := s.Acquire(context.Background()); err != nil {
		t.Fatalf("a released slot was not reusable: %v", err)
	}
}

func TestSemaphoreWaiterLeavesWhenItsContextStops(t *testing.T) {
	s := NewSemaphore(1)
	if _, err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, err := s.Acquire(ctx)
		done <- err
	}()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter err = %v", err)
	}
}

func TestNilSemaphoreNeverBlocks(t *testing.T) {
	var s *Semaphore
	for range 3 {
		release, err := s.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	if NewSemaphore(0) != nil {
		t.Fatal("NewSemaphore(0) is not the unbounded nil semaphore")
	}
}
