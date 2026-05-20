package sessionllm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/providers/discovery"
	providerfactory "github.com/Suren878/matrixclaw/internal/providers/factory"
)

var ErrNoActiveProvider = errors.New("no active provider configured; open setup providers to configure one")

type Registry struct {
	mu        sync.Mutex
	providers map[string]ProviderSpec
	order     []string
	activeID  string
	runtimes  map[string]providers.Runtime
}

type ProviderSpec struct {
	ID              string
	CatalogID       string
	Name            string
	Type            string
	APIKey          string
	BaseURL         string
	Model           string
	ContextWindow   int
	MaxOutputTokens int64
	ReasoningEffort string
	ToolUseMode     providers.ToolUseMode
}

func New(activeProviderID string, providerSpecs []ProviderSpec) *Registry {
	reg := &Registry{
		providers: make(map[string]ProviderSpec, len(providerSpecs)),
		order:     make([]string, 0, len(providerSpecs)),
		activeID:  strings.TrimSpace(activeProviderID),
		runtimes:  map[string]providers.Runtime{},
	}
	for _, provider := range providerSpecs {
		id := strings.TrimSpace(provider.ID)
		if id == "" {
			continue
		}
		provider.ID = id
		provider.CatalogID = providers.NormalizeProviderID(provider.CatalogID)
		provider.Type = providers.NormalizeOptionalProviderType(provider.Type)
		provider.Model = strings.TrimSpace(provider.Model)
		reg.providers[id] = provider
		reg.order = append(reg.order, id)
	}
	if reg.activeID == "" && len(reg.order) > 0 {
		reg.activeID = reg.order[0]
	}
	return reg
}

func (r *Registry) ActiveSelection() (string, string) {
	cfg, ok := r.lookup(strings.TrimSpace(r.activeID))
	if !ok {
		return "", ""
	}
	return cfg.ID, strings.TrimSpace(cfg.Model)
}

func (r *Registry) Providers() []core.SessionProviderOption {
	options := make([]core.SessionProviderOption, 0, len(r.order))
	for _, id := range r.order {
		cfg, ok := r.lookup(id)
		if !ok {
			continue
		}
		options = append(options, core.SessionProviderOption{
			ID:           cfg.ID,
			Label:        firstNonEmpty(cfg.Name, cfg.ID),
			Type:         providers.NormalizeOptionalProviderType(cfg.Type),
			DefaultModel: strings.TrimSpace(cfg.Model),
			Configured:   true,
		})
	}
	return options
}

func (r *Registry) Normalize(providerID string, modelID string) (core.SessionProviderOption, string, error) {
	cfg, ok := r.resolveProvider(providerID)
	if !ok {
		if len(r.order) == 0 {
			if strings.TrimSpace(providerID) == "" && strings.TrimSpace(modelID) == "" {
				return core.SessionProviderOption{}, "", nil
			}
			return core.SessionProviderOption{}, "", ErrNoActiveProvider
		}
		return core.SessionProviderOption{}, "", fmt.Errorf("provider %q is not configured", strings.TrimSpace(providerID))
	}
	option := core.SessionProviderOption{
		ID:           cfg.ID,
		Label:        firstNonEmpty(cfg.Name, cfg.ID),
		Type:         providers.NormalizeOptionalProviderType(cfg.Type),
		DefaultModel: strings.TrimSpace(cfg.Model),
		Configured:   true,
	}
	resolvedModel := strings.TrimSpace(modelID)
	if resolvedModel == "" {
		resolvedModel = strings.TrimSpace(cfg.Model)
	}
	resolvedModel = providers.NormalizeModelID(cfg.CatalogID, cfg.Type, resolvedModel)
	if resolvedModel == "" {
		return core.SessionProviderOption{}, "", fmt.Errorf("provider %q has no model", cfg.ID)
	}
	return option, resolvedModel, nil
}

func (r *Registry) Models(ctx context.Context, providerID string) ([]string, error) {
	cfg, ok := r.resolveProvider(providerID)
	if !ok {
		if len(r.order) == 0 {
			return nil, ErrNoActiveProvider
		}
		return nil, fmt.Errorf("provider %q is not configured", strings.TrimSpace(providerID))
	}
	models, err := discovery.Models(ctx, discovery.ModelDiscoveryInput{
		ID:        cfg.ID,
		CatalogID: cfg.CatalogID,
		Type:      cfg.Type,
		BaseURL:   cfg.BaseURL,
		APIKey:    cfg.APIKey,
		Model:     cfg.Model,
	})
	if err != nil {
		return nil, err
	}
	return models, nil
}

func (r *Registry) ContextWindowTokens(providerID string, modelID string) (int, bool) {
	cfg, ok := r.resolveProvider(providerID)
	if !ok || cfg.ContextWindow <= 0 {
		return 0, false
	}
	return cfg.ContextWindow, true
}

func (r *Registry) Resolve(ctx context.Context, providerID string, modelID string) (providers.Runtime, core.SessionProviderOption, string, error) {
	option, resolvedModel, err := r.Normalize(providerID, modelID)
	if err != nil {
		return nil, core.SessionProviderOption{}, "", err
	}
	if strings.TrimSpace(option.ID) == "" {
		return nil, core.SessionProviderOption{}, "", ErrNoActiveProvider
	}
	cfg, _ := r.resolveProvider(option.ID)
	cacheKey := option.ID + "\x00" + resolvedModel

	r.mu.Lock()
	if runtime, ok := r.runtimes[cacheKey]; ok {
		r.mu.Unlock()
		return runtime, option, resolvedModel, nil
	}
	r.mu.Unlock()

	runtime, err := providerfactory.NewRuntime(ctx, runtimeConfigWithModel(cfg, resolvedModel))
	if err != nil {
		return nil, core.SessionProviderOption{}, "", err
	}

	r.mu.Lock()
	r.runtimes[cacheKey] = runtime
	r.mu.Unlock()
	return runtime, option, resolvedModel, nil
}

func (r *Registry) resolveProvider(providerID string) (ProviderSpec, bool) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		providerID = strings.TrimSpace(r.activeID)
	}
	return r.lookup(providerID)
}

func (r *Registry) lookup(providerID string) (ProviderSpec, bool) {
	cfg, ok := r.providers[strings.TrimSpace(providerID)]
	return cfg, ok
}

func runtimeConfigWithModel(provider ProviderSpec, model string) providerfactory.Config {
	return providerfactory.Config{
		ProviderID:      provider.ID,
		CatalogID:       provider.CatalogID,
		Type:            provider.Type,
		APIKey:          provider.APIKey,
		BaseURL:         provider.BaseURL,
		Model:           model,
		MaxOutputTokens: provider.MaxOutputTokens,
		ReasoningEffort: provider.ReasoningEffort,
		ToolUseMode:     provider.ToolUseMode,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
