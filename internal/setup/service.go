package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Suren878/matrixclaw/internal/providers"
	providerdiscovery "github.com/Suren878/matrixclaw/internal/providers/discovery"
)

type Service struct {
	mu               sync.Mutex
	store            *FileStore
	daemonManager    daemonManager
	telegramValidate telegramValidator
}

func NewService(store *FileStore) *Service {
	return &Service{
		store:            store,
		daemonManager:    newSystemdUserDaemonManager(),
		telegramValidate: newTelegramHTTPValidator(),
	}
}

func NewDefaultService() (*Service, error) {
	path, err := DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	return NewService(NewFileStore(path)), nil
}

func DefaultConfigPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_SETUP_PATH")); value != "" {
		return value, nil
	}

	cfgDir, err := os.UserConfigDir()
	if err != nil {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", fmt.Errorf("resolve config dir: %w", err)
		}
		cfgDir = filepath.Join(home, ".config")
	}
	return filepath.Join(cfgDir, "matrixclaw", "setup.json"), nil
}

func (s *Service) Path() string {
	return s.store.Path()
}

func (s *Service) Load() (Config, error) {
	return s.store.Load()
}

// Update applies change to the saved config, validates the result and saves
// it under the service lock, so concurrent edits don't overwrite each other.
func (s *Service) Update(change func(*Config) error) (Config, error) {
	return s.update(change, false)
}

// update is Update; with create it starts from an empty config when there is
// no usable file yet.
func (s *Service) update(change func(*Config) error, create bool) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.store.Load()
	if create && (errors.Is(err, ErrConfigNotFound) || errors.Is(err, ErrUnsupportedConfigVersion)) {
		cfg, err = Config{Version: CurrentVersion}, nil
	}
	if err != nil {
		return Config{}, err
	}
	if err := change(&cfg); err != nil {
		return Config{}, err
	}
	cfg = normalizeConfig(cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	if err := s.store.Save(cfg); err != nil {
		return Config{}, err
	}
	return s.store.Load()
}

// EditableConfig is the saved config, or a new one with local defaults when
// none is saved yet; existing reports which.
func (s *Service) EditableConfig() (cfg Config, existing bool, err error) {
	cfg, err = s.Load()
	switch {
	case err == nil:
		return cfg, true, nil
	case errors.Is(err, ErrConfigNotFound), errors.Is(err, ErrUnsupportedConfigVersion):
		return NewConfig(), false, nil
	default:
		return Config{}, false, err
	}
}

// NewConfig is a first-run config with this machine's defaults.
func NewConfig() Config {
	return Config{
		Version: CurrentVersion,
		Daemon: DaemonConfig{
			HTTPAddr: defaultHTTPAddr(),
			DBPath:   defaultDBPath(),
			Timezone: defaultTimezone(),
		},
	}
}

func (s *Service) AllowProviderSetupForClient(client string) (bool, error) {
	client = strings.ToLower(strings.TrimSpace(client))
	if client != "telegram" {
		return true, nil
	}
	cfg, err := s.Load()
	if err != nil {
		return false, err
	}
	return cfg.Clients.Telegram.AllowProviderSetup, nil
}

func (s *Service) IsConfigured() (bool, error) {
	_, err := s.Load()
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrConfigNotFound) || errors.Is(err, ErrUnsupportedConfigVersion) {
		return false, nil
	}
	return false, err
}

func (s *Service) ProviderItems() ([]ProviderSetupItem, error) {
	cfg, err := s.Load()
	if err != nil {
		return nil, err
	}
	return ProviderItems(cfg), nil
}

// ConfigureProvider applies update to the provider, adding it when it is not
// configured yet; a new provider, or update.Active, makes it the active one.
func (s *Service) ConfigureProvider(providerID string, update ProviderSetupUpdate) (ProviderSetupItem, error) {
	var savedID string
	cfg, err := s.Update(func(cfg *Config) error {
		provider, exists := cfg.Provider(providerID)
		if !exists {
			var err error
			provider, err = NewProviderConfig(providerID, deref(update.Type), deref(update.Name))
			if err != nil {
				return err
			}
		}
		if err := applyProviderUpdate(&provider, update); err != nil {
			return err
		}
		if err := CheckProvider(provider); err != nil {
			return err
		}
		cfg.SetProvider(provider)
		if cfg.ActiveProviderID == "" || update.Active || !exists {
			cfg.ActiveProviderID = provider.ID
		}
		savedID = provider.ID
		return nil
	})
	if err != nil {
		return ProviderSetupItem{}, err
	}
	for _, item := range ProviderItems(cfg) {
		if item.Configured && sameProvider(item.ID, savedID) {
			return item, nil
		}
	}
	return ProviderSetupItem{}, fmt.Errorf("provider %q was not saved", savedID)
}

// DeleteProvider removes a custom provider; built-in ones stay.
func (s *Service) DeleteProvider(providerID string) error {
	_, err := s.Update(func(cfg *Config) error {
		provider, ok := cfg.Provider(providerID)
		if !ok {
			return fmt.Errorf("provider %q was not found", providerID)
		}
		if provider.policy().Known {
			return fmt.Errorf("provider %q is built in and cannot be deleted here", provider.Effective().Name)
		}
		cfg.removeProvider(provider.ID)
		return nil
	})
	return err
}

// ProviderModelCatalogFor lists the models of a provider as update would
// leave it, without saving anything.
func (s *Service) ProviderModelCatalogFor(ctx context.Context, providerID string, update ProviderSetupUpdate) (ProviderModelsResponse, error) {
	cfg, err := s.Load()
	if err != nil && !errors.Is(err, ErrConfigNotFound) {
		return ProviderModelsResponse{}, err
	}
	provider, ok := cfg.Provider(providerID)
	if !ok {
		provider, err = NewProviderConfig(providerID, deref(update.Type), deref(update.Name))
		if err != nil {
			return ProviderModelsResponse{}, err
		}
	}
	if err := applyProviderUpdate(&provider, update); err != nil {
		return ProviderModelsResponse{}, err
	}
	return ProviderModelCatalog(ctx, provider), nil
}

// ProviderModelCatalog asks the provider for its models with its stored or
// environment API key.
func ProviderModelCatalog(ctx context.Context, provider ProviderConfig) ProviderModelsResponse {
	runtime, hasKey := provider.Runtime()
	policy := provider.policy()
	if !providers.ResolveModelCapabilities(providers.ModelCapabilityInput{ProviderID: runtime.ID, ProviderType: runtime.Type, ModelID: runtime.Model}).ProviderCapabilities.ModelDiscovery {
		return ProviderModelsResponse{
			Status:      ProviderModelStatusUnsupported,
			Source:      ProviderModelSourceManual,
			Message:     runtime.Name + " does not support model discovery",
			ManualInput: true,
		}
	}
	if !hasKey && !policy.PublicModelCatalog {
		return ProviderModelsResponse{
			Status:         ProviderModelStatusRequiresKey,
			Source:         ProviderModelSourceManual,
			Message:        "API key required",
			RequiresAPIKey: true,
		}
	}
	source := providerModelCatalogSource(policy, runtime.APIKey)
	models, err := providerdiscovery.Models(ctx, providerdiscovery.ModelDiscoveryInput{
		ID:        runtime.ID,
		CatalogID: runtime.ID,
		Type:      runtime.Type,
		BaseURL:   runtime.BaseURL,
		APIKey:    runtime.APIKey,
		Model:     runtime.Model,
	})
	if err != nil {
		status := ProviderModelStatusUnavailable
		if isProviderModelAuthError(err) {
			status = ProviderModelStatusAuthError
		}
		return ProviderModelsResponse{
			Status:         status,
			Source:         source,
			Message:        "Could not load remote models: " + err.Error(),
			RequiresAPIKey: policy.RequiresAPIKey,
			ManualInput:    !policy.Known && status != ProviderModelStatusAuthError,
		}
	}
	if len(models) == 0 {
		return ProviderModelsResponse{
			Status:         ProviderModelStatusUnavailable,
			Source:         source,
			Message:        "No models available",
			RequiresAPIKey: policy.RequiresAPIKey,
			ManualInput:    !policy.Known,
		}
	}
	return ProviderModelsResponse{
		Models:         models,
		Metadata:       providerCatalogMetadata(runtime.ID, runtime.Type, models),
		Status:         ProviderModelStatusOK,
		Source:         source,
		RequiresAPIKey: policy.RequiresAPIKey,
	}
}

func providerCatalogMetadata(providerID string, providerType string, models []string) []providers.ModelMetadata {
	metadata := make([]providers.ModelMetadata, 0, len(models))
	for _, model := range models {
		item := providers.ResolveModelMetadata(providerID, providerType, model)
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		metadata = append(metadata, item)
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func providerModelCatalogSource(policy providers.ProviderPolicy, apiKey string) string {
	if policy.PublicModelCatalog {
		return ProviderModelSourcePublicCatalog
	}
	if strings.TrimSpace(apiKey) != "" {
		return ProviderModelSourceConfiguredKey
	}
	return ProviderModelSourceLiveCatalog
}

func isProviderModelAuthError(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"401", "403", "unauthorized", "forbidden", "invalid api key", "incorrect api key", "permission"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// Apply saves the sections the setup wizard edits (assistant, providers,
// daemon address, paths, timezone and autostart, clients) over the current
// file, after checking the Telegram token, and installs and starts the daemon.
func (s *Service) Apply(ctx context.Context, edited Config) (ApplyResult, error) {
	edited = normalizeConfig(edited)
	if err := edited.Validate(); err != nil {
		return ApplyResult{}, err
	}
	telegramSummary, err := s.validateTelegram(ctx, edited.Clients.Telegram)
	if err != nil {
		return ApplyResult{}, err
	}
	cfg, err := s.update(func(cfg *Config) error {
		cfg.Assistant = edited.Assistant
		cfg.Providers = edited.Providers
		cfg.ActiveProviderID = edited.ActiveProviderID
		cfg.Daemon.HTTPAddr = edited.Daemon.HTTPAddr
		cfg.Daemon.DBPath = edited.Daemon.DBPath
		cfg.Daemon.Timezone = edited.Daemon.Timezone
		cfg.Daemon.AutostartOnBoot = edited.Daemon.AutostartOnBoot
		cfg.Clients = edited.Clients
		token, err := existingOrNewAPIToken(cfg.Daemon.APIToken)
		if err != nil {
			return fmt.Errorf("generate api token: %w", err)
		}
		cfg.Daemon.APIToken = token
		return nil
	}, true)
	if err != nil {
		return ApplyResult{}, err
	}

	result := ApplyResult{Config: cfg, Path: s.store.Path()}
	result.Summary = SummaryFromConfig(cfg)
	if cfg.Clients.Telegram.Enabled {
		result.Summary.Telegram = telegramSummary
	}
	if s.daemonManager != nil {
		daemonSummary, warnings, applyErr := s.daemonManager.Apply(ctx, s.store.Path(), cfg)
		result.Warnings = append(result.Warnings, warnings...)
		result.Summary.Daemon = daemonSummary
		if applyErr != nil {
			return result, applyErr
		}
	}
	return result, nil
}

func (s *Service) validateTelegram(ctx context.Context, telegram TelegramConfig) (TelegramSummary, error) {
	if s.telegramValidate == nil || !telegram.Enabled {
		return TelegramSummary{}, nil
	}
	return s.telegramValidate.Validate(ctx, telegram.BotToken)
}

func (s *Service) Summary() (Summary, error) {
	return s.SummaryContext(context.Background())
}

func (s *Service) SummaryContext(ctx context.Context) (Summary, error) {
	cfg, err := s.Load()
	if err != nil {
		return Summary{}, err
	}
	summary := SummaryFromConfig(cfg)
	if s.daemonManager != nil {
		daemonSummary, inspectErr := s.daemonManager.Inspect(ctx, s.store.Path(), cfg)
		if inspectErr != nil {
			return Summary{}, inspectErr
		}
		summary.Daemon = daemonSummary
	}
	return summary, nil
}

func (s *Service) EnsureDaemonContext(ctx context.Context) (DaemonSummary, error) {
	cfg, err := s.Load()
	if err != nil {
		return DaemonSummary{}, err
	}
	summary := daemonConfiguredSummary(cfg)
	if s.daemonManager == nil {
		return summary, nil
	}

	inspected, inspectErr := s.daemonManager.Inspect(ctx, s.store.Path(), cfg)
	if inspectErr == nil && inspected.Running {
		return inspected, nil
	}

	applied, _, applyErr := s.daemonManager.Apply(ctx, s.store.Path(), cfg)
	if applyErr != nil {
		if inspectErr != nil {
			return applied, fmt.Errorf("%w; %w", inspectErr, applyErr)
		}
		return applied, applyErr
	}
	return applied, nil
}

func (s *Service) RestartDaemonContext(ctx context.Context) (DaemonSummary, error) {
	cfg, err := s.Load()
	if err != nil {
		return DaemonSummary{}, err
	}
	if s.daemonManager == nil {
		return daemonConfiguredSummary(cfg), nil
	}
	return s.daemonManager.Restart(ctx, s.store.Path(), cfg)
}

func (s *Service) StopDaemonContext(ctx context.Context) (DaemonSummary, error) {
	cfg, err := s.Load()
	if err != nil {
		return DaemonSummary{}, err
	}
	if s.daemonManager == nil {
		return daemonConfiguredSummary(cfg), nil
	}
	return s.daemonManager.Stop(ctx, s.store.Path(), cfg)
}

func deref[T any](value *T) T {
	var zero T
	if value == nil {
		return zero
	}
	return *value
}
