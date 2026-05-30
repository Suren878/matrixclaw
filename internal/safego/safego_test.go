package safego

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunReturnsTrueWhenFunctionCompletes(t *testing.T) {
	t.Parallel()

	called := false
	completed := Run("test.normal", func() {
		called = true
	})

	if !completed {
		t.Fatalf("Run returned false for non-panicking function")
	}
	if !called {
		t.Fatalf("Run did not execute function")
	}
}

func TestRunRecoversPanicAndReturnsFalse(t *testing.T) {
	var logs bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(originalOutput)
	})

	completed := Run("test.panic", func() {
		panic("boom")
	})

	if completed {
		t.Fatalf("Run returned true after panic")
	}
	got := logs.String()
	if !strings.Contains(got, `safego: recovered panic in "test.panic": boom`) {
		t.Fatalf("panic recovery log missing worker name and panic value: %q", got)
	}
	if !strings.Contains(got, "goroutine ") {
		t.Fatalf("panic recovery log missing stack trace: %q", got)
	}
}

func TestGoRecoversPanic(t *testing.T) {
	var logs lockedBuffer
	originalOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(originalOutput)
	})

	started := make(chan struct{})
	Go("test.go.panic", func() {
		close(started)
		panic("boom")
	})

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("Go did not start function")
	}

	deadline := time.After(time.Second)
	for {
		if strings.Contains(logs.String(), `safego: recovered panic in "test.go.panic": boom`) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("panic recovery log missing for Go: %q", logs.String())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}
