package daemoncmd

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/api"
	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/checkpoint"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents/builtins"
	"github.com/Suren878/matrixclaw/internal/hooks"
	"github.com/Suren878/matrixclaw/internal/modules/geo"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	skillsmodule "github.com/Suren878/matrixclaw/internal/modules/skills"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	geminilive "github.com/Suren878/matrixclaw/internal/modules/voice/realtime/providers/gemini"
	grokvoice "github.com/Suren878/matrixclaw/internal/modules/voice/realtime/providers/grok"
	openairealtime "github.com/Suren878/matrixclaw/internal/modules/voice/realtime/providers/openai"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/shelltask"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func Run(ctx context.Context) error {
	bootstrap, err := loadBootstrap()
	if err != nil {
		return err
	}
	// Task recovery kills what the previous daemon left, so a second daemon on
	// the same data must not start.
	releaseData, err := lockDataDir(dataDir(bootstrap.DBPath))
	if err != nil {
		return err
	}
	defer func() { _ = releaseData() }()
	providers.UseModelMetadataCache(filepath.Join(dataDir(bootstrap.DBPath), "runtime", "context-windows.json"))

	sqliteStore, err := store.NewSQLite(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = sqliteStore.Close() }()
	automationStore, err := automation.NewSQLiteStore(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = automationStore.Close() }()

	localRuntime := localruntime.New("")
	defer func() { _ = localRuntime.Close() }()
	storageModule, err := localstorage.New(localstorage.Config{
		Root: defaultStorageRoot(bootstrap.DBPath),
	})
	if err != nil {
		return err
	}
	skillsModule, err := skillsmodule.New(sqliteStore.DB(), filepath.Join(dataDir(bootstrap.DBPath), "skills"))
	if err != nil {
		return err
	}

	app := core.New(sqliteStore).
		WithSessionLLMs(bootstrap.SessionLLMs).
		WithRunBudgets(bootstrap.Budgets).
		WithSessionFiles(sessionFilesRoot(bootstrap.DBPath)).
		WithCompactModel(bootstrap.CompactModel.Provider, bootstrap.CompactModel.Model).
		WithContextWindowCap(bootstrap.WindowCap).
		WithModelConcurrency(bootstrap.ModelConcurrency).
		WithBackgroundTaskLimit(bootstrap.BackgroundTasks).
		WithBackgroundAgents(bootstrap.BackgroundAgents).
		WithAttachmentReader(storageAttachmentReader{store: storageModule.Store()}).
		WithSkillsContext(skillsModule).
		WithPromptEnvironment(promptEnvironment(bootstrap))
	if bootstrap.Setup.Daemon.Checkpoints.On() {
		app.WithCheckpoints(checkpoint.New(filepath.Join(dataDir(bootstrap.DBPath), "checkpoints")))
	}
	externalRegistry, externalRuntimes, err := builtins.BuildRegistry(bootstrap.Setup.Modules)
	if err != nil {
		return err
	}
	app.WithExternalAgents(externalRegistry, sqliteStore)
	automationService := automation.NewService(automationStore, app, bootstrap.Timezone).
		WithDeliveryTargets(automationDeliveryTargets(bootstrap))
	osmGeo := geo.NewOSMServiceFromEnv()
	realtimeVoice := realtime.NewManager(app, geminilive.Spec, grokvoice.Spec, openairealtime.Spec)
	daemon, err := buildModules(moduleDeps{
		app:        app,
		automation: automationService,
		runtime:    localRuntime,
		storage:    storageModule,
		skills:     skillsModule,
		realtime:   realtimeVoice,
		geo:        osmGeo,
	})
	if err != nil {
		return err
	}
	app.WithTools(daemon.set)
	lifetime, stopLifetime := context.WithCancel(ctx)
	defer stopLifetime()
	app.WithLifetime(lifetime)
	safego.Go("core.runWakeups", func() { app.RunWakeups(lifetime) })
	supervisor := newSupervisor(ctx, app, osmGeo, daemon.set)
	server := api.New(api.Deps{
		Core:       app,
		Automation: automationService,
		Storage:    storageModule.Store(),
		Realtime:   realtimeVoice,
		Modules:    daemon.api,
		Skills:     skillsModule.Service(),
		Setup:      bootstrap.SetupService,
		Reload:     supervisor.Reload,
		Restart:    supervisor.RestartDaemon,
		Stop:       supervisor.StopDaemon,
		APIToken:   bootstrap.APIToken,
	})
	app.WithRuntimeStatusContext(supervisor)
	commandEnv := commandEnvironment(bootstrap.Setup, supervisor.appliedConfig)
	app.WithCommandEnvironment(commandEnv)
	// The agent's file tools may not trust a project's hooks for the owner.
	hooksTrust := filepath.Join(dataDir(bootstrap.DBPath), "hooks-trust.json")
	tools.ProtectCredentials(func() []string { return []string{hooksTrust} })
	app.WithHooks(hooks.NewRunner(commandEnv, func() hooks.Config {
		cfg := supervisor.appliedConfig()
		return hooks.Config{Hooks: cfg.Hooks, Diagnostics: cfg.Diagnostics}
	}, hooksTrust))
	supervisor.SetExternalAgents(sqliteStore, externalRuntimes, bootstrap.Setup.Modules.ExternalAgents)
	defer func() {
		// Ending the lifetime interrupts the executing runs and keeps them for
		// recovery; they finish writing, and background commands are stopped,
		// before modules, external agents and the store close.
		stopLifetime()
		app.WaitRuns()
		app.StopShellTasks()
		supervisor.CloseExternalAgents()
		_ = daemon.set.Close()
	}()
	httpServer := &http.Server{
		Handler:           server.Handler(),
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	// Requests wait on the bound listener until the setup is applied, so no
	// run starts without the module tools.
	listener, err := net.Listen("tcp", bootstrap.Addr)
	if err != nil {
		return err
	}
	// A setup part that fails to apply must not keep the daemon, and with it
	// the clients that could fix the setup, from starting.
	if err := supervisor.ApplyBootstrap(bootstrap); err != nil {
		log.Printf("matrixclawd apply setup: %v", err)
	}
	errCh := make(chan error, 2)
	safego.Go("daemon.httpServer", func() {
		err := httpServer.Serve(listener)
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	})
	safego.Go("automation.Run", func() { automationService.Run(ctx) })
	safego.Go("core.releaseHeldDeliveries", func() {
		if err := app.ReleaseHeldClientDeliveries(context.Background()); err != nil {
			log.Printf("matrixclawd release held deliveries: %v", err)
		}
	})
	// External agents are configured by now, so recovered runs find theirs.
	safego.Go("core.recover", func() {
		if err := app.Recover(context.Background()); err != nil {
			log.Printf("matrixclawd recovery: %v", err)
		}
	})

	log.Printf("matrixclawd bootstrap: setup=%s", bootstrap.SetupPath)
	log.Printf("matrixclawd listening on %s using %s", bootstrap.Addr, bootstrap.DBPath)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// loginShellTimeout bounds the one login shell that reads the profile's
// environment for commands.
const loginShellTimeout = 10 * time.Second

// commandEnvironment is the environment of the agent's shell commands: the
// login environment, read once at the first command, without the keys and
// daemon variables of the setup applied at that time.
func commandEnvironment(boot setup.Config, applied func() setup.Config) func() []string {
	login := sync.OnceValue(func() []string {
		env, err := shelltask.LoginEnvironment(setup.ChildEnvironment(boot, os.Environ()), loginShellTimeout)
		if err != nil {
			log.Printf("matrixclawd: read the login shell environment, commands get the daemon's: %v", err)
			return os.Environ()
		}
		return env
	})
	return func() []string { return setup.ChildEnvironment(applied(), login()) }
}
