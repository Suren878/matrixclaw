package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunStatusEdges(t *testing.T) {
	t.Parallel()
	statuses := []RunStatus{RunStatusAccepted, RunStatusRunning, RunStatusWaitingApproval, RunStatusWaitingEvents, RunStatusCompleted, RunStatusFailed, RunStatusCanceled}
	allowed := map[RunStatus][]RunStatus{
		RunStatusAccepted:        {RunStatusRunning, RunStatusFailed, RunStatusCanceled},
		RunStatusRunning:         {RunStatusWaitingApproval, RunStatusWaitingEvents, RunStatusCompleted, RunStatusFailed, RunStatusCanceled},
		RunStatusWaitingApproval: {RunStatusRunning, RunStatusFailed, RunStatusCanceled},
		RunStatusWaitingEvents:   {RunStatusRunning, RunStatusFailed, RunStatusCanceled},
	}
	for _, from := range statuses {
		for _, to := range statuses {
			want := false
			for _, ok := range allowed[from] {
				want = want || ok == to
			}
			if got := from.canBecome(to); got != want {
				t.Errorf("%s -> %s allowed = %v, want %v", from, to, got, want)
			}
		}
	}
}

// runStatusStore answers GetRun with a run in status.
type runStatusStore struct {
	Store
	status RunStatus
}

func (s runStatusStore) GetRun(_ context.Context, runID string) (Run, error) {
	return Run{ID: runID, Status: s.status}, nil
}

// runEndWaiters counts the callers waiting for runs to end.
func (c *Core) runEndWaiters() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, waiters := range c.runEnds {
		n += len(waiters)
	}
	return n
}

func TestWaitingForARunThatEndedReturnsAtOnce(t *testing.T) {
	t.Parallel()
	c := New(runStatusStore{status: RunStatusCompleted})

	if err := c.waitRunEnd(context.Background(), "run_done"); err != nil {
		t.Fatal(err)
	}
	if n := c.runEndWaiters(); n != 0 {
		t.Fatalf("waiters left = %d", n)
	}
}

func TestARunEndWakesItsWaitersAndForgetsThem(t *testing.T) {
	t.Parallel()
	c := New(runStatusStore{status: RunStatusRunning})
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- c.waitRunEnd(context.Background(), "run_child") }()
	}
	for c.runEndWaiters() < 2 {
		time.Sleep(time.Millisecond)
	}

	c.notifyRunEnd("run_child")

	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if n := c.runEndWaiters(); n != 0 {
		t.Fatalf("waiters left = %d", n)
	}
}

func TestAWaiterThatStopsIsForgotten(t *testing.T) {
	t.Parallel()
	c := New(runStatusStore{status: RunStatusWaitingApproval})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.waitRunEnd(ctx, "run_child") }()
	for c.runEndWaiters() == 0 {
		time.Sleep(time.Millisecond)
	}

	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error = %v", err)
	}
	if n := c.runEndWaiters(); n != 0 {
		t.Fatalf("waiters left = %d", n)
	}
}
