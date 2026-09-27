package core

import (
	"context"
	"log"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

// WithCompactModel names the provider and model that write context summaries;
// empty keeps each run's own model.
func (c *Core) WithCompactModel(providerID string, modelID string) *Core {
	c.compactProvider, c.compactModel = strings.TrimSpace(providerID), strings.TrimSpace(modelID)
	return c
}

// compactRuntime is the configured summary model; nil means the run's own model,
// also when the configured one cannot be resolved.
func (c *Core) compactRuntime(ctx context.Context) providers.Runtime {
	if c.compactProvider == "" {
		return nil
	}
	llms := c.sessionLLMs()
	if llms == nil {
		return nil
	}
	runtime, _, _, err := llms.Resolve(ctx, c.compactProvider, c.compactModel)
	if err != nil || runtime == nil {
		log.Printf("core: compact model %s/%s is unavailable, summaries use the run's model: %v", c.compactProvider, c.compactModel, err)
		return nil
	}
	return runtime
}
