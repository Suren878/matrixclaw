package daemoncmd

import (
	"context"
	"fmt"
	"log"
	"maps"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/api"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/externalagents/builtins"
	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const (
	daemonSystemdService = "matrixclawd.service"
	daemonRestartTimeout = 25 * time.Second
	daemonRestartText    = "Daemon restarted."
)

type supervisor struct {
	ctx           context.Context
	server        *api.Server
	app           *core.Core
	clients       *clientRegistry
	moduleContext func() []string

	// reloadMu serializes applying the setup and swapping external agents.
	reloadMu         sync.Mutex
	externalStore    externalagents.AttachmentStore
	externalRuntimes []externalagents.RuntimeAgent
	externalConfig   map[string]setup.ExternalAgentConfig

	restartMu  sync.Mutex
	restarting bool
}

// assistantProfileSetter is the minimal seam applyAssistantProfile needs;
// *core.Core satisfies it. Lets tests substitute a fake profile sink.
type assistantProfileSetter interface {
	SetAssistantProfile(core.AssistantProfile)
}

// applyAssistantProfile appends the current module context (storage/MCP/skills)
// to base.SystemPrompt and sets the resulting profile on app.
func applyAssistantProfile(app assistantProfileSetter, base core.AssistantProfile, moduleContext func() []string) {
	if app == nil {
		return
	}
	if moduleContext != nil {
		base.SystemPrompt = appendModuleContext(base.SystemPrompt, moduleContext())
	}
	app.SetAssistantProfile(base)
}

func newSupervisor(ctx context.Context, server *api.Server, app *core.Core, geo *tools.OSMService) *supervisor {
	s := &supervisor{
		ctx:     ctx,
		server:  server,
		app:     app,
		clients: newClientRegistry(geo),
	}
	if server != nil {
		server.SetAdminReload(s.Reload)
		server.SetAdminRestart(s.RestartDaemon)
		server.SetAdminStop(s.StopDaemon)
	}
	return s
}

func (s *supervisor) ApplyBootstrap(bootstrap bootstrapConfig) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	return s.applyBootstrap(bootstrap)
}

func (s *supervisor) applyBootstrap(bootstrap bootstrapConfig) error {
	if s.app != nil {
		s.app.SetSessionLLMs(bootstrap.SessionLLMs)
		applyAssistantProfile(s.app, bootstrap.Assistant, s.moduleContext)
	}
	return s.clients.Apply(s.ctx, bootstrap)
}

// SetModuleContext sets the module-context source (storage/MCP/skills) that
// ApplyBootstrap appends to the assistant profile's system prompt.
func (s *supervisor) SetModuleContext(moduleContext func() []string) {
	s.moduleContext = moduleContext
}

func (s *supervisor) Reload(ctx context.Context) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	bootstrap, err := loadBootstrap()
	if err != nil {
		return err
	}
	if err := s.applyExternalAgents(bootstrap.ExternalAgents); err != nil {
		return err
	}
	return s.applyBootstrap(bootstrap)
}

// applyExternalAgents rebuilds the external agents only when their config
// changed, since rebuilding closes the runtimes live sessions use.
func (s *supervisor) applyExternalAgents(modules setup.ModulesConfig) error {
	if s.app == nil || s.externalStore == nil || maps.Equal(modules.ExternalAgents, s.externalConfig) {
		return nil
	}
	registry, runtimes, err := builtins.BuildRegistry(modules)
	if err != nil {
		return err
	}
	s.app.SetExternalAgents(registry)
	s.replaceExternalRuntimes(runtimes)
	s.externalConfig = maps.Clone(modules.ExternalAgents)
	return nil
}

func (s *supervisor) SetExternalAgents(store externalagents.AttachmentStore, runtimes []externalagents.RuntimeAgent, cfg map[string]setup.ExternalAgentConfig) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	s.externalStore = store
	s.externalRuntimes = append([]externalagents.RuntimeAgent(nil), runtimes...)
	s.externalConfig = maps.Clone(cfg)
}

func (s *supervisor) CloseExternalAgents() {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	s.replaceExternalRuntimes(nil)
}

func (s *supervisor) replaceExternalRuntimes(runtimes []externalagents.RuntimeAgent) {
	old := s.externalRuntimes
	s.externalRuntimes = append([]externalagents.RuntimeAgent(nil), runtimes...)
	for _, runtime := range old {
		if runtime != nil {
			_ = runtime.Close()
		}
	}
}

func (s *supervisor) RestartDaemon(ctx context.Context, req core.AdminRestartRequest) error {
	s.restartMu.Lock()
	if s.restarting {
		s.restartMu.Unlock()
		return nil
	}
	s.restarting = true
	s.restartMu.Unlock()

	delivery, err := s.saveRestartDelivery(ctx, req)
	if err != nil {
		s.restartMu.Lock()
		s.restarting = false
		s.restartMu.Unlock()
		return err
	}

	safego.Go("supervisor.restartDaemon", func() {
		defer func() {
			s.restartMu.Lock()
			s.restarting = false
			s.restartMu.Unlock()
		}()

		time.Sleep(300 * time.Millisecond)
		if err := s.restartSystemdService(context.Background()); err != nil {
			log.Printf("matrixclawd daemon restart failed: %v", err)
			if s.app != nil && delivery.ID != "" {
				if markErr := s.app.MarkClientDeliveryFailed(context.Background(), delivery, err); markErr != nil {
					log.Printf("matrixclawd mark restart delivery failed: %v", markErr)
				}
			}
		}
	})

	return nil
}

func (s *supervisor) restartSystemdService(ctx context.Context) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, daemonRestartTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "systemctl", "--user", "restart", daemonSystemdService)
	output, err := cmd.CombinedOutput()
	message := strings.TrimSpace(string(output))
	if err == nil {
		return nil
	}

	startOutput, startErr := exec.CommandContext(ctx, "systemctl", "--user", "start", daemonSystemdService).CombinedOutput()
	if startErr == nil {
		return nil
	}

	startMsg := strings.TrimSpace(string(startOutput))
	if message == "" {
		message = startMsg
	}
	if message == "" {
		message = "systemctl restart and start both failed"
	}
	if startMsg != "" && startMsg != message {
		message = message + "\n" + startMsg
	}
	return fmt.Errorf("systemctl restart matrixclawd.service failed: %w: %s", err, message)
}

func (s *supervisor) StopDaemon(ctx context.Context) error {
	safego.Go("supervisor.stopDaemon", func() {
		time.Sleep(300 * time.Millisecond)
		if err := s.stopSystemdService(context.Background()); err != nil {
			log.Printf("matrixclawd daemon stop failed: %v", err)
		}
	})
	return nil
}

func (s *supervisor) stopSystemdService(ctx context.Context) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, daemonRestartTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "systemctl", "--user", "stop", daemonSystemdService)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		message = "systemctl stop failed"
	}
	return fmt.Errorf("systemctl stop matrixclawd.service failed: %w: %s", err, message)
}

func (s *supervisor) saveRestartDelivery(ctx context.Context, req core.AdminRestartRequest) (core.ClientDelivery, error) {
	if s.app == nil || req.Notification == nil {
		return core.ClientDelivery{}, nil
	}
	notification := req.Notification
	client := strings.TrimSpace(notification.Client)
	if client == "" {
		return core.ClientDelivery{}, nil
	}
	summary := strings.TrimSpace(notification.Summary)
	if summary == "" {
		summary = daemonRestartText
	}
	address, err := s.clients.NormalizeRestartDeliveryAddress(notification)
	if err != nil {
		return core.ClientDelivery{}, err
	}
	return s.app.CreateClientDelivery(ctx, core.ClientDelivery{
		Type:        core.ClientDeliveryTypeDaemonRestart,
		Client:      client,
		ExternalKey: strings.TrimSpace(notification.ExternalKey),
		SessionID:   strings.TrimSpace(notification.SessionID),
		RunID:       strings.TrimSpace(notification.RunID),
		TaskID:      strings.TrimSpace(notification.TaskID),
		Summary:     summary,
		Address:     address,
	})
}

func (s *supervisor) DeliverPendingStartupNotifications(bootstrap bootstrapConfig) {
	senders := s.clients.RestartDeliverySenders(bootstrap)
	s.markPullClientRestartDeliveriesReady(senders)
	s.deliverPendingRestartNotifications(senders)
}

func (s *supervisor) markPullClientRestartDeliveriesReady(senders []restartDeliverySender) {
	if s.app == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	deliveries, err := s.app.ListClientDeliveries(ctx, core.ClientDeliveryFilter{
		Type:   core.ClientDeliveryTypeDaemonRestart,
		Status: core.ClientDeliveryStatusPending,
		Limit:  100,
	})
	if err != nil {
		log.Printf("matrixclawd delivery recovery failed: %v", err)
		return
	}
	pushClients := map[string]struct{}{}
	for _, sender := range senders {
		if sender == nil {
			continue
		}
		client := strings.TrimSpace(sender.ClientName())
		if client != "" {
			pushClients[client] = struct{}{}
		}
	}
	for _, delivery := range deliveries {
		if _, ok := pushClients[strings.TrimSpace(delivery.Client)]; ok {
			continue
		}
		if err := s.app.MarkClientDeliveryReady(ctx, delivery); err != nil {
			log.Printf("matrixclawd mark delivery %s ready failed: %v", delivery.ID, err)
		}
	}
}

func (s *supervisor) deliverPendingRestartNotifications(senders []restartDeliverySender) {
	if s.app == nil || len(senders) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for _, sender := range senders {
		if sender == nil {
			continue
		}
		clientName := strings.TrimSpace(sender.ClientName())
		if clientName == "" {
			log.Printf("matrixclawd restart delivery sender %T has empty client name", sender)
			continue
		}
		deliveries, err := s.app.ListClientDeliveries(ctx, core.ClientDeliveryFilter{
			Client: clientName,
			Type:   core.ClientDeliveryTypeDaemonRestart,
			Status: core.ClientDeliveryStatusPending,
			Limit:  20,
		})
		if err != nil {
			log.Printf("matrixclawd %s delivery recovery failed: %v", clientName, err)
			continue
		}
		for _, delivery := range deliveries {
			if err := sender.DeliverRestartNotification(ctx, delivery, daemonRestartText); err != nil {
				log.Printf("matrixclawd %s delivery %s failed: %v", clientName, delivery.ID, err)
				if markErr := s.app.MarkClientDeliveryFailed(ctx, delivery, err); markErr != nil {
					log.Printf("matrixclawd mark %s delivery %s failed: %v", clientName, delivery.ID, markErr)
				}
				continue
			}
			if err := s.app.MarkClientDeliverySent(ctx, delivery); err != nil {
				log.Printf("matrixclawd mark %s delivery %s sent failed: %v", clientName, delivery.ID, err)
			}
		}
	}
}
