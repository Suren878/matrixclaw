package discovery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/providers/factory"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

// modelsDiscoveryTimeout caps how long a remote model listing call may take.
var modelsDiscoveryTimeout = 30 * time.Second

type ModelDiscoveryInput struct {
	ID        string
	CatalogID string
	Type      string
	BaseURL   string
	APIKey    string
	Model     string
}

func Models(ctx context.Context, input ModelDiscoveryInput) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, modelsDiscoveryTimeout)
	defer cancel()

	providerID := textutil.FirstNonEmpty(input.CatalogID, input.ID)
	policy := providers.PolicyForProvider(providerID, input.Type)
	if strings.TrimSpace(input.APIKey) == "" && policy.RequiresAPIKey && !policy.PublicModelCatalog {
		return nil, errors.New("enter a valid API key first")
	}

	models, err := fetchRemoteModels(ctx, input)
	providers.SaveModelMetadata()
	if err != nil {
		return nil, fmt.Errorf("could not verify API key or load models: %w", err)
	}
	if len(models) == 0 {
		return nil, errors.New("no models available")
	}

	models = normalizedModels(providerID, input.Type, models)
	if len(models) == 0 {
		return nil, errors.New("no models available")
	}
	return models, nil
}

func normalizedModels(providerID string, providerType string, models []string) []string {
	normalized := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = providers.NormalizeModelID(providerID, providerType, model)
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		normalized = append(normalized, model)
	}
	sort.Strings(normalized)
	return normalized
}

func fetchRemoteModels(ctx context.Context, input ModelDiscoveryInput) ([]string, error) {
	cfg := providers.RuntimeConfig{
		ProviderID: strings.TrimSpace(input.ID),
		CatalogID:  strings.TrimSpace(input.CatalogID),
		Type:       input.Type,
		APIKey:     strings.TrimSpace(input.APIKey),
		BaseURL:    strings.TrimSpace(input.BaseURL),
		Model:      strings.TrimSpace(input.Model),
	}
	adapter, err := factory.AdapterFor(cfg)
	if err != nil {
		return nil, errors.New("remote model list unavailable")
	}
	return adapter.ListModels(ctx, cfg)
}
