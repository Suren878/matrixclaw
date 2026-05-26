package daemoncmd

import (
	"context"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/api"
	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents/builtins"
	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	mcpmodule "github.com/Suren878/matrixclaw/internal/modules/mcp"
	skillsmodule "github.com/Suren878/matrixclaw/internal/modules/skills"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
	goworkflows "github.com/Suren878/matrixclaw/internal/orchestration/go_workflows"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/skills"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func Run(ctx context.Context) error {
	bootstrap, err := loadBootstrap()
	if err != nil {
		return err
	}

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

	storageModule, err := localstorage.New(localstorage.Config{
		Root: defaultStorageRoot(bootstrap.DBPath),
	})
	if err != nil {
		return err
	}
	mcpModule, err := mcpmodule.New(ctx, bootstrap.ExternalAgents.MCP)
	if err != nil {
		return err
	}
	defer func() { _ = mcpModule.Close() }()
	skillsModule, err := skillsmodule.New(skillsConfigFromBootstrap(bootstrap))
	if err != nil {
		return err
	}
	defer func() { _ = skillsModule.Close() }()
	moduleRegistry := modules.NewRegistry(storageModule, mcpModule, skillsModule)
	assistant := bootstrap.Assistant
	assistant.SystemPrompt = appendModuleContext(assistant.SystemPrompt, moduleRegistry.Context())

	app := core.New(sqliteStore).
		WithSessionLLMs(bootstrap.SessionLLMs).
		WithAttachmentReader(storageAttachmentReader{store: storageModule.Store()}).
		WithSkillsContext(skillsModule)
	app.SetAssistantProfile(assistant)
	externalRegistry, externalRuntimes, err := builtins.BuildRegistry(bootstrap.ExternalAgents)
	if err != nil {
		return err
	}
	app.WithExternalAgents(externalRegistry, sqliteStore)
	runStarter, err := goworkflows.New(bootstrap.DBPath, app)
	if err != nil {
		return err
	}
	defer func() { _ = runStarter.Close() }()

	app.WithRunStarter(runStarter)
	automationService := automation.NewService(automationStore, app, bootstrap.Timezone).
		WithDeliveryTargets(automationDeliveryTargets(bootstrap))
	toolRegistry := tools.NewCoreCodingRegistry(
		automation.NewReminderTool(automationService),
		automation.NewScheduledAITaskTool(automationService),
		voicemodule.NewTextToSpeechTool(bootstrap.SetupService),
		tools.NewWebSearchExecutor(func() (tools.WebSearchProviderConfig, error) {
			if bootstrap.SetupService == nil {
				return tools.WebSearchProviderConfig{}, nil
			}
			cfg, err := bootstrap.SetupService.GetWebSearchConfig()
			if err != nil {
				return tools.WebSearchProviderConfig{}, err
			}
			return tools.WebSearchProviderConfig{
				Provider:  cfg.Provider,
				TavilyKey: cfg.TavilyKey,
				SerperKey: cfg.SerperKey,
				BaseURL:   cfg.BaseURL,
			}, nil
		}),
	)
	if err := toolRegistry.Register(core.PlanToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.MemoryToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.DelegateTaskToolExecutor(app)); err != nil {
		return err
	}
	if err := toolRegistry.Err(); err != nil {
		return err
	}
	if err := moduleRegistry.RegisterTools(toolRegistry); err != nil {
		return err
	}
	app.WithTools(toolRegistry)
	server := api.New(app)
	server.SetAPIToken(bootstrap.APIToken)
	server.SetAutomationService(automationService)
	server.SetStorageStore(storageModule.Store())
	server.SetSkillsService(skillsModule.Service())
	server.SetSetupService(bootstrap.SetupService)
	supervisor := newSupervisor(ctx, server, app)
	supervisor.SetExternalAgents(sqliteStore, externalRuntimes)
	defer supervisor.CloseExternalAgents()
	httpServer := &http.Server{
		Addr:              bootstrap.Addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 2)
	go func() {
		err := httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	}()

	if err := supervisor.ApplyBootstrap(bootstrap); err != nil {
		return err
	}
	startConfiguredVoiceRuntimes(ctx, bootstrap.SetupService)
	go automationService.Run(ctx)
	go supervisor.DeliverPendingStartupNotifications(bootstrap)

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
		Enabled:     cfg.Enabled,
		AutoInvoke:  cfg.AutoInvoke,
		TrustPolicy: cfg.TrustPolicy,
		SelfImprove: cfg.SelfImprove,
	}
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

type storageAttachmentReader struct {
	store *localstorage.LocalStore
}

func (r storageAttachmentReader) ReadAttachment(ctx context.Context, storagePath string, temporary bool, maxBytes int64) (core.AttachmentData, error) {
	if r.store == nil {
		return core.AttachmentData{}, localstorage.ErrInvalidPath
	}
	if temporary {
		entry, data, err := r.store.ReadTemporaryBytes(storagePath)
		if err != nil {
			return core.AttachmentData{}, err
		}
		return core.AttachmentData{
			Data:     data,
			MIMEType: entry.MIMEType,
			Name:     entry.Title,
			Size:     entry.Size,
		}, nil
	}
	entry, data, err := r.store.ReadBytes(storagePath, maxBytes)
	if err != nil {
		return core.AttachmentData{}, err
	}
	return core.AttachmentData{
		Data:     data,
		MIMEType: entry.MIMEType,
		Name:     entry.Title,
		Size:     entry.Size,
	}, nil
}

func defaultStorageRoot(dbPath string) string {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		dbPath = setup.DefaultDBPath()
	}
	if abs, err := filepath.Abs(dbPath); err == nil {
		dbPath = abs
	}
	return filepath.Join(filepath.Dir(dbPath), "storage")
}

func appendModuleContext(systemPrompt string, contexts []string) string {
	if len(contexts) == 0 {
		return strings.TrimSpace(systemPrompt)
	}
	context := strings.TrimSpace(strings.Join(contexts, "\n"))
	if context == "" {
		return strings.TrimSpace(systemPrompt)
	}
	systemPrompt = strings.TrimSpace(systemPrompt)
	if systemPrompt == "" {
		return "Enabled modules:\n" + context
	}
	return systemPrompt + "\n\nEnabled modules:\n" + context
}

func daemonBaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	return "http://" + addr
}
