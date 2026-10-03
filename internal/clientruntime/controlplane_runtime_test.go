package clientruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

type recordedRequests struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordedRequests) add(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req.Method+" "+req.URL.Path)
}

func (r *recordedRequests) has(call string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.calls {
		if existing == call {
			return true
		}
	}
	return false
}

func newTestDispatcher(t *testing.T, handler http.HandlerFunc) (*controlplane.Dispatcher, *recordedRequests) {
	t.Helper()
	recorded := &recordedRequests{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded.add(r)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	runtime := ControlplaneRuntime{
		Client:      "test",
		ExternalKey: "test:1",
		Owner:       true,
		Daemon: func(externalKey string) (*daemonclient.Client, error) {
			return daemonclient.New(server.URL, "test", externalKey), nil
		},
	}
	return controlplane.New(runtime, ""), recorded
}

func TestMemoryCommandListsDaemonMemories(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/memory" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(core.MemoryResponse{Memories: []core.MemoryEntry{{Scope: core.MemoryScopeGlobal, Key: "lang", Content: "Go"}}})
	})
	result, err := dispatcher.Handle(context.Background(), "test:1", "/memory")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if result.Info == nil || !strings.Contains(result.Info.Text, "global/lang: Go") {
		t.Fatalf("result = %+v, want memory info with the daemon entry", result)
	}
}

func TestSkillRemoveConfirmDeletesSkill(t *testing.T) {
	dispatcher, recorded := newTestDispatcher(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/modules/skills/demo":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/modules/skills":
			_ = json.NewEncoder(w).Encode(map[string]any{"skills": []any{}})
		default:
			http.Error(w, `{"error":"unexpected"}`, http.StatusBadRequest)
		}
	})
	result, err := dispatcher.Handle(context.Background(), "test:1", "/modules skills library demo remove confirm")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !recorded.has("DELETE /v1/modules/skills/demo") {
		t.Fatalf("requests = %v, want DELETE of the skill", recorded.calls)
	}
	if result.Picker == nil {
		t.Fatalf("result = %+v, want the skills list after removal", result)
	}
}
