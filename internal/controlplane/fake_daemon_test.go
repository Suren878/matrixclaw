package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

// fakeDaemon serves the daemon HTTP API the dispatcher tests need: a bound
// session list by default, and whatever routes a test adds with on.
type fakeDaemon struct {
	t        *testing.T
	mu       sync.Mutex
	routes   map[string]func(*http.Request) any
	calls    []string
	sessions []core.Session
	bound    string
	server   *httptest.Server
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	return &fakeDaemon{
		t:        t,
		routes:   map[string]func(*http.Request) any{},
		sessions: []core.Session{{ID: "s1", Title: "s1"}},
		bound:    "s1",
	}
}

// on serves pattern (a ServeMux pattern) with handler, whose result is sent as
// JSON; an error result is sent as the error a core error maps to.
func (f *fakeDaemon) on(pattern string, handler func(*http.Request) any) *fakeDaemon {
	f.routes[pattern] = handler
	return f
}

func (f *fakeDaemon) dispatcher(role core.Role) *Dispatcher {
	if f.server == nil {
		f.start()
	}
	client := daemonclient.New(f.server.URL, "test", "key")
	client.Role = role
	return New(client, "")
}

// run handles command as the owner and fails the test on an error.
func (f *fakeDaemon) run(command string) Result {
	f.t.Helper()
	return f.runAs(core.RoleOwner, command)
}

func (f *fakeDaemon) runAs(role core.Role, command string) Result {
	f.t.Helper()
	result, err := f.dispatcher(role).Handle(context.Background(), command)
	if err != nil {
		f.t.Fatalf("%s: %v", command, err)
	}
	return result
}

func (f *fakeDaemon) called(call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, existing := range f.calls {
		if existing == call {
			count++
		}
	}
	return count
}

func (f *fakeDaemon) start() {
	defaults := map[string]func(*http.Request) any{
		"GET /v1/bindings/current": func(*http.Request) any {
			if f.bound == "" {
				return core.ErrBindingNotFound
			}
			return core.ClientBindingResponse{Binding: core.ClientBinding{SessionID: f.bound}}
		},
		"POST /v1/bindings/use": func(r *http.Request) any {
			input := decode[core.UseBindingInput](r)
			f.bound = input.SessionID
			return core.ClientBindingResponse{Binding: core.ClientBinding{SessionID: input.SessionID}}
		},
		"GET /v1/sessions": func(*http.Request) any { return core.SessionsResponse{Sessions: f.sessions} },
		"GET /v1/sessions/{id}": func(r *http.Request) any {
			index := slices.IndexFunc(f.sessions, func(s core.Session) bool { return s.ID == r.PathValue("id") })
			if index < 0 {
				return core.ErrNotFound
			}
			return core.SessionResponse{Session: f.sessions[index]}
		},
	}
	mux := http.NewServeMux()
	for pattern, handler := range defaults {
		if _, ok := f.routes[pattern]; !ok {
			f.routes[pattern] = handler
		}
	}
	for pattern, handler := range f.routes {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.calls = append(f.calls, r.Method+" "+r.URL.Path)
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			result := handler(r)
			if err, ok := result.(error); ok {
				w.WriteHeader(statusOf(err))
				_ = json.NewEncoder(w).Encode(core.ErrorResponse{Error: err.Error()})
				return
			}
			if result == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_ = json.NewEncoder(w).Encode(result)
		})
	}
	f.server = httptest.NewServer(mux)
	f.t.Cleanup(f.server.Close)
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, core.ErrNotFound), errors.Is(err, core.ErrBindingNotFound):
		return http.StatusNotFound
	case errors.Is(err, core.ErrOwnerOnly), errors.Is(err, core.ErrSessionRestricted):
		return http.StatusForbidden
	default:
		return http.StatusBadRequest
	}
}

func decode[T any](r *http.Request) T {
	var value T
	_ = json.NewDecoder(r.Body).Decode(&value)
	return value
}
