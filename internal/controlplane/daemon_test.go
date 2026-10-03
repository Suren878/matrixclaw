package controlplane

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/api"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/store"
)

// apiDaemon runs the real daemon API over a fresh store with session s1 bound
// to the test client; deps adds the services a test needs.
type apiDaemon struct {
	t    *testing.T
	core *core.Core
	url  string
}

func newAPIDaemon(t *testing.T, deps api.Deps) *apiDaemon {
	t.Helper()
	st, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	ctx := context.Background()
	if err := st.CreateSession(ctx, core.Session{ID: "s1", Title: "s1", Kind: core.SessionKindAssistant, Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	deps.Core = core.New(st)
	if deps.Reload == nil {
		deps.Reload = func(context.Context) error { return nil }
	}
	if _, err := deps.Core.UseBinding(ctx, core.UseBindingInput{Client: "test", ExternalKey: "key", SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.New(deps).Handler())
	t.Cleanup(server.Close)
	return &apiDaemon{t: t, core: deps.Core, url: server.URL}
}

func (d *apiDaemon) dispatcher(role core.Role) *Dispatcher {
	client := daemonclient.New(d.url, "test", "key")
	client.Role = role
	return New(client, "")
}

func (d *apiDaemon) runAs(role core.Role, command string) Result {
	d.t.Helper()
	result, err := d.dispatcher(role).Handle(context.Background(), command)
	if err != nil {
		d.t.Fatalf("%s: %v", command, err)
	}
	return result
}

func (d *apiDaemon) run(command string) Result {
	d.t.Helper()
	return d.runAs(core.RoleOwner, command)
}

// newModulesDaemon runs the daemon API over mods, applied from a setup.json
// of its own after every change as the daemon does.
func newModulesDaemon(t *testing.T, mods ...modules.Module) (*apiDaemon, *setup.Service) {
	t.Helper()
	store := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	if err := store.Save(setup.Config{Version: setup.CurrentVersion}); err != nil {
		t.Fatal(err)
	}
	service := setup.NewService(store)
	set, err := modules.NewSet(nil, mods...)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(ctx context.Context) error {
		cfg, err := service.Load()
		if err != nil {
			return err
		}
		return set.Apply(ctx, cfg)
	}
	if err := apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	return newAPIDaemon(t, api.Deps{Setup: service, Modules: api.Modules{Set: set}, Reload: apply}), service
}
