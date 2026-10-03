package daemoncmd

import (
	"context"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Suren878/matrixclaw/internal/api"
	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents/builtins"
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
	"github.com/Suren878/matrixclaw/internal/skills"
	"github.com/Suren878/matrixclaw/internal/store"
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

	removeWebResearchFiles(bootstrap.DBPath)
	localRuntime := localruntime.New("")
	defer func() { _ = localRuntime.Close() }()
	storageModule, err := localstorage.New(localstorage.Config{
		Root: defaultStorageRoot(bootstrap.DBPath),
	})
	if err != nil {
		return err
	}
	skillsModule, err := skillsmodule.New(skillsConfigFromBootstrap(bootstrap))
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
		WithSkillsContext(skillsModule)
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
	server := api.New(app)
	server.SetAPIToken(bootstrap.APIToken)
	server.SetAutomationService(automationService)
	server.SetStorageStore(storageModule.Store())
	server.SetSkillsService(skillsModule.Service())
	server.SetSetupService(bootstrap.SetupService)
	server.SetModules(daemon.api)
	server.SetRealtimeVoiceService(realtimeVoice)
	supervisor := newSupervisor(ctx, server, app, osmGeo, daemon.set)
	app.WithRuntimeStatusContext(supervisor)
	supervisor.SetExternalAgents(sqliteStore, externalRuntimes, bootstrap.Setup.Modules.ExternalAgents)
	defer func() {
		// Ending the lifetime interrupts the executing runs and keeps them for
		// recovery; they finish writing before modules, external agents and
		// the store close.
		stopLifetime()
		app.WaitRuns()
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
	if err := supervisor.ApplyBootstrap(bootstrap); err != nil {
		_ = listener.Close()
		return err
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
	safego.Go("supervisor.deliverStartupNotifications", func() {
		supervisor.DeliverPendingStartupNotifications(bootstrap)
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

func skillsConfigFromBootstrap(bootstrap bootstrapConfig) skills.Config {
	cfg := bootstrap.Setup.Modules.Skills
	return skills.Config{
		DBPath:      bootstrap.DBPath,
		Enabled:     cfg.IsEnabled(),
		AutoInvoke:  cfg.IsAutoInvoke(),
		TrustPolicy: cfg.TrustPolicy,
	}
}
