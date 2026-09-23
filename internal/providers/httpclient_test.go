package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientTimesOutOnlyOnSilence(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wait := func() {
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}
		if r.URL.Path == "/no-headers" {
			wait()
			return
		}
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		for i := 0; i < 5; i++ {
			_, _ = fmt.Fprintf(w, "data: %d\n\n", i)
			flusher.Flush()
			if r.URL.Path == "/stall" {
				wait()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer server.Close()
	defer close(release)
	client := newHTTPClient(200*time.Millisecond, 200*time.Millisecond)

	t.Run("slow but live stream completes", func(t *testing.T) {
		res, err := client.Get(server.URL + "/live")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		body, err := io.ReadAll(res.Body)
		if err != nil || len(body) != 5*len("data: 0\n\n") {
			t.Fatalf("body=%q err=%v", body, err)
		}
	})

	t.Run("stalled stream fails with retryable idle error", func(t *testing.T) {
		res, err := client.Get(server.URL + "/stall")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		_, err = io.ReadAll(res.Body)
		if !errors.Is(err, ErrStreamIdle) || !IsRetryableGenerationError(err) {
			t.Fatalf("err=%v, want retryable ErrStreamIdle", err)
		}
	})

	t.Run("missing headers fail with retryable timeout", func(t *testing.T) {
		_, err := client.Get(server.URL + "/no-headers")
		if err == nil || !IsRetryableGenerationError(err) {
			t.Fatalf("err=%v, want retryable header timeout", err)
		}
	})

	t.Run("caller cancel is context.Canceled, not idle timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/stall", nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		cancel()
		_, err = io.ReadAll(res.Body)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v, want context.Canceled", err)
		}
		if errors.Is(err, ErrStreamIdle) {
			t.Fatalf("err=%v, want not ErrStreamIdle", err)
		}
		if IsRetryableGenerationError(err) {
			t.Fatalf("err=%v, want not retryable", err)
		}
	})
}

// TestIdleTimerExcludesConsumerProcessingTime guards against counting time the
// caller spends between Read calls: only time actually waiting on the network
// must count against the idle budget.
func TestIdleTimerExcludesConsumerProcessingTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		for i := 0; i < 3; i++ {
			_, _ = fmt.Fprintf(w, "data: %d\n\n", i)
			flusher.Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer server.Close()

	client := newHTTPClient(200*time.Millisecond, 100*time.Millisecond)
	res, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	chunk := make([]byte, len("data: 0\n\n"))
	for i := 0; i < 3; i++ {
		if _, err := io.ReadFull(res.Body, chunk); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		// The consumer is slower than the idle limit between reads; that must
		// not be mistaken for a stalled network.
		time.Sleep(250 * time.Millisecond)
	}
	if _, err := res.Body.Read(chunk); err != io.EOF {
		t.Fatalf("final read err=%v, want io.EOF", err)
	}
}
