package factory

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	anthropic "github.com/Suren878/matrixclaw/internal/providers/ai/anthropiccompat"
	"github.com/Suren878/matrixclaw/internal/providers/ai/gemini"
	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicodex"
	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicompat"
)

// Adapter builds runtimes and lists models for one runtime provider type.
type Adapter struct {
	New        func(context.Context, providers.RuntimeConfig) (providers.Runtime, error)
	ListModels func(context.Context, providers.RuntimeConfig) ([]string, error)
}

var adapters = map[string]Adapter{
	providers.TypeOpenAICompat: {New: openaicompat.New, ListModels: openaicompat.ListModels},
	providers.TypeOpenAICodex:  {New: openaicodex.New, ListModels: openaicodex.ListModels},
	providers.TypeAnthropic:    {New: anthropic.New, ListModels: anthropic.ListModels},
	providers.TypeGemini:       {New: gemini.New, ListModels: gemini.ListModels},
}

// AdapterFor picks the adapter that speaks to cfg's provider.
func AdapterFor(cfg providers.RuntimeConfig) (Adapter, error) {
	providerType := providers.NormalizeOptionalProviderType(cfg.Type)
	adapter, ok := adapters[providers.PolicyForProvider(cfg.CatalogKey(), providerType).RuntimeProviderType]
	if providerType == "" || !ok {
		return Adapter{}, fmt.Errorf("unsupported provider type %q", strings.TrimSpace(cfg.Type))
	}
	return adapter, nil
}

func NewRuntime(ctx context.Context, cfg providers.RuntimeConfig) (providers.Runtime, error) {
	adapter, err := AdapterFor(cfg)
	if err != nil {
		return nil, err
	}
	return adapter.New(ctx, cfg)
}
