package discovery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	anthropic "github.com/Suren878/matrixclaw/internal/providers/ai/anthropiccompat"
	"github.com/Suren878/matrixclaw/internal/providers/ai/gemini"
	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicodex"
	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicompat"
)

type ModelDiscoveryInput struct {
	ID        string
	CatalogID string
	Type      string
	BaseURL   string
	APIKey    string
	Model     string
}

func Models(ctx context.Context, input ModelDiscoveryInput) ([]string, error) {
	providerID := firstNonEmpty(input.CatalogID, input.ID)
	policy := providers.PolicyForProvider(providerID, input.Type)
	if strings.TrimSpace(input.APIKey) == "" && policy.RequiresAPIKey && !policy.PublicModelCatalog {
		return nil, errors.New("enter a valid API key first")
	}

	models, err := fetchRemoteModels(ctx, input)
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
	providerType := providers.NormalizeOptionalProviderType(input.Type)
	if providerType == "" {
		return nil, errors.New("remote model list unavailable")
	}
	providerID := firstNonEmpty(input.CatalogID, input.ID)
	profile := providers.ProfileForModel(providerID, providerType, input.Model)

	switch profile.RuntimeProviderType {
	case providers.TypeOpenAICodex:
		return openaicodex.ListModels(ctx, openaicodex.Config{
			ProviderID: strings.TrimSpace(input.ID),
			CatalogID:  strings.TrimSpace(input.CatalogID),
			BaseURL:    strings.TrimSpace(input.BaseURL),
			Model:      strings.TrimSpace(input.Model),
			Profile:    profile,
		})
	case providers.TypeOpenAICompat:
		policy := providers.PolicyForProvider(providerID, providerType)
		return openaicompat.ListModels(ctx, openaicompat.Config{
			ProviderID:   strings.TrimSpace(input.ID),
			CatalogID:    strings.TrimSpace(input.CatalogID),
			APIKey:       strings.TrimSpace(input.APIKey),
			BaseURL:      strings.TrimSpace(input.BaseURL),
			ModelsURL:    policy.ModelsURL,
			PublicModels: policy.PublicModelCatalog,
			Model:        strings.TrimSpace(input.Model),
			Profile:      profile,
		})
	case providers.TypeAnthropic:
		return anthropic.ListModels(ctx, anthropic.Config{
			ProviderID: strings.TrimSpace(input.ID),
			CatalogID:  strings.TrimSpace(input.CatalogID),
			APIKey:     strings.TrimSpace(input.APIKey),
			BaseURL:    strings.TrimSpace(input.BaseURL),
			Model:      strings.TrimSpace(input.Model),
			Profile:    profile,
		})
	case providers.TypeGemini:
		return gemini.ListModels(ctx, gemini.Config{
			ProviderID: strings.TrimSpace(input.ID),
			CatalogID:  strings.TrimSpace(input.CatalogID),
			APIKey:     strings.TrimSpace(input.APIKey),
			BaseURL:    strings.TrimSpace(input.BaseURL),
			Model:      strings.TrimSpace(input.Model),
			Profile:    profile,
		})
	default:
		return nil, errors.New("remote model list unavailable")
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
