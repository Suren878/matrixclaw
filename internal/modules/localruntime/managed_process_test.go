package localruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestManagedProcessRunningFollowsDone(t *testing.T) {
	done := make(chan struct{})
	process := &managedProcess{done: done}
	if !process.running() {
		t.Fatal("running() = false, want true before done closes")
	}
	close(done)
	if process.running() {
		t.Fatal("running() = true, want false after done closes")
	}
}

func TestManagedProcessStopHandlesNilAndFinishedProcess(t *testing.T) {
	var nilProcess *managedProcess
	if err := nilProcess.stop(time.Millisecond); err != nil {
		t.Fatalf("nil stop() error = %v, want nil", err)
	}

	done := make(chan struct{})
	close(done)
	process := &managedProcess{done: done}
	if err := process.stop(time.Millisecond); err != nil {
		t.Fatalf("finished stop() error = %v, want nil", err)
	}
}

func TestWaitHTTPReadyReturnsOnReadyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	r := New(t.TempDir())
	err := r.waitHTTPReady(context.Background(), httpReadyOptions{
		url:     server.URL,
		timeout: time.Second,
		ready: func(response *http.Response) bool {
			return response.StatusCode >= 200 && response.StatusCode < 500
		},
		exitedMessage:  "test server exited before it was ready",
		timeoutMessage: "test server did not start",
	})
	if err != nil {
		t.Fatalf("waitHTTPReady() error = %v, want nil", err)
	}
}

func TestWaitHTTPReadyReturnsProviderSpecificExitError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	done := make(chan struct{})
	close(done)
	r := New(t.TempDir())
	err := r.waitHTTPReady(context.Background(), httpReadyOptions{
		url:            server.URL,
		timeout:        time.Second,
		process:        &managedProcess{done: done},
		exitedMessage:  "provider exited before ready",
		timeoutMessage: "provider did not start",
	})
	if err == nil {
		t.Fatal("waitHTTPReady() error = nil, want provider-specific exit error")
	}
	if err.Error() != "provider exited before ready" {
		t.Fatalf("waitHTTPReady() error = %q, want provider-specific exit error", err.Error())
	}
}
