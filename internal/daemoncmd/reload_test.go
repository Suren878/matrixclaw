package daemoncmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/store"
)

type closeCountingRuntime struct {
	externalagents.RuntimeAgent
	closed atomic.Int32
}

func (r *closeCountingRuntime) Close() error {
	r.closed.Add(1)
	return nil
}

func TestReloadKeepsExternalAgentsWhenTheirConfigIsUnchanged(t *testing.T) {
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqliteStore.Close() }()
	s := newSupervisor(context.Background(), nil, core.New(sqliteStore), nil, nil)
	runtime := &closeCountingRuntime{}
	cfg := map[string]setup.ExternalAgentConfig{"codex": {Enabled: true}}
	s.SetExternalAgents(sqliteStore, []externalagents.RuntimeAgent{runtime}, cfg)

	if err := s.applyExternalAgents(setup.ModulesConfig{ExternalAgents: map[string]setup.ExternalAgentConfig{"codex": {Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	if got := runtime.closed.Load(); got != 0 {
		t.Fatalf("runtime closed %d times on an unrelated reload", got)
	}
	if err := s.applyExternalAgents(setup.ModulesConfig{ExternalAgents: map[string]setup.ExternalAgentConfig{"codex": {Enabled: false}}}); err != nil {
		t.Fatal(err)
	}
	if got := runtime.closed.Load(); got != 1 {
		t.Fatalf("runtime closed %d times after its config changed, want 1", got)
	}
	s.CloseExternalAgents()
}

type fakeBotAPI struct {
	polls     atomic.Int32
	cancelled atomic.Int32
	started   chan struct{}
}

func newFakeBotAPI(t *testing.T) (*fakeBotAPI, *httptest.Server) {
	t.Helper()
	bot := &fakeBotAPI{started: make(chan struct{}, 16)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			bot.polls.Add(1)
			bot.started <- struct{}{}
			select {
			case <-r.Context().Done():
				bot.cancelled.Add(1)
				return
			case <-time.After(time.Second):
			}
			_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
		case strings.Contains(r.URL.Path, "/bot"):
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return bot, server
}

func (b *fakeBotAPI) waitForPoll(t *testing.T) {
	t.Helper()
	select {
	case <-b.started:
	case <-time.After(5 * time.Second):
		t.Fatal("telegram worker did not poll")
	}
}

func telegramTestBootstrap(addr string, allowedUserID int64) bootstrapConfig {
	return bootstrapConfig{
		Addr:     addr,
		Telegram: telegramClientBootstrap{Enabled: true, BotToken: "123:test", AllowedUserID: allowedUserID},
	}
}

func TestTelegramAdapterRestartsWorkerOnlyWhenItsConfigChanges(t *testing.T) {
	bot, server := newFakeBotAPI(t)
	adapter := &telegramClientAdapter{botAPIURL: server.URL}
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		adapter.mu.Lock()
		adapter.stopWorker()
		adapter.mu.Unlock()
	}()
	addr := strings.TrimPrefix(server.URL, "http://")

	if err := adapter.Apply(ctx, telegramTestBootstrap(addr, 1)); err != nil {
		t.Fatal(err)
	}
	bot.waitForPoll(t)
	if err := adapter.Apply(ctx, telegramTestBootstrap(addr, 1)); err != nil {
		t.Fatal(err)
	}
	bot.waitForPoll(t)
	if got := bot.cancelled.Load(); got != 0 {
		t.Fatalf("unchanged config cancelled %d polls", got)
	}

	if err := adapter.Apply(ctx, telegramTestBootstrap(addr, 2)); err != nil {
		t.Fatal(err)
	}
	bot.waitForPoll(t)
	deadline := time.Now().Add(2 * time.Second)
	for bot.cancelled.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := bot.cancelled.Load(); got != 1 {
		t.Fatalf("changed config cancelled %d polls, want 1", got)
	}
}
