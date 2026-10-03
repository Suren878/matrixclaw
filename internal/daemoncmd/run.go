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
	"github.com/Suren878/matrixclaw/internal/modules/geo"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	mcpmodule "github.com/Suren878/matrixclaw/internal/modules/mcp"
	skillsmodule "github.com/Suren878/matrixclaw/internal/modules/skills"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	telephonymodule "github.com/Suren878/matrixclaw/internal/modules/telephony"
	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	geminilive "github.com/Suren878/matrixclaw/internal/modules/voice/realtime/providers/gemini"
	grokvoice "github.com/Suren878/matrixclaw/internal/modules/voice/realtime/providers/grok"
	openairealtime "github.com/Suren878/matrixclaw/internal/modules/voice/realtime/providers/openai"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/skills"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/webtools"
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
	voiceService := voicemodule.NewService(bootstrap.SetupService, localRuntime)
	storageModule, err := localstorage.New(localstorage.Config{
		Root: defaultStorageRoot(bootstrap.DBPath),
	})
	if err != nil {
		return err
	}
	mcpModule, err := mcpmodule.New(ctx, mcpConfigWithBrowser(localRuntime, bootstrap.Setup.Modules))
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
		WithRuntimeStatusContext(&setupRuntimeStatusContext{setup: bootstrap.SetupService, runtime: localRuntime})
	// The workflow worker (below) may start executing persisted runs before
	// supervisor.ApplyBootstrap runs, so the profile is set here too, via the
	// same helper, ensuring module context is never missing.
	applyAssistantProfile(app, bootstrap.Assistant, moduleRegistry.Context)
	externalRegistry, externalRuntimes, err := builtins.BuildRegistry(bootstrap.Setup.Modules)
	if err != nil {
		return err
	}
	app.WithExternalAgents(externalRegistry, sqliteStore)
	automationService := automation.NewService(automationStore, app, bootstrap.Timezone).
		WithDeliveryTargets(automationDeliveryTargets(bootstrap))
	osmGeo := geo.NewOSMServiceFromEnv()
	extraTools := []tools.Executor{
		automation.NewReminderTool(automationService),
		automation.NewScheduledAITaskTool(automationService),
		deliverymodule.NewSendFileTool(storageModule.Store(), app),
		telephonymodule.NewCallTool(bootstrap.SetupService),
		telephonymodule.NewEndCallTool(bootstrap.SetupService),
		voicemodule.NewTextToSpeechTool(voiceService),
		webtools.NewFetchTool(),
		webtools.NewSearchTool(webSearchConfig(bootstrap.SetupService)),
	}
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
	if err := toolRegistry.Register(geo.NewOSMGeoExecutors(osmGeo)...); err != nil {
		return err
	}
	if err := moduleRegistry.RegisterTools(toolRegistry); err != nil {
		return err
	}
	app.WithTools(newSetupAwareToolExecutor(toolRegistry, bootstrap.SetupService))
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
	server.SetLocalVoice(localRuntime, voiceService)
	realtimeVoice := realtime.NewManager(app, geminilive.Spec, grokvoice.Spec, openairealtime.Spec)
	server.SetRealtimeVoiceService(realtimeVoice)
	server.SetMCPChanged(mcpConfigChanged(localRuntime, bootstrap))
	supervisor := newSupervisor(ctx, server, app, osmGeo)
	supervisor.realtime = realtimeVoice
	supervisor.SetModuleContext(moduleRegistry.Context)
	supervisor.SetExternalAgents(sqliteStore, externalRuntimes, bootstrap.Setup.Modules.ExternalAgents)
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
	startConfiguredVoiceRuntimes(ctx, bootstrap.SetupService, localRuntime)
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

func mcpConfigWithBrowser(runtime *localruntime.Runtime, modules setup.ModulesConfig) setup.MCPConfig {
	cfg := modules.MCP
	browserModule := setup.BrowserModuleFromConfig(modules)
	if !browserModule.Enabled {
		return cfg
	}
	for _, provider := range browserModule.Providers {
		if provider.ID != browserModule.ProviderID {
			continue
		}
		if server, ok := runtime.PlaywrightMCPServerConfig(provider); ok {
			cfg.Enabled = true
			cfg.Servers = appendOrReplaceMCPServer(cfg.Servers, server)
		}
		return cfg
	}
	return cfg
}

// mcpConfigChanged reports whether the saved MCP and browser settings differ
// from those the MCP module was built with at startup.
func mcpConfigChanged(runtime *localruntime.Runtime, bootstrap bootstrapConfig) func() bool {
	started := mcpConfigWithBrowser(runtime, bootstrap.Setup.Modules)
	return func() bool {
		cfg, err := bootstrap.SetupService.Load()
		return err == nil && !reflect.DeepEqual(mcpConfigWithBrowser(runtime, cfg.Modules), started)
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

func startConfiguredVoiceRuntimes(ctx context.Context, service *setup.Service, runtime *localruntime.Runtime) {
	if service == nil {
		return
	}
	modules, err := service.VoiceModules()
	if err != nil {
		log.Printf("voice runtime bootstrap skipped: %s", err)
		return
	}
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

// webSearchConfig reads the web search provider from setup at each search.
func webSearchConfig(service *setup.Service) func() (webtools.SearchConfig, error) {
	return func() (webtools.SearchConfig, error) {
		cfg, err := service.GetWebSearchConfig()
		return webtools.SearchConfig{Provider: cfg.Provider, TavilyKey: cfg.TavilyKey, SerperKey: cfg.SerperKey, BaseURL: cfg.BaseURL}, err
	}
}
