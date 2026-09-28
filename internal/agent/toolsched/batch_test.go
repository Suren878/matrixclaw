package toolsched

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// receive fails the test instead of hanging when a call never signals.
func receive(t *testing.T, ch <-chan int) int {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("a call never started")
		return 0
	}
}

func drain(b *Batch[int]) []Done[int] {
	var out []Done[int]
	for b.Running() > 0 {
		out = append(out, b.Next())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

func TestBatchRunsCallsOfDifferentKeysAtOnce(t *testing.T) {
	b := NewBatch[int](context.Background(), NewLocks(), 8)
	started, release := make(chan int), make(chan struct{})
	for i, key := range []string{"", "", "dir:/a", "mcp.browser"} {
		b.Go(i, key, func(context.Context) int {
			started <- i
			<-release
			return i * 10
		})
	}

	for range 4 {
		receive(t, started)
	}
	close(release)

	for i, done := range drain(b) {
		if done.Index != i || done.Err != nil || done.Value != i*10 {
			t.Fatalf("done %d = %+v", i, done)
		}
	}
}

func TestBatchRunsCallsOfOneKeyInStartOrder(t *testing.T) {
	b := NewBatch[int](context.Background(), NewLocks(), 8)
	var mu sync.Mutex
	var order []int
	var inFlight, most atomic.Int32
	for i := range 4 {
		b.Go(i, "dir:/work", func(context.Context) int {
			n := inFlight.Add(1)
			for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
			}
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			inFlight.Add(-1)
			return i
		})
	}

	drain(b)

	if most.Load() != 1 || len(order) != 4 || order[0] != 0 || order[1] != 1 || order[2] != 2 || order[3] != 3 {
		t.Fatalf("order = %v, most at once = %d", order, most.Load())
	}
}

func TestBatchKeyWaitsForAnotherRunsHolder(t *testing.T) {
	locks := NewLocks()
	unlock, err := locks.Lock(context.Background(), "dir:/work")
	if err != nil {
		t.Fatal(err)
	}
	var held atomic.Bool
	held.Store(true)
	b := NewBatch[int](context.Background(), locks, 8)
	b.Go(0, "dir:/work", func(context.Context) int {
		if held.Load() {
			return -1
		}
		return 1
	})

	held.Store(false)
	unlock()

	if done := b.Next(); done.Err != nil || done.Value != 1 {
		t.Fatalf("done = %+v, want the call to run after the other holder", done)
	}
}

func TestBatchLimitsCallsRunningAtOnce(t *testing.T) {
	b := NewBatch[int](context.Background(), NewLocks(), 2)
	started, release := make(chan int), make(chan struct{})
	var inFlight, most atomic.Int32
	for i := range 3 {
		b.Go(i, "", func(context.Context) int {
			n := inFlight.Add(1)
			for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
			}
			started <- i
			<-release
			inFlight.Add(-1)
			return i
		})
	}

	receive(t, started)
	receive(t, started)
	close(release)
	receive(t, started)
	drain(b)

	if most.Load() != 2 {
		t.Fatalf("%d calls ran at once, want 2", most.Load())
	}
}

func TestBatchCallStoppedBeforeItsTurnDoesNotRun(t *testing.T) {
	locks := NewLocks()
	unlock, err := locks.Lock(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	b := NewBatch[int](ctx, locks, 8)
	var ran atomic.Bool
	b.Go(0, "k", func(context.Context) int {
		ran.Store(true)
		return 1
	})
	b.Go(1, "k", func(context.Context) int {
		ran.Store(true)
		return 2
	})

	cancel()

	for _, done := range drain(b) {
		if !errors.Is(done.Err, context.Canceled) {
			t.Fatalf("done = %+v, want context.Canceled", done)
		}
	}
	if ran.Load() {
		t.Fatal("a call ran after its batch stopped")
	}
}

func TestBatchReportsAPanickingCallAndFreesItsKey(t *testing.T) {
	b := NewBatch[int](context.Background(), NewLocks(), 8)
	b.Go(0, "k", func(context.Context) int { panic("boom") })
	b.Go(1, "k", func(context.Context) int { return 7 })

	got := drain(b)

	if !errors.Is(got[0].Err, ErrPanicked) || got[1].Err != nil || got[1].Value != 7 {
		t.Fatalf("done = %+v", got)
	}
}
