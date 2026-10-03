package daemoncmd

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Suren878/matrixclaw/internal/api"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/externalagents/builtins"
	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/modules/geo"
	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/setup"
)

const (
	daemonSystemdService = "matrixclawd.service"
	daemonRestartTimeout = 25 * time.Second
	daemonRestartText    = "Daemon restarted."
)

type supervisor struct {
	ctx      context.Context
	server   *api.Server
	app      *core.Core
	telegram *telegramClientAdapter
	modules  *modules.Set
	applied  atomic.Pointer[setup.Config]

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

func newSupervisor(ctx context.Context, server *api.Server, app *core.Core, geo *geo.OSMService, set *modules.Set) *supervisor {
	s := &supervisor{
		ctx:      ctx,
		server:   server,
		app:      app,
		modules:  set,
		telegram: &telegramClientAdapter{geo: geo},
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

// applyBootstrap makes the daemon follow the loaded setup; each part does
// nothing when its own settings did not change.
func (s *supervisor) applyBootstrap(bootstrap bootstrapConfig) error {
	cfg := bootstrap.Setup
	s.applied.Store(&cfg)
	var errs []error
	if s.modules != nil {
		if err := s.modules.Apply(s.ctx, cfg); err != nil {
			errs = append(errs, err)
		}
	}
	if s.app != nil {
		s.app.SetSessionLLMs(bootstrap.SessionLLMs)
		var moduleContext func() []string
		if s.modules != nil {
			moduleContext = s.modules.Context
		}
		applyAssistantProfile(s.app, bootstrap.Assistant, moduleContext)
	}
	if err := s.telegram.Apply(s.ctx, bootstrap); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// appliedConfig is the setup the daemon follows now.
func (s *supervisor) appliedConfig() setup.Config {
	if cfg := s.applied.Load(); cfg != nil {
		return *cfg
	}
	return setup.Config{}
}

func (s *supervisor) Reload(ctx context.Context) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	bootstrap, err := loadBootstrap()
	if err != nil {
		return err
	}
	if err := s.applyExternalAgents(bootstrap.Setup.Modules); err != nil {
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

// saveRestartDelivery holds the restart notice for the requester until the
// restarted daemon releases it.
func (s *supervisor) saveRestartDelivery(ctx context.Context, req core.AdminRestartRequest) (core.ClientDelivery, error) {
	if s.app == nil || req.Notification == nil {
		return core.ClientDelivery{}, nil
	}
	notification := req.Notification
	client := strings.TrimSpace(notification.Client)
	if client == "" {
		return core.ClientDelivery{}, nil
	}
	payload, err := json.Marshal(core.NoticeDeliveryPayload{Replace: true})
	if err != nil {
		return core.ClientDelivery{}, err
	}
	return s.app.CreateClientDelivery(ctx, core.ClientDelivery{
		Type:        core.ClientDeliveryTypeNotice,
		Status:      core.ClientDeliveryStatusHeld,
		Client:      client,
		ExternalKey: strings.TrimSpace(notification.ExternalKey),
		SessionID:   strings.TrimSpace(notification.SessionID),
		RunID:       strings.TrimSpace(notification.RunID),
		TaskID:      strings.TrimSpace(notification.TaskID),
		Summary:     cmp.Or(strings.TrimSpace(notification.Summary), daemonRestartText),
		Address:     notification.Address,
		Payload:     payload,
	})
}
