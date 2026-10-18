package core

import (
	"context"
	"log"

	"github.com/Suren878/matrixclaw/internal/providers"
)

// compactRuntime is the configured summary model and its context window; nil
// means the run's own model, also when the configured one cannot be resolved.
func (c *Core) compactRuntime(ctx context.Context) (providers.Runtime, int) {
	tuning := c.Tuning()
	if tuning.CompactProvider == "" {
		return nil, 0
	}
	llms := c.sessionLLMs()
	if llms == nil {
		return nil, 0
	}
	runtime, _, modelID, err := llms.Resolve(ctx, tuning.CompactProvider, tuning.CompactModel)
	if err != nil || runtime == nil {
		if !c.compactUnavailable.Swap(true) {
			log.Printf("core: compact model %s/%s is unavailable, summaries use the run's model: %v", tuning.CompactProvider, tuning.CompactModel, err)
		}
		return nil, 0
	}
	c.compactUnavailable.Store(false)
	return runtime, c.modelContextWindowTokens(tuning.CompactProvider, modelID)
}
