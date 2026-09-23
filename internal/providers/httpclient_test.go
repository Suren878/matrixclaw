package providers

import (
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
}
