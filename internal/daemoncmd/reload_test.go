package daemoncmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/automation"
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
	s := newSupervisor(context.Background(), core.New(core.Config{Store: sqliteStore}), nil, nil, nil)
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
	offset    atomic.Int64 // of the last poll
	cancelled atomic.Int32
	started   chan struct{}
}

func newFakeBotAPI(t *testing.T) (*fakeBotAPI, *httptest.Server) {
	t.Helper()
	bot := &fakeBotAPI{started: make(chan struct{}, 16)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var poll struct {
			Offset int64 `json:"offset"`
		}
		_ = json.NewDecoder(r.Body).Decode(&poll)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			bot.offset.Store(poll.Offset)
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

func TestTelegramAdapterPollsANewBotFromTheStart(t *testing.T) {
	bot, server := newFakeBotAPI(t)
	adapter := &telegramClientAdapter{botAPIURL: server.URL}
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		adapter.mu.Lock()
		adapter.stopWorker()
		adapter.mu.Unlock()
	}()
	cfg := telegramTestBootstrap(strings.TrimPrefix(server.URL, "http://"), 1)
	if err := adapter.Apply(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	bot.waitForPoll(t)
	adapter.offset.Store(987654321)

	cfg.Telegram.AllowedUserID = 2
	if err := adapter.Apply(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	bot.waitForPoll(t)
	if got := bot.offset.Load(); got != 987654321 {
		t.Fatalf("same bot polled from offset %d, want the confirmed one", got)
	}
	cfg.Telegram.BotToken = "456:other"
	if err := adapter.Apply(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	bot.waitForPoll(t)
	if got := bot.offset.Load(); got != 0 {
		t.Fatalf("new bot polled from offset %d, want 0", got)
	}
}

func TestRestartNoticeIsHeldUntilTheNextStart(t *testing.T) {
	ctx := context.Background()
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqliteStore.Close() }()
	app := core.New(core.Config{Store: sqliteStore})
	s := newSupervisor(ctx, app, nil, nil, nil)
	saved, err := s.saveRestartDelivery(ctx, core.AdminRestartRequest{Notification: &core.ClientDeliveryTarget{Client: "telegram", ExternalKey: "7", Address: []byte(`{"kind":"chat","chat_id":7,"message_id":3}`)}})
	if err != nil {
		t.Fatal(err)
	}
	pending := func() []core.ClientDelivery {
		deliveries, err := app.ListClientDeliveries(ctx, core.ClientDeliveryFilter{Client: "telegram", Status: core.ClientDeliveryStatusPending})
		if err != nil {
			t.Fatal(err)
		}
		return deliveries
	}
	if saved.Status != core.ClientDeliveryStatusHeld || len(pending()) != 0 {
		t.Fatalf("saved = %+v, pending before restart = %v", saved, pending())
	}
	if err := app.ReleaseHeldClientDeliveries(ctx); err != nil {
		t.Fatal(err)
	}
	released := pending()
	if len(released) != 1 || released[0].Type != core.ClientDeliveryTypeNotice || released[0].Summary != daemonRestartText || string(released[0].Payload) != `{"replace":true}` {
		t.Fatalf("released = %+v", released)
	}
}

type idleRunStarter struct{}

func (idleRunStarter) StartRun(context.Context, string) error { return nil }

func TestReloadReappliesRunLimitsAndAutomationSettings(t *testing.T) {
	_, botAPI := newFakeBotAPI(t)
	dir := t.TempDir()
	setupPath := filepath.Join(dir, "setup.json")
	dbPath := filepath.Join(dir, "matrixclaw.db")
	for name, value := range map[string]string{
		"HOME": dir, "XDG_STATE_HOME": filepath.Join(dir, "xdg-state"), "XDG_CONFIG_HOME": filepath.Join(dir, "xdg-config"),
		"MATRIXCLAW_SETUP_PATH": setupPath, "MATRIXCLAW_HTTP_ADDR": "", "MATRIXCLAW_DB_PATH": "", "MATRIXCLAW_API_TOKEN": "", "MATRIXCLAW_TIMEZONE": "",
	} {
		t.Setenv(name, value)
	}
	save := func(cfg setup.Config) {
		t.Helper()
		cfg.Version = setup.CurrentVersion
		cfg.Daemon.DBPath, cfg.Daemon.APIToken, cfg.Daemon.HTTPAddr = dbPath, "test-token", strings.TrimPrefix(botAPI.URL, "http://")
		if err := setup.NewFileStore(setupPath).Save(cfg); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	sqliteStore, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqliteStore.Close() }()
	app := core.New(core.Config{Store: sqliteStore, RunStarter: idleRunStarter{}})
	jobs := automation.NewService(automation.NewSQLiteStore(sqliteStore.DB()), app)
	s := newSupervisor(ctx, app, jobs, nil, nil)
	s.telegram.botAPIURL = botAPI.URL
	defer func() {
		s.telegram.mu.Lock()
		s.telegram.stopWorker()
		s.telegram.mu.Unlock()
	}()
	defaults := core.New(core.Config{Store: sqliteStore}).Tuning()
	// fire runs a new job at once in a session of its own, since a session
	// takes one triggered run at a time.
	fire := func() (automation.Job, []core.ClientDelivery) {
		t.Helper()
		now := time.Now().UTC()
		session := core.Session{ID: "s" + now.Format("150405.000000000"), Kind: core.SessionKindAssistant, Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now}
		if err := sqliteStore.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}
		runAt := now.Add(time.Hour)
		job, err := jobs.CreateJob(ctx, automation.CreateJobInput{SessionID: session.ID, ScheduleMode: automation.ScheduleModeOnce, RunAt: &runAt, Prompt: "check the backups"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := jobs.RunNow(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
		deliveries, err := app.ListClientDeliveries(ctx, core.ClientDeliveryFilter{TaskID: job.ID})
		if err != nil {
			t.Fatal(err)
		}
		return job, deliveries
	}

	save(setup.Config{Daemon: setup.DaemonConfig{Timezone: "UTC"}})
	if err := s.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if job, deliveries := fire(); job.Timezone != "UTC" || len(deliveries) != 0 {
		t.Fatalf("before: timezone %q, deliveries %+v", job.Timezone, deliveries)
	}

	save(setup.Config{
		Daemon: setup.DaemonConfig{
			Timezone:         "Asia/Almaty",
			Budgets:          setup.RunBudgetsConfig{User: setup.RunBudgetConfig{Steps: 7}},
			CompactModel:     setup.CompactModelConfig{Provider: "cheap", Model: "small"},
			ContextWindowCap: 64_000,
			ModelConcurrency: 2,
			BackgroundTasks:  3,
			BackgroundAgents: 1,
		},
		Clients: setup.ClientsConfig{Telegram: setup.TelegramConfig{Enabled: true, BotToken: "123:test", AllowedUserID: "42"}},
	})
	if err := s.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	got := app.Tuning()
	if got.Budgets.User.Steps != 7 || got.Budgets.Subagent != defaults.Budgets.Subagent {
		t.Errorf("budgets = %+v", got.Budgets)
	}
	if got.CompactProvider != "cheap" || got.CompactModel != "small" {
		t.Errorf("compact model = %s/%s", got.CompactProvider, got.CompactModel)
	}
	if got.WindowCap != 64_000 || got.ModelConcurrency != 2 || got.BackgroundTasks != 3 || got.BackgroundAgents != 1 {
		t.Errorf("limits = %+v", got)
	}
	job, deliveries := fire()
	if job.Timezone != "Asia/Almaty" {
		t.Errorf("timezone of a new job = %q", job.Timezone)
	}
	if len(deliveries) != 1 || deliveries[0].Client != "telegram" || deliveries[0].ExternalKey != "42" {
		t.Errorf("deliveries once Telegram is on = %+v", deliveries)
	}

	save(setup.Config{Daemon: setup.DaemonConfig{Timezone: "UTC"}})
	if err := s.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if got := app.Tuning(); got != defaults {
		t.Errorf("tuning after the settings are removed = %+v, want %+v", got, defaults)
	}
	if job, deliveries := fire(); job.Timezone != "UTC" || len(deliveries) != 0 {
		t.Errorf("after: timezone %q, deliveries %+v", job.Timezone, deliveries)
	}
}
