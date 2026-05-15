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
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	goworkflows "github.com/Suren878/matrixclaw/internal/orchestration/go_workflows"
	"github.com/Suren878/matrixclaw/internal/setup"
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
	defer sqliteStore.Close()
	automationStore, err := automation.NewSQLiteStore(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer automationStore.Close()

	storageModule, err := localstorage.New(localstorage.Config{
		Root: defaultStorageRoot(bootstrap.DBPath),
	})
	if err != nil {
		return err
	}
	moduleRegistry := modules.NewRegistry(storageModule)
	assistant := bootstrap.Assistant
	assistant.SystemPrompt = appendModuleContext(assistant.SystemPrompt, moduleRegistry.Context())

	app := core.New(sqliteStore).
		WithSessionLLMs(bootstrap.SessionLLMs).
		WithAttachmentReader(storageAttachmentReader{store: storageModule.Store()})
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
	defer runStarter.Close()

	app.WithRunStarter(runStarter)
	automationService := automation.NewService(automationStore, app, bootstrap.Timezone)
	toolRegistry := tools.NewCoreCodingRegistry(
		automation.NewReminderTool(automationService),
		automation.NewScheduledAITaskTool(automationService),
	)
	if err := toolRegistry.Register(core.PlanToolExecutors(app)...); err != nil {
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
