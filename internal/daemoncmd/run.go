package daemoncmd

import (
	"context"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/api"
	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents/builtins"
	"github.com/Suren878/matrixclaw/internal/modules"
	deliverymodule "github.com/Suren878/matrixclaw/internal/modules/delivery"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	mcpmodule "github.com/Suren878/matrixclaw/internal/modules/mcp"
	skillsmodule "github.com/Suren878/matrixclaw/internal/modules/skills"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	telephonymodule "github.com/Suren878/matrixclaw/internal/modules/telephony"
	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/skills"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/webresearch"
	"github.com/Suren878/matrixclaw/internal/webtools"
	"github.com/Suren878/matrixclaw/internal/work"
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
	workStore, err := work.NewSQLiteStore(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = workStore.Close() }()
	webResearchStore := webresearch.NewStore(workStore)

	storageModule, err := localstorage.New(localstorage.Config{
		Root: defaultStorageRoot(bootstrap.DBPath),
	})
	if err != nil {
		return err
	}
	mcpModule, err := mcpmodule.New(ctx, mcpConfigWithBrowser(bootstrap.ExternalAgents))
	if err != nil {
		log.Printf("matrixclawd mcp module disabled: %v", err)
		mcpModule, _ = mcpmodule.New(ctx, setup.MCPConfig{})
	}
	defer func() { _ = mcpModule.Close() }()
	skillsModule, err := skillsmodule.New(skillsConfigFromBootstrap(bootstrap))
	if err != nil {
		return err
	}
	defer func() { _ = skillsModule.Close() }()
	moduleRegistry := modules.NewRegistry(storageModule, mcpModule, skillsModule)

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
		WithRuntimeStatusContext(&setupRuntimeStatusContext{setup: bootstrap.SetupService, runtime: localruntime.New("")})
	// The workflow worker (below) may start executing persisted runs before
	// supervisor.ApplyBootstrap runs, so the profile is set here too, via the
	// same helper, ensuring module context is never missing.
	applyAssistantProfile(app, bootstrap.Assistant, moduleRegistry.Context)
	externalRegistry, externalRuntimes, err := builtins.BuildRegistry(bootstrap.ExternalAgents)
	if err != nil {
		return err
	}
	app.WithExternalAgents(externalRegistry, sqliteStore)
	automationService := automation.NewService(automationStore, app, bootstrap.Timezone).
		WithDeliveryTargets(automationDeliveryTargets(bootstrap))
	webSearchConfig := webSearchProviderConfig(bootstrap.SetupService)
	webResearchEngine := newWebResearchEngine(bootstrap.DBPath, bootstrap.ExternalAgents.MCP, mcpModule, webResearchStore, webSearchConfig)
	webTools := webtools.NewWebService(webSearchConfig, webResearchEngine)
	osmGeo := tools.NewOSMServiceFromEnv()
	extraTools := []tools.Executor{
		automation.NewReminderTool(automationService),
		automation.NewScheduledAITaskTool(automationService),
		deliverymodule.NewSendFileTool(storageModule.Store(), app),
		telephonymodule.NewCallTool(bootstrap.SetupService),
		telephonymodule.NewEndCallTool(bootstrap.SetupService),
		voicemodule.NewTextToSpeechTool(bootstrap.SetupService),
		webtools.NewWebFetchExecutorWithService(webTools),
		webtools.NewWebSearchExecutorWithService(webTools),
	}
	extraTools = append(extraTools, webtools.NewWebResearchExecutorsWithService(webTools)...)
	toolRegistry := tools.NewRegistry(append(tools.CoreExecutors(), extraTools...)...)
	if err := toolRegistry.Register(tools.NewShellExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.TodoToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.AwaitToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.MemoryToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.AgentToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Err(); err != nil {
		return err
	}
	if err := toolRegistry.Register(tools.NewOSMGeoExecutors(osmGeo)...); err != nil {
		return err
	}
	if err := moduleRegistry.RegisterTools(toolRegistry); err != nil {
		return err
	}
	app.WithTools(newSetupAwareToolExecutor(toolRegistry, bootstrap.SetupService))
	// Shell tasks do not survive a restart; this runs before any run can start new ones.
	if err := app.RecoverTasks(ctx); err != nil {
		log.Printf("matrixclawd background task recovery failed: %v", err)
	}
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
	server.SetRealtimeVoiceService(newRealtimeVoiceManager(bootstrap.SetupService, app))
	server.SetMCPChanged(mcpConfigChanged(bootstrap))
	supervisor := newSupervisor(ctx, server, app, osmGeo)
	supervisor.SetModuleContext(moduleRegistry.Context)
	supervisor.SetExternalAgents(sqliteStore, externalRuntimes, bootstrap.ExternalAgents.ExternalAgents)
	defer func() {
		// Ending the lifetime interrupts the executing runs and keeps them for
		// recovery; they finish writing before external agents and the store close.
		stopLifetime()
		app.WaitRuns()
		supervisor.CloseExternalAgents()
	}()
	httpServer := &http.Server{
		Addr:              bootstrap.Addr,
		Handler:           server.Handler(),
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 2)
	safego.Go("daemon.httpServer", func() {
		err := httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	})

	if err := supervisor.ApplyBootstrap(bootstrap); err != nil {
		return err
	}
	startConfiguredVoiceRuntimes(ctx, bootstrap.SetupService)
	safego.Go("automation.Run", func() { automationService.Run(ctx) })
	safego.Go("webresearch.Run", func() { webResearchEngine.Start(ctx) })
	safego.Go("supervisor.deliverStartupNotifications", func() {
		supervisor.DeliverPendingStartupNotifications(bootstrap)
	})
	safego.Go("core.recoverState", func() {
		if err := app.RecoverActiveRuns(context.Background()); err != nil {
			log.Printf("matrixclawd active run recovery failed: %v", err)
		}
		if err := app.RecoverSessionInputs(context.Background()); err != nil {
			log.Printf("matrixclawd session input recovery failed: %v", err)
		}
		if err := app.RecoverSubagentTasks(context.Background()); err != nil {
			log.Printf("matrixclawd subagent recovery failed: %v", err)
		}
		if err := app.RecoverTaskEvents(context.Background()); err != nil {
			log.Printf("matrixclawd background task event recovery failed: %v", err)
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
	cfg := bootstrap.ExternalAgents.Skills
	return skills.Config{
		DBPath:      bootstrap.DBPath,
		Enabled:     cfg.IsEnabled(),
		AutoInvoke:  cfg.IsAutoInvoke(),
		TrustPolicy: cfg.TrustPolicy,
		SelfImprove: cfg.SelfImprove,
	}
}

func mcpConfigWithBrowser(modules setup.ModulesConfig) setup.MCPConfig {
	cfg := modules.MCP
	browserModule := setup.BrowserModuleFromConfig(modules)
	if !browserModule.Enabled {
		return cfg
	}
	for _, provider := range browserModule.Providers {
		if provider.ID != browserModule.ProviderID {
			continue
		}
		if server, ok := localruntime.New("").PlaywrightMCPServerConfig(provider); ok {
			cfg.Enabled = true
			cfg.Servers = appendOrReplaceMCPServer(cfg.Servers, server)
		}
		return cfg
	}
	return cfg
}

// mcpConfigChanged reports whether the saved MCP and browser settings differ
// from those the MCP module was built with at startup.
func mcpConfigChanged(bootstrap bootstrapConfig) func() bool {
	started := mcpConfigWithBrowser(bootstrap.ExternalAgents)
	return func() bool {
		cfg, err := bootstrap.SetupService.Load()
		return err == nil && !reflect.DeepEqual(mcpConfigWithBrowser(cfg.Modules), started)
	}
}

func appendOrReplaceMCPServer(servers []setup.MCPServerConfig, server setup.MCPServerConfig) []setup.MCPServerConfig {
	out := append([]setup.MCPServerConfig(nil), servers...)
	for i := range out {
		if out[i].ID == server.ID {
			out[i] = server
			return out
		}
	}
	return append(out, server)
}

func startConfiguredVoiceRuntimes(ctx context.Context, service *setup.Service) {
	if service == nil {
		return
	}
	modules, err := service.VoiceModules()
	if err != nil {
		log.Printf("voice runtime bootstrap skipped: %s", err)
		return
	}
	runtime := localruntime.New("")
	for _, module := range modules {
		if !module.Enabled {
			continue
		}
		for _, provider := range module.Providers {
			if provider.ID != module.ProviderID || !provider.Local || (provider.ID != "piper" && provider.ID != "supertonic" && provider.ID != "whispercpp") {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(provider.Config.RuntimeMode), "always_running") {
				continue
			}
			if _, err := runtime.ApplyVoiceAction(ctx, module.ID, provider, setup.VoiceProviderActionRequest{Action: localruntime.ActionStart}); err != nil {
				log.Printf("%s %s runtime autostart failed: %s", module.ID, provider.ID, err)
			}
		}
	}
}
