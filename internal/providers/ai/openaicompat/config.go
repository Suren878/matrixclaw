package openaicompat

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/Suren878/matrixclaw/internal/providers"
)

type Config struct {
	ProviderID      string
	CatalogID       string
	APIKey          string
	BaseURL         string
	ModelsURL       string
	PublicModels    bool
	Model           string
	MaxOutputTokens int64
	ReasoningEffort string
	ToolUseMode     providers.ToolUseMode
	Profile         providers.ProviderProfile
	HTTPClient      *http.Client
}

type Runtime struct {
	client           *http.Client
	endpoint         string
	apiKey           string
	model            string
	metadataID       string
	maxOutputTokens  int64
	reasoningEffort  string
	useCompletionMax bool
	promptCacheKey   bool
	headers          map[string]string
	quirks           providers.OpenAIChatRequestQuirks
	profile          providers.RuntimeProfile
	capabilities     providers.ModelCapabilities
	maxTokensLimit   maxTokensLimitState
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

func New(_ context.Context, cfg Config) (providers.Runtime, error) {
	client, apiKey, baseURL, model, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	providerProfile := cfg.Profile
	if providerProfile.IsZero() {
		providerProfile = providers.ProfileForProvider(providers.TypeOpenAICompat)
	}
	profile := providerProfile.RuntimeProfileWithOverrides(providers.RuntimeProfile{
		ToolUseMode: cfg.ToolUseMode,
	})
	reasoningEffort := ""
	if providerProfile.SupportsReasoningEffort {
		reasoningEffort = runtimeReasoningEffort(cfg.ReasoningEffort)
	}
	chatOptions := providers.ResolveOpenAIChatOptions(providerProfile, baseURL, model)
	return &Runtime{
		client:           client,
		endpoint:         strings.TrimRight(baseURL, "/") + "/chat/completions",
		apiKey:           apiKey,
		model:            model,
		metadataID:       firstNonEmptyString(cfg.ProviderID, cfg.CatalogID),
		maxOutputTokens:  cfg.MaxOutputTokens,
		reasoningEffort:  reasoningEffort,
		useCompletionMax: chatOptions.MaxTokensField == providers.OpenAIChatMaxCompletionTokens,
		promptCacheKey:   chatOptions.PromptCacheKey,
		headers:          chatOptions.DefaultHeaders,
		quirks:           chatOptions.RequestQuirks,
		profile:          profile,
		capabilities:     providerProfile.Capabilities,
	}, nil
}

func runtimeReasoningEffort(value string) string {
	effort := providers.NormalizeReasoningEffort(value)
	if effort == providers.ReasoningEffortNone {
		return ""
	}
	return effort
}

func (r *Runtime) RuntimeProfile() providers.RuntimeProfile {
	return r.profile
}

func (r *Runtime) Identity() (string, string) {
	return providers.TypeOpenAICompat, r.model
}

func (r *Runtime) ModelCapabilities() providers.ModelCapabilities {
	return r.capabilities
}

func (r *Runtime) OutputLimits() (int64, int64) {
	capTokens, omit := r.learnedMaxTokensLimit()
	if omit {
		return 0, 0
	}
	current := providers.ResolveMaxOutputTokens(0, r.maxOutputTokens, r.metadataID, providers.TypeOpenAICompat, r.model)
	ceiling := int64(providers.ResolveModelMetadata(r.metadataID, providers.TypeOpenAICompat, r.model).MaxOutputTokens)
	if capTokens > 0 && (ceiling == 0 || capTokens < ceiling) {
		ceiling = capTokens
	}
	if ceiling > 0 && current > ceiling {
		current = ceiling
	}
	return current, ceiling
}

func normalizeConfig(cfg Config) (*http.Client, string, string, string, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, "", "", "", errors.New("openaicompat: api key is required")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return nil, "", "", "", errors.New("openaicompat: base url is required")
	}

	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = providers.DefaultOpenAICompatModel
	}

	client := cfg.HTTPClient
	if client == nil {
		client = providers.NewHTTPClient()
	}
	return client, apiKey, baseURL, model, nil
}
