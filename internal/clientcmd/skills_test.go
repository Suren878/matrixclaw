package clientcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/daemonclient"
	appsetup "github.com/Suren878/matrixclaw/internal/setup"
)

func TestSkillsCommandUsesTheDaemon(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	var installPath string
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method == http.MethodPost && r.URL.Path == "/v1/modules/skills" {
			var req struct {
				Path string `json:"path"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			installPath = req.Path
		}
		_, _ = w.Write([]byte(`{"skills":[]}`))
	}))
	defer daemon.Close()

	store := appsetup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	if err := store.Save(appsetup.Config{}); err != nil {
		t.Fatal(err)
	}
	service := appsetup.NewService(store)
	restore := stubDaemon(daemon.URL)
	defer restore()

	var stdout, stderr bytes.Buffer
	if code := runSkillsCommand(&stdout, &stderr, "matrixclaw", service, []string{"remove", "demo"}); code != 0 {
		t.Fatalf("remove exit=%d stderr=%s", code, stderr.String())
	}
	if code := runSkillsCommand(&stdout, &stderr, "matrixclaw", service, []string{"install", "skills/demo"}); code != 0 {
		t.Fatalf("install exit=%d stderr=%s", code, stderr.String())
	}
	if len(calls) != 2 || calls[0] != "DELETE /v1/modules/skills/demo" || calls[1] != "POST /v1/modules/skills" {
		t.Fatalf("daemon calls = %v", calls)
	}
	if !filepath.IsAbs(installPath) {
		t.Fatalf("install path = %q, want it resolved against the CLI working directory", installPath)
	}
}

func stubDaemon(baseURL string) func() {
	oldEnsure, oldClient := ensureDaemon, newDaemonClient
	ensureDaemon = func(context.Context, *appsetup.Service) (appsetup.DaemonSummary, error) {
		return appsetup.DaemonSummary{}, nil
	}
	newDaemonClient = func(string) *daemonclient.Client {
		return daemonclient.New(baseURL, "test", "local")
	}
	return func() { ensureDaemon, newDaemonClient = oldEnsure, oldClient }
}
