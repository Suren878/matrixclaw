package setup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type noSystemd struct{}

func (noSystemd) Run(context.Context, string, ...string) error { return errNoSystemd }
func (noSystemd) Output(context.Context, string, ...string) ([]byte, error) {
	return nil, errNoSystemd
}

var errNoSystemd = &stubError{"no user session"}

type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }

// fakeDaemon answers health until it is asked to stop on /v1/admin/stop.
func fakeDaemon(t *testing.T, token string) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	var down, stopped atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/v1/admin/stop":
			if r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			stopped.Store(true)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			go func() { time.Sleep(300 * time.Millisecond); down.Store(true) }()
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &stopped
}

func TestStopWithoutSystemdUsesTheDaemonAPI(t *testing.T) {
	srv, stopped := fakeDaemon(t, "tok")
	m := newSystemdUserDaemonManager()
	m.runner = noSystemd{}
	cfg := Config{Daemon: DaemonConfig{HTTPAddr: strings.TrimPrefix(srv.URL, "http://"), APIToken: "tok"}}

	summary, err := m.Stop(context.Background(), "", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !stopped.Load() || summary.RuntimeStatus != "Stopped" {
		t.Fatalf("stopped=%v status=%q", stopped.Load(), summary.RuntimeStatus)
	}
	if m.healthCheck(context.Background(), cfg.Daemon.HTTPAddr) == nil {
		t.Fatal("daemon still answers")
	}
	// Nothing running is not an error.
	if _, err := m.Stop(context.Background(), "", cfg); err != nil {
		t.Fatal(err)
	}
}

func TestStopRejectedByTheDaemonIsAnError(t *testing.T) {
	srv, _ := fakeDaemon(t, "tok")
	m := newSystemdUserDaemonManager()
	m.runner = noSystemd{}
	cfg := Config{Daemon: DaemonConfig{HTTPAddr: strings.TrimPrefix(srv.URL, "http://"), APIToken: "wrong"}}
	if _, err := m.Stop(context.Background(), "", cfg); err == nil {
		t.Fatal("stop with a wrong token succeeded")
	}
}
