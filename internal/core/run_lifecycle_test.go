package core

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/transcript"
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
	status *atomic.Value
}

func statusStore(status RunStatus) runStatusStore {
	s := runStatusStore{status: &atomic.Value{}}
	s.status.Store(status)
	return s
}

func (s runStatusStore) GetRun(_ context.Context, runID string) (Run, error) {
	return Run{ID: runID, Status: s.status.Load().(RunStatus)}, nil
}

// runEndWaiters counts the callers waiting for runs to end.
func (c *Core) runEndWaiters() int {
	c.runEnds.mu.Lock()
	defer c.runEnds.mu.Unlock()
	n := 0
	for _, waiters := range c.runEnds.waiters {
		n += len(waiters)
	}
	return n
}

func TestWaitingForARunThatEndedReturnsAtOnce(t *testing.T) {
	t.Parallel()
	c := New(Config{Store: statusStore(RunStatusCompleted)})

	if err := c.waitRunEnd(context.Background(), "run_done"); err != nil {
		t.Fatal(err)
	}
	if n := c.runEndWaiters(); n != 0 {
		t.Fatalf("waiters left = %d", n)
	}
}

func TestARunEndWakesItsWaitersAndForgetsThem(t *testing.T) {
	t.Parallel()
	store := statusStore(RunStatusRunning)
	c := New(Config{Store: store})
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- c.waitRunEnd(context.Background(), "run_child") }()
	}
	for c.runEndWaiters() < 2 {
		time.Sleep(time.Millisecond)
	}

	c.runEnds.begin("run_child")
	store.status.Store(RunStatusCompleted)
	c.runEnds.end("run_child")

	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if n := c.runEndWaiters(); n != 0 {
		t.Fatalf("waiters left = %d", n)
	}
}

func TestAWaiterWaitsForWhatFollowsTheEndItSees(t *testing.T) {
	t.Parallel()
	c := New(Config{Store: statusStore(RunStatusCompleted)})
	c.runEnds.begin("run_child")
	done := make(chan error, 1)
	go func() { done <- c.waitRunEnd(context.Background(), "run_child") }()
	for c.runEndWaiters() == 0 {
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("returned before the follow-up finished: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	c.runEnds.end("run_child")

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAWaiterThatStopsIsForgotten(t *testing.T) {
	t.Parallel()
	c := New(Config{Store: statusStore(RunStatusWaitingApproval)})
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

// failingFollowUpStore commits run changes but fails the steps after them.
type failingFollowUpStore struct {
	runStatusStore
}

func (s failingFollowUpStore) SealRun(_ context.Context, run Run, _ *transcript.Message, _ bool) error {
	s.status.Store(run.Status)
	return nil
}
func (failingFollowUpStore) DeleteRunWakeup(context.Context, string) error { return nil }
func (failingFollowUpStore) DeleteRunCheckpoint(context.Context, string) error {
	return errors.New("disk full")
}
func (failingFollowUpStore) ListRunApprovals(context.Context, string, string) ([]Approval, error) {
	return nil, errors.New("disk full")
}
func (failingFollowUpStore) ListTasks(context.Context, TaskFilter) ([]Task, error) { return nil, nil }

func TestARunEndWakesItsWaitersWhenTheStepsAfterItFail(t *testing.T) {
	t.Parallel()
	c := New(Config{Store: failingFollowUpStore{statusStore(RunStatusRunning)}})
	done := make(chan error, 1)
	go func() { done <- c.waitRunEnd(context.Background(), "run_child") }()
	for c.runEndWaiters() < 1 {
		time.Sleep(time.Millisecond)
	}

	run := Run{ID: "run_child", SessionID: "session_child", Status: RunStatusRunning}
	if err := c.transition(context.Background(), &run, runChange{To: RunStatusFailed, Err: "boom"}); err != nil {
		t.Fatalf("transition = %v, want the committed change to stand", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter was not woken")
	}
}
