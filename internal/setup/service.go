package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
	providerdiscovery "github.com/Suren878/matrixclaw/internal/providers/discovery"
)

type Service struct {
	store            Store
	now              func() time.Time
	daemonManager    daemonManager
	telegramValidate telegramValidator
}

func NewService(store Store) *Service {
	return &Service{
		store:            store,
		now:              time.Now,
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

func (s *Service) ProviderOptions() []ProviderOption {
	return builtInProviderOptions()
}

func (s *Service) ProviderSetupItems() ([]ProviderSetupItem, error) {
	draft, err := s.Draft()
	if err != nil {
		return nil, err
	}
	return ProviderSetupItemsFromDraft(draft, s.ProviderOptions()), nil
}

func ProviderSetupItemsFromDraft(draft Draft, options []ProviderOption) []ProviderSetupItem {
	configured := ConfiguredProviders(draft)
	providers := make([]providerItemSource, 0, len(configured))
	for _, provider := range configured {
		providers = append(providers, providerDraftItemSource(provider))
	}
	return providerSetupItems(providers, draft.ActiveProviderID, availableBuiltInProviders(draft, options))
}

type providerItemSource struct {
	id              string
	catalogID       string
	name            string
	providerTyp     string
	capabilities    providers.Capabilities
	requiresBaseURL bool
	baseURL         string
	model           string
	reasoningEffort string
	toolUseMode     providers.ToolUseMode
	contextWindow   int
	apiKeyPreview   string
}

func providerDraftItemSource(provider ProviderDraft) providerItemSource {
	catalogID := firstNonEmptyTrimmed(provider.CatalogID, provider.ID)
	capabilitySet := providers.ResolveModelCapabilities(providers.ModelCapabilityInput{
		ProviderID:   catalogID,
		ProviderType: provider.Type,
		ModelID:      provider.Model,
	})
	return providerItemSource{
		id:              provider.ID,
		catalogID:       provider.CatalogID,
		name:            provider.Name,
		providerTyp:     provider.Type,
		capabilities:    capabilitySet.ProviderCapabilities,
		requiresBaseURL: providers.PolicyForProvider(catalogID, provider.Type).RequiresBaseURL,
		baseURL:         provider.BaseURL,
		model:           provider.Model,
		reasoningEffort: providers.NormalizeReasoningEffortForModel(catalogID, provider.Type, provider.Model, provider.ReasoningEffort),
		toolUseMode:     providers.NormalizeOptionalToolUseMode(provider.ToolUseMode),
		contextWindow:   parsePositiveInt(provider.ContextWindow),
		apiKeyPreview:   currentDraftAPIKeyPreview(provider),
	}
}

func providerConfigItemSource(provider ProviderConfig) providerItemSource {
	catalogID := firstNonEmptyTrimmed(provider.CatalogID, provider.ID)
	capabilitySet := providers.ResolveModelCapabilities(providers.ModelCapabilityInput{
		ProviderID:   catalogID,
		ProviderType: provider.Type,
		ModelID:      provider.Model,
	})
	return providerItemSource{
		id:              provider.ID,
		catalogID:       provider.CatalogID,
		name:            provider.Name,
		providerTyp:     provider.Type,
		capabilities:    capabilitySet.ProviderCapabilities,
		requiresBaseURL: providers.PolicyForProvider(catalogID, provider.Type).RequiresBaseURL,
		baseURL:         provider.BaseURL,
		model:           provider.Model,
		reasoningEffort: providers.NormalizeReasoningEffortForModel(catalogID, provider.Type, provider.Model, provider.ReasoningEffort),
		toolUseMode:     providers.NormalizeOptionalToolUseMode(provider.ToolUseMode),
		contextWindow:   provider.ContextWindow,
		apiKeyPreview:   ProviderAPIKeyPreview(provider),
	}
}

func providerSetupItems(providerSources []providerItemSource, activeProviderID string, options []ProviderOption) []ProviderSetupItem {
	items := make([]ProviderSetupItem, 0, len(providerSources)+len(options))
	seen := make(map[string]struct{}, len(providerSources))
	for _, provider := range providerSources {
		id := strings.TrimSpace(provider.id)
		if id == "" {
			continue
		}
		active := sameProvider(id, activeProviderID)
		if active {
			items = append(items, providerSetupItem(provider, active))
			seen[providers.CanonicalProviderID(id)] = struct{}{}
		}
	}
	for _, provider := range providerSources {
		id := strings.TrimSpace(provider.id)
		if id == "" {
			continue
		}
		seenID := providers.CanonicalProviderID(id)
		if _, ok := seen[seenID]; ok {
			continue
		}
		items = append(items, providerSetupItem(provider, false))
		seen[seenID] = struct{}{}
	}
	for _, option := range options {
		if _, ok := seen[providers.CanonicalProviderID(option.ID)]; ok {
			continue
		}
		items = append(items, providerOptionSetupItem(option))
	}
	return items
}

func providerSetupItem(provider providerItemSource, active bool) ProviderSetupItem {
	status := "Configured"
	if model := strings.TrimSpace(provider.model); model != "" {
		status += " · " + model
	}
	if active {
		status += " · Active"
	}
	return ProviderSetupItem{
		ID:              provider.id,
		CatalogID:       provider.catalogID,
		Name:            firstNonEmptyTrimmed(provider.name, provider.id),
		Type:            provider.providerTyp,
		Status:          status,
		Configured:      true,
		Active:          active,
		Implemented:     true,
		RequiresBaseURL: provider.requiresBaseURL,
		Capabilities:    provider.capabilities,
		BaseURL:         provider.baseURL,
		BaseURLOptions:  providerBaseURLOptions(firstNonEmptyTrimmed(provider.catalogID, provider.id)),
		Model:           provider.model,
		ContextWindow:   provider.contextWindow,
		ReasoningEffort: provider.reasoningEffort,
		ToolUseMode:     provider.toolUseMode,
		APIKeyPreview:   provider.apiKeyPreview,
	}
}

func providerOptionSetupItem(option ProviderOption) ProviderSetupItem {
	status := ""
	if !option.Implemented {
		status = "Planned"
	}
	capabilitySet := providers.ResolveModelCapabilities(providers.ModelCapabilityInput{
		ProviderID:   option.ID,
		ProviderType: option.Type,
		ModelID:      option.DefaultModel,
	})
	return ProviderSetupItem{
		ID:              option.ID,
		CatalogID:       option.ID,
		Name:            option.Name,
		Type:            option.Type,
		Status:          status,
		Configured:      false,
		Active:          false,
		Implemented:     option.Implemented,
		RequiresBaseURL: option.RequiresBaseURL,
		Capabilities:    capabilitySet.ProviderCapabilities,
		BaseURL:         option.DefaultBaseURL,
		BaseURLOptions:  append([]providers.BaseURLOption(nil), option.BaseURLOptions...),
		DefaultModel:    option.DefaultModel,
		ReasoningEffort: capabilitySet.DefaultReasoningEffort,
		Notes:           option.Notes,
	}
}

func (s *Service) Draft() (Draft, error) {
	draft, draftErr := s.store.LoadDraft()
	if draftErr == nil {
		return draft, nil
	}
	if draftErr != nil && !errors.Is(draftErr, ErrDraftNotFound) {
		return Draft{}, draftErr
	}

	cfg, err := s.Load()
	if err == nil {
		return draftFromConfig(cfg), nil
	}
	if !errors.Is(err, ErrConfigNotFound) && !errors.Is(err, ErrUnsupportedConfigVersion) {
		return Draft{}, err
	}

	return defaultDraft(), nil
}

func (s *Service) Summary() (Summary, error) {
	return s.SummaryContext(context.Background())
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
			return applied, fmt.Errorf("%v; %w", inspectErr, applyErr)
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
	summary := daemonConfiguredSummary(cfg)
	if s.daemonManager == nil {
		return summary, nil
	}
	return s.daemonManager.Restart(ctx, s.store.Path(), cfg)
}

func (s *Service) StopDaemonContext(ctx context.Context) (DaemonSummary, error) {
	cfg, err := s.Load()
	if err != nil {
		return DaemonSummary{}, err
	}
	summary := daemonConfiguredSummary(cfg)
	if s.daemonManager == nil {
		return summary, nil
	}
	return s.daemonManager.Stop(ctx, s.store.Path(), cfg)
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

func (s *Service) BuiltInProviderDraft(draft Draft, providerID string) (ProviderDraft, error) {
	if provider, ok := FindProviderDraft(draft, providerID); ok {
		return provider, nil
	}
	option, ok := lookupProviderOption(providerID)
	if !ok {
		return ProviderDraft{}, fmt.Errorf("unknown provider %q", providerID)
	}
	if !option.Implemented {
		return ProviderDraft{}, fmt.Errorf("provider %q is listed but not implemented yet", option.Name)
	}
	return draftProviderFromOption(option), nil
}

func (s *Service) NewCustomProviderDraft(draft Draft, providerType string) (ProviderDraft, error) {
	providerType = providers.NormalizeOptionalProviderType(providerType)
	switch providerType {
	case providers.TypeOpenAICompat, providers.TypeOpenAICodex, providers.TypeAnthropic:
		return newCustomDraftProvider(providerType, draft.Providers), nil
	default:
		return ProviderDraft{}, fmt.Errorf("unsupported custom provider type %q", providerType)
	}
}

func (s *Service) SaveDraft(draft Draft) error {
	return s.store.SaveDraft(draft)
}

func (s *Service) ConfigureProviderContext(ctx context.Context, providerID string, update ProviderSetupUpdate) (ProviderSetupItem, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return ProviderSetupItem{}, errors.New("provider id is required")
	}

	draft, err := s.Draft()
	if err != nil {
		return ProviderSetupItem{}, err
	}
	provider, err := s.providerDraftForSetupUpdate(draft, providerID, update)
	if err != nil {
		return ProviderSetupItem{}, err
	}
	wasConfigured := ProviderDraftConfigured(provider)
	provider, err = applyProviderSetupUpdate(provider, update)
	if err != nil {
		return ProviderSetupItem{}, err
	}

	draft = UpsertProviderDraft(draft, provider)
	if draft.ActiveProviderID == "" || update.Active || !wasConfigured {
		draft.ActiveProviderID = provider.ID
	}
	if _, err := s.SaveRuntimeConfigContext(ctx, draft); err != nil {
		return ProviderSetupItem{}, err
	}
	return s.providerSetupItem(provider.ID)
}

func (s *Service) DeleteProviderContext(ctx context.Context, providerID string) error {
	providerID = providers.NormalizeProviderID(providerID)
	if providerID == "" {
		return errors.New("provider id is required")
	}
	draft, err := s.Draft()
	if err != nil {
		return err
	}
	provider, ok := FindProviderDraft(draft, providerID)
	if !ok {
		return fmt.Errorf("provider %q was not found", providerID)
	}
	if !isCustomProviderDraft(provider) {
		return fmt.Errorf("provider %q is built in and cannot be deleted here", provider.Name)
	}
	wasActive := sameProvider(draft.ActiveProviderID, providerID)
	draft = DeleteProviderDraft(draft, providerID)
	activeProvider, hasActiveProvider := FindProviderDraft(draft, draft.ActiveProviderID)
	if wasActive || draft.ActiveProviderID == "" || !hasActiveProvider || !ProviderDraftConfigured(activeProvider) {
		draft.ActiveProviderID = ""
		for _, candidate := range ConfiguredProviders(draft) {
			draft.ActiveProviderID = candidate.ID
			break
		}
	}
	_, err = s.SaveRuntimeConfigContext(ctx, draft)
	return err
}

func (s *Service) providerDraftForSetupUpdate(draft Draft, providerID string, update ProviderSetupUpdate) (ProviderDraft, error) {
	provider, err := s.BuiltInProviderDraft(draft, providerID)
	if err == nil {
		return provider, nil
	}
	providerType := providers.NormalizeOptionalProviderType(update.Type)
	switch providerType {
	case providers.TypeOpenAICompat, providers.TypeOpenAICodex, providers.TypeAnthropic:
	default:
		return ProviderDraft{}, err
	}
	providerID = providers.NormalizeProviderID(providerID)
	if providerID == "" {
		return ProviderDraft{}, errors.New("provider id is required")
	}
	name := strings.TrimSpace(update.Name)
	if name == "" {
		name = providerID
	}
	return ProviderDraft{
		ID:              providerID,
		Name:            name,
		Type:            providerType,
		BaseURL:         strings.TrimSpace(update.BaseURL),
		Model:           strings.TrimSpace(update.Model),
		ToolUseMode:     providers.NormalizeOptionalToolUseMode(update.ToolUseMode),
		ReasoningEffort: providers.DefaultReasoningEffortForModel(providerID, providerType, update.Model),
		HasStoredAPIKey: false,
	}, nil
}

func (s *Service) SaveRuntimeConfigContext(ctx context.Context, draft Draft) (ApplyResult, error) {
	cfg, telegramSummary, err := s.buildValidatedConfig(ctx, draft)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := s.store.Save(cfg); err != nil {
		return ApplyResult{}, err
	}
	if err := s.store.ClearDraft(); err != nil {
		return ApplyResult{}, err
	}

	return ApplyResult{
		Config:  cfg,
		Path:    s.store.Path(),
		Summary: summaryWithRuntimeValidation(cfg, telegramSummary),
	}, nil
}

func (s *Service) ProviderModels(ctx context.Context, provider ProviderDraft) ([]string, error) {
	result, err := s.ProviderModelCatalog(ctx, provider)
	if err != nil {
		return nil, err
	}
	if result.Status != ProviderModelStatusOK {
		return nil, providerModelsResponseError(result)
	}
	return result.Models, nil
}

func (s *Service) ProviderModelCatalog(ctx context.Context, provider ProviderDraft) (ProviderModelsResponse, error) {
	providerID := firstNonEmptyTrimmed(provider.CatalogID, provider.ID)
	policy := providers.PolicyForProvider(providerID, provider.Type)
	if !providers.ResolveModelCapabilities(providers.ModelCapabilityInput{
		ProviderID:   providerID,
		ProviderType: provider.Type,
		ModelID:      provider.Model,
	}).ProviderCapabilities.ModelDiscovery {
		return ProviderModelsResponse{
			Status:      ProviderModelStatusUnsupported,
			Source:      ProviderModelSourceManual,
			Message:     fmt.Sprintf("%s does not support model discovery", providerDisplayName(provider, ProviderOption{}, false)),
			ManualInput: true,
		}, nil
	}
	if strings.TrimSpace(provider.APIKey) == "" {
		existing, err := s.Load()
		if err == nil {
			if saved, ok := findProviderConfig(existing, provider.ID); ok {
				provider.APIKey, _ = ResolvedProviderAPIKey(saved)
			}
		}
	}
	if strings.TrimSpace(provider.APIKey) == "" {
		provider.APIKey = providerAPIKeyFromEnvName(providerDraftAPIKeyEnvName(provider))
	}
	if strings.TrimSpace(provider.APIKey) == "" && policy.RequiresAPIKey && !policy.PublicModelCatalog {
		return ProviderModelsResponse{
			Status:         ProviderModelStatusRequiresKey,
			Source:         ProviderModelSourceManual,
			Message:        "API key required",
			RequiresAPIKey: true,
		}, nil
	}
	models, err := providerdiscovery.Models(ctx, providerdiscovery.ModelDiscoveryInput{
		ID:        provider.ID,
		CatalogID: provider.CatalogID,
		Type:      provider.Type,
		BaseURL:   provider.BaseURL,
		APIKey:    provider.APIKey,
		Model:     provider.Model,
	})
	if err != nil {
		status := ProviderModelStatusUnavailable
		if isProviderModelAuthError(err) {
			status = ProviderModelStatusAuthError
		}
		return ProviderModelsResponse{
			Status:         status,
			Source:         providerModelCatalogSource(policy, provider.APIKey),
			Message:        "Could not load remote models: " + err.Error(),
			RequiresAPIKey: policy.RequiresAPIKey,
			ManualInput:    !policy.Known && status != ProviderModelStatusAuthError,
		}, nil
	}
	if len(models) == 0 {
		return ProviderModelsResponse{
			Status:         ProviderModelStatusUnavailable,
			Source:         providerModelCatalogSource(policy, provider.APIKey),
			Message:        "No models available",
			RequiresAPIKey: policy.RequiresAPIKey,
			ManualInput:    !policy.Known,
		}, nil
	}
	return ProviderModelsResponse{
		Models:         models,
		Metadata:       providerCatalogMetadata(firstNonEmptyTrimmed(provider.CatalogID, provider.ID), provider.Type, models),
		Status:         ProviderModelStatusOK,
		Source:         providerModelCatalogSource(policy, provider.APIKey),
		RequiresAPIKey: policy.RequiresAPIKey,
	}, nil
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

func (s *Service) ProviderModelCatalogContext(ctx context.Context, providerID string, update ProviderSetupUpdate) (ProviderModelsResponse, error) {
	draft, err := s.Draft()
	if err != nil {
		return ProviderModelsResponse{}, err
	}
	provider, ok := FindProviderDraft(draft, providerID)
	if !ok {
		provider, err = s.BuiltInProviderDraft(draft, providerID)
		if err != nil {
			provider, err = s.providerDraftForSetupUpdate(draft, providerID, update)
			if err != nil {
				return ProviderModelsResponse{}, err
			}
		}
	}
	provider, err = applyProviderSetupUpdate(provider, update)
	if err != nil {
		return ProviderModelsResponse{}, err
	}
	return s.ProviderModelCatalog(ctx, provider)
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

func providerModelsResponseError(response ProviderModelsResponse) error {
	return errors.New(ProviderModelCatalogMessage(response))
}

func isProviderModelAuthError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "401") ||
		strings.Contains(message, "403") ||
		strings.Contains(message, "unauthorized") ||
		strings.Contains(message, "forbidden") ||
		strings.Contains(message, "invalid api key") ||
		strings.Contains(message, "incorrect api key") ||
		strings.Contains(message, "permission")
}

func applyProviderSetupUpdate(provider ProviderDraft, update ProviderSetupUpdate) (ProviderDraft, error) {
	if isCustomProviderDraft(provider) {
		if name := strings.TrimSpace(update.Name); name != "" {
			provider.Name = name
		}
		if providerType := providers.NormalizeOptionalProviderType(update.Type); providerType != "" {
			switch providerType {
			case providers.TypeOpenAICompat, providers.TypeOpenAICodex, providers.TypeAnthropic:
				provider.Type = providerType
			default:
				return ProviderDraft{}, fmt.Errorf("unsupported custom provider type %q", providerType)
			}
		}
	}
	if apiKey := normalizeProviderAPIKey(update.APIKey); apiKey != "" {
		provider.APIKey = apiKey
		provider.HasStoredAPIKey = true
		provider.StoredAPIKeyPreview = MaskSecret(apiKey)
	}
	if baseURL := strings.TrimSpace(update.BaseURL); baseURL != "" {
		if provider.HasStoredAPIKey && strings.TrimSpace(update.APIKey) == "" && strings.TrimSpace(provider.BaseURL) != "" && strings.TrimSpace(provider.BaseURL) != baseURL {
			return ProviderDraft{}, errors.New("changing provider base URL requires re-entering the API key")
		}
		provider.BaseURL = baseURL
	}
	if model := strings.TrimSpace(update.Model); model != "" {
		provider.Model = model
	}
	if update.ContextWindow > 0 {
		provider.ContextWindow = strconv.Itoa(update.ContextWindow)
	}
	if reasoningEffort := providers.NormalizeReasoningEffortForModel(firstNonEmptyTrimmed(provider.CatalogID, provider.ID), provider.Type, firstNonEmptyTrimmed(update.Model, provider.Model), update.ReasoningEffort); reasoningEffort != "" {
		provider.ReasoningEffort = reasoningEffort
	}
	if toolUseMode := providers.NormalizeOptionalToolUseMode(update.ToolUseMode); toolUseMode != "" {
		provider.ToolUseMode = toolUseMode
	}
	return provider, nil
}

func parsePositiveInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func (s *Service) providerSetupItem(providerID string) (ProviderSetupItem, error) {
	items, err := s.ProviderSetupItems()
	if err != nil {
		return ProviderSetupItem{}, err
	}
	for _, item := range items {
		if sameProvider(item.ID, providerID) {
			return item, nil
		}
	}
	return ProviderSetupItem{}, fmt.Errorf("provider %q was not saved", providerID)
}

func (s *Service) Apply(draft Draft) (ApplyResult, error) {
	return s.ApplyContext(context.Background(), draft)
}

func (s *Service) ApplyContext(ctx context.Context, draft Draft) (ApplyResult, error) {
	cfg, telegramSummary, err := s.buildValidatedConfig(ctx, draft)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := s.store.Save(cfg); err != nil {
		return ApplyResult{}, err
	}

	result := ApplyResult{
		Config: cfg,
		Path:   s.store.Path(),
	}
	summary := summaryWithRuntimeValidation(cfg, telegramSummary)
	if s.daemonManager != nil {
		daemonSummary, warnings, applyErr := s.daemonManager.Apply(ctx, s.store.Path(), cfg)
		result.Warnings = append(result.Warnings, warnings...)
		summary.Daemon = daemonSummary
		if applyErr != nil {
			result.Summary = summary
			return result, applyErr
		}
	}
	if err := s.store.ClearDraft(); err != nil {
		return ApplyResult{}, err
	}
	result.Summary = summary
	return result, nil
}
