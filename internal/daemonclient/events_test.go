package daemonclient

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/safego"
)

func TestReadSSEPublishesEventAndClosesOnEOF(t *testing.T) {
	body := io.NopCloser(strings.NewReader("id: 12\nevent: message\ndata: {\"type\":\"message.created\",\"session_id\":\"session-1\",\"run_id\":\"run-1\"}\n\n"))
	events := make(chan LiveEvent, 16)
	errs := make(chan error, 1)

	readSSE(context.Background(), body, events, errs)

	event, ok := <-events
	if !ok {
		t.Fatalf("events closed before delivering event")
	}
	if event.ID != 12 || event.Type != core.EventMessageCreated || event.SessionID != "session-1" || event.RunID != "run-1" {
		t.Fatalf("event = %#v, want decoded message event with id 12", event)
	}
	if _, ok := <-events; ok {
		t.Fatalf("events channel remained open after EOF")
	}
	if err, ok := <-errs; ok {
		t.Fatalf("unexpected error after valid SSE: %v", err)
	}
}

func TestReadSSEConvertsReadPanicToError(t *testing.T) {
	body := &panicSSEBody{}
	events := make(chan LiveEvent, 16)
	errs := make(chan error, 1)

	_ = safego.Run("daemonclient.testReadSSE", func() {
		readSSE(context.Background(), body, events, errs)
	})

	if event, ok := <-events; ok {
		t.Fatalf("events delivered %#v, want closed channel after panic", event)
	}
	err, ok := <-errs
	if !ok {
		t.Fatalf("errs closed without panic error")
	}
	if !strings.Contains(err.Error(), "daemon event stream reader panicked") {
		t.Fatalf("error = %v, want panic reader error", err)
	}
	if err, ok := <-errs; ok {
		t.Fatalf("unexpected second error: %v", err)
	}
	if !body.closed.Load() {
		t.Fatalf("body was not closed after panic")
	}
}

type panicSSEBody struct {
	closed atomic.Bool
}

func (b *panicSSEBody) Read([]byte) (int, error) {
	panic("sse read boom")
}

func (b *panicSSEBody) Close() error {
	b.closed.Store(true)
	return nil
}
