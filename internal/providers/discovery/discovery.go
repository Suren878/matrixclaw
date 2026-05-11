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
	if strings.TrimSpace(input.APIKey) == "" {
		return nil, errors.New("enter a valid API key first")
	}

	models, err := fetchRemoteModels(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("could not verify API key or load models: %w", err)
	}
	if len(models) == 0 {
		return nil, errors.New("no models available")
	}

	normalized := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = providers.NormalizeModelID(input.CatalogID, input.Type, model)
		if strings.TrimSpace(model) == "" {
			model = providers.NormalizeModelID(input.ID, input.Type, model)
		}
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
	models = normalized
	if len(models) == 0 {
		return nil, errors.New("no models available")
	}
	sort.Strings(models)
	return models, nil
}

func fetchRemoteModels(ctx context.Context, input ModelDiscoveryInput) ([]string, error) {
	providerType := strings.ToLower(strings.TrimSpace(input.Type))
	if providerType == "" {
		return nil, errors.New("remote model list unavailable")
	}
	profile := providers.ProfileForProvider(providerType)

	switch profile.RuntimeProviderType {
	case providers.TypeOpenAICompat:
		return openaicompat.ListModels(ctx, openaicompat.Config{
			APIKey:  strings.TrimSpace(input.APIKey),
			BaseURL: strings.TrimSpace(input.BaseURL),
			Model:   strings.TrimSpace(input.Model),
			Profile: profile,
		})
	case providers.TypeAnthropic:
		return anthropic.ListModels(ctx, anthropic.Config{
			APIKey:  strings.TrimSpace(input.APIKey),
			BaseURL: strings.TrimSpace(input.BaseURL),
			Model:   strings.TrimSpace(input.Model),
		})
	case providers.TypeGemini:
		return gemini.ListModels(ctx, gemini.Config{
			APIKey:  strings.TrimSpace(input.APIKey),
			BaseURL: strings.TrimSpace(input.BaseURL),
			Model:   strings.TrimSpace(input.Model),
			Profile: profile,
		})
	default:
		return nil, errors.New("remote model list unavailable")
	}
}
