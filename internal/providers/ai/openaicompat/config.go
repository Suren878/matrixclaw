package openaicompat

import (
	"context"
	"strings"
	"sync"

	"github.com/Suren878/matrixclaw/internal/providers"
)

type Runtime struct {
	providers.RuntimeBase
	endpoint            string
	reasoningEffort     string
	useCompletionMax    bool
	promptCacheKey      bool
	contentCacheControl bool
	headers             map[string]string
	maxTokensLimit      maxTokensLimitState
}

// maxTokensLimitState remembers a gateway's rejection of the output-limit
// field across calls, so later requests skip the 400-and-retry: a cap on its
// value, or that the field itself is unsupported and must be omitted.
type maxTokensLimitState struct {
	mu   sync.Mutex
	cap  int64
	omit bool
}

// learnedMaxTokensLimit reports a remembered cap and whether the field should
// be omitted entirely.
func (r *Runtime) learnedMaxTokensLimit() (capTokens int64, omit bool) {
	r.maxTokensLimit.mu.Lock()
	defer r.maxTokensLimit.mu.Unlock()
	return r.maxTokensLimit.cap, r.maxTokensLimit.omit
}

// rememberMaxTokensRejection records a gateway's rejection of the output-limit
// field so future requests apply it upfront. An unsupported field wins over
// any previously learned cap.
func (r *Runtime) rememberMaxTokensRejection(capTokens int64, omit bool) {
	r.maxTokensLimit.mu.Lock()
	defer r.maxTokensLimit.mu.Unlock()
	if omit {
		r.maxTokensLimit.omit = true
		r.maxTokensLimit.cap = 0
		return
	}
	if r.maxTokensLimit.omit {
		return
	}
	if capTokens > 0 {
		r.maxTokensLimit.cap = capTokens
	}
}

func New(_ context.Context, cfg providers.RuntimeConfig) (providers.Runtime, error) {
	base := providers.NewRuntimeBase(cfg, providers.TypeOpenAICompat, providers.DefaultOpenAICompatModel)
	if err := base.RequireKeyAndURL("openaicompat"); err != nil {
		return nil, err
	}
	reasoningEffort := ""
	if base.Capabilities.ReasoningEffort {
		reasoningEffort = runtimeReasoningEffort(cfg.ReasoningEffort)
	}
	chatOptions := providers.ResolveOpenAIChatOptions(cfg.CatalogKey(), base.BaseURL, base.Model)
	return &Runtime{
		RuntimeBase:         base,
		endpoint:            strings.TrimRight(base.BaseURL, "/") + "/chat/completions",
		reasoningEffort:     reasoningEffort,
		useCompletionMax:    chatOptions.MaxCompletionTokens,
		promptCacheKey:      chatOptions.PromptCacheKey,
		contentCacheControl: chatOptions.ContentCacheControl,
		headers:             chatOptions.Headers,
	}, nil
}

func runtimeReasoningEffort(value string) string {
	effort := providers.NormalizeReasoningEffort(value)
	if effort == providers.ReasoningEffortNone {
		return ""
	}
	return effort
}

func (r *Runtime) OutputLimits() (int64, int64) {
	capTokens, omit := r.learnedMaxTokensLimit()
	if omit {
		return 0, 0
	}
	current, ceiling := r.CatalogOutputLimits()
	if capTokens > 0 && (ceiling == 0 || capTokens < ceiling) {
		ceiling = capTokens
	}
	if ceiling > 0 && current > ceiling {
		current = ceiling
	}
	return current, ceiling
}
