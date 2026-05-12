package factory

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	anthropic "github.com/Suren878/matrixclaw/internal/providers/ai/anthropiccompat"
	"github.com/Suren878/matrixclaw/internal/providers/ai/gemini"
	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicompat"
)

type Config struct {
	ProviderID      string
	CatalogID       string
	Type            string
	APIKey          string
	BaseURL         string
	Model           string
	MaxOutputTokens int64
	ReasoningEffort string
	ToolUseMode     providers.ToolUseMode
}

var (
	newOpenAICompatRuntime = func(ctx context.Context, cfg openaicompat.Config) (providers.Runtime, error) {
		return openaicompat.New(ctx, cfg)
	}
	newAnthropicRuntime = func(ctx context.Context, cfg anthropic.Config) (providers.Runtime, error) {
		return anthropic.New(ctx, cfg)
	}
	newGeminiRuntime = func(ctx context.Context, cfg gemini.Config) (providers.Runtime, error) {
		return gemini.New(ctx, cfg)
	}
)

func NewRuntime(ctx context.Context, cfg Config) (providers.Runtime, error) {
	providerType := strings.ToLower(strings.TrimSpace(cfg.Type))
	if providerType == "" {
		return nil, fmt.Errorf("unsupported provider type %q", strings.TrimSpace(cfg.Type))
	}
	apiKey := strings.TrimSpace(cfg.APIKey)
	baseURL := strings.TrimSpace(cfg.BaseURL)
	model := strings.TrimSpace(cfg.Model)
	profileID := strings.TrimSpace(cfg.CatalogID)
	if profileID == "" {
		profileID = strings.TrimSpace(cfg.ProviderID)
	}
	profile := providers.ProfileForModel(profileID, providerType, model)

	switch profile.RuntimeProviderType {
	case providers.TypeOpenAICompat:
		return newOpenAICompatRuntime(ctx, openaicompat.Config{
			ProviderID:      strings.TrimSpace(cfg.ProviderID),
			CatalogID:       strings.TrimSpace(cfg.CatalogID),
			APIKey:          apiKey,
			BaseURL:         baseURL,
			Model:           model,
			MaxOutputTokens: cfg.MaxOutputTokens,
			ReasoningEffort: strings.TrimSpace(cfg.ReasoningEffort),
			ToolUseMode:     cfg.ToolUseMode,
			Profile:         profile,
		})
	case providers.TypeAnthropic:
		return newAnthropicRuntime(ctx, anthropic.Config{
			ProviderID:      strings.TrimSpace(cfg.ProviderID),
			CatalogID:       strings.TrimSpace(cfg.CatalogID),
			APIKey:          apiKey,
			BaseURL:         baseURL,
			Model:           model,
			MaxOutputTokens: cfg.MaxOutputTokens,
			Profile:         profile,
		})
	case providers.TypeGemini:
		return newGeminiRuntime(ctx, gemini.Config{
			ProviderID:      strings.TrimSpace(cfg.ProviderID),
			CatalogID:       strings.TrimSpace(cfg.CatalogID),
			APIKey:          apiKey,
			BaseURL:         baseURL,
			Model:           model,
			MaxOutputTokens: cfg.MaxOutputTokens,
			ToolUseMode:     cfg.ToolUseMode,
			Profile:         profile,
		})
	default:
		return nil, fmt.Errorf("unsupported provider type %q", strings.TrimSpace(cfg.Type))
	}
}
