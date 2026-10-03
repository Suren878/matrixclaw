package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestReloadingTheSnapshotClosesThePreviousEventStream(t *testing.T) {
	var open atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/events" {
			http.NotFound(w, r)
			return
		}
		open.Add(1)
		defer open.Add(-1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: ready\ndata: {}\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newApp(ctx, New(Config{BaseURL: server.URL}))
	snapshot := core.ClientSnapshot{SessionID: "session_1"}
	for range 3 {
		m.applySnapshot(snapshot)
		ready, ok := m.subscribeCmd(snapshot.SessionID, m.streamID, m.lastEventID)().(subscribeReadyMsg)
		if !ok || ready.err != nil {
			t.Fatalf("subscribe = %#v", ready)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for open.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := open.Load(); got != 1 {
		t.Fatalf("open event streams = %d, want 1", got)
	}
}
