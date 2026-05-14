package daemoncmd

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/sessionllm"
	"github.com/Suren878/matrixclaw/internal/setup"
)

const (
	defaultDaemonAddr = "127.0.0.1:8080"
)

var ErrSetupRequired = errors.New("setup is required before starting the daemon")

type bootstrapConfig struct {
	Addr           string
	DBPath         string
	SessionLLMs    core.SessionLLMRegistry
	Assistant      core.AssistantProfile
	SetupService   *setup.Service
	SetupPath      string
	Timezone       string
	APIToken       string
	Clients        map[string]setup.ClientBootstrap
	ExternalAgents setup.ModulesConfig
}

func loadBootstrap() (bootstrapConfig, error) {
	cfg := bootstrapConfig{
		Addr:   defaultDaemonAddr,
		DBPath: setup.DefaultDBPath(),
	}

	service, err := setup.NewDefaultService()
	if err != nil {
		return bootstrapConfig{}, fmt.Errorf("resolve setup service: %w", err)
	}
	cfg.SetupPath = service.Path()
	cfg.SetupService = service

	setupCfg, err := service.Load()
	switch {
	case errors.Is(err, setup.ErrConfigNotFound):
		return bootstrapConfig{}, fmt.Errorf("%w: run `matrixclaw setup` first (%s)", ErrSetupRequired, service.Path())
	case err != nil:
		return bootstrapConfig{}, fmt.Errorf("load setup config %s: %w", service.Path(), err)
	default:
		if strings.TrimSpace(setupCfg.Daemon.APIToken) == "" {
			setupCfg, err = service.EnsureDaemonAPIToken()
			if err != nil {
				return bootstrapConfig{}, fmt.Errorf("initialize daemon api token %s: %w", service.Path(), err)
			}
		}
		if addr := strings.TrimSpace(setupCfg.Daemon.HTTPAddr); addr != "" {
			cfg.Addr = addr
		}
		if dbPath := strings.TrimSpace(setupCfg.Daemon.DBPath); dbPath != "" {
			cfg.DBPath = dbPath
		}
		cfg.Timezone = strings.TrimSpace(setupCfg.Daemon.Timezone)
		cfg.APIToken = strings.TrimSpace(setupCfg.Daemon.APIToken)

		if err := setup.ImportDaemonEnvironmentFile(service.Path(), setupCfg); err != nil {
			return bootstrapConfig{}, fmt.Errorf("load setup daemon environment %s: %w", setup.DaemonEnvironmentFilePath(service.Path()), err)
		}

		if activeProvider, ok := setup.ActiveProviderConfig(setupCfg); ok {
			if _, ok := setup.ProviderConfigWithResolvedAPIKey(activeProvider); !ok {
				return bootstrapConfig{}, fmt.Errorf("load setup config %s: %s API key is required; set api_key or %s", service.Path(), firstNonEmpty(activeProvider.Name, activeProvider.ID, activeProvider.Type), firstNonEmpty(activeProvider.APIKeyEnv, "the provider API key environment variable"))
			}
		}

		cfg.SessionLLMs = sessionllm.New(setupCfg.ActiveProviderID, sessionProviderSpecsFromSetup(setupCfg))
		cfg.Assistant = core.AssistantProfile{
			Name:               setupCfg.Assistant.Name,
			SystemPrompt:       setup.InitializeAssistantSystemPromptForConfig(setupCfg.Assistant.SystemPrompt, setupCfg),
			CustomInstructions: setupCfg.Assistant.CustomInstructions,
		}
		clients, err := setup.ClientBootstrapsFromConfig(setupCfg)
		if err != nil {
			return bootstrapConfig{}, fmt.Errorf("load setup config %s: %w", service.Path(), err)
		}
		cfg.Clients = clients
		cfg.ExternalAgents = setupCfg.Modules
	}

	if addr := strings.TrimSpace(getenv("MATRIXCLAW_HTTP_ADDR", "")); addr != "" {
		cfg.Addr = addr
	}
	if dbPath := strings.TrimSpace(getenv("MATRIXCLAW_DB_PATH", "")); dbPath != "" {
		cfg.DBPath = dbPath
	}
	if timezone := strings.TrimSpace(getenv("MATRIXCLAW_TIMEZONE", "")); timezone != "" {
		cfg.Timezone = timezone
	}
	if token := strings.TrimSpace(getenv("MATRIXCLAW_API_TOKEN", "")); token != "" {
		cfg.APIToken = token
	}
	if strings.TrimSpace(cfg.Timezone) == "" {
		cfg.Timezone = "UTC"
	}
	if !allowRemoteHTTP() && !isLoopbackHTTPAddr(cfg.Addr) {
		return bootstrapConfig{}, fmt.Errorf("refusing to bind daemon API to non-loopback address %q without MATRIXCLAW_ALLOW_REMOTE_HTTP=1", cfg.Addr)
	}
	if !isLoopbackHTTPAddr(cfg.Addr) && strings.TrimSpace(cfg.APIToken) == "" {
		return bootstrapConfig{}, fmt.Errorf("refusing to bind daemon API to non-loopback address %q without MATRIXCLAW_API_TOKEN or setup api_token", cfg.Addr)
	}

	return cfg, nil
}

func sessionProviderSpecsFromSetup(cfg setup.Config) []sessionllm.ProviderSpec {
	specs := make([]sessionllm.ProviderSpec, 0, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		runtimeProvider, _ := setup.ProviderConfigWithResolvedAPIKey(provider)
		specs = append(specs, sessionllm.ProviderSpec{
			ID:              runtimeProvider.ID,
			CatalogID:       runtimeProvider.CatalogID,
			Name:            runtimeProvider.Name,
			Type:            runtimeProvider.Type,
			APIKey:          runtimeProvider.APIKey,
			BaseURL:         runtimeProvider.BaseURL,
			Model:           runtimeProvider.Model,
			MaxOutputTokens: runtimeProvider.MaxOutputTokens,
			ReasoningEffort: runtimeProvider.ReasoningEffort,
			ToolUseMode:     runtimeProvider.ToolUseMode,
		})
	}
	return specs
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func getenv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func allowRemoteHTTP() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MATRIXCLAW_ALLOW_REMOTE_HTTP"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isLoopbackHTTPAddr(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	if strings.Contains(addr, "://") {
		parsed, err := url.Parse(addr)
		if err != nil {
			return false
		}
		addr = parsed.Host
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
