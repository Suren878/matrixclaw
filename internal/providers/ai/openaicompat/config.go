package openaicompat

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

const defaultTimeout = 90 * time.Second

type Config struct {
	ProviderID      string
	CatalogID       string
	APIKey          string
	BaseURL         string
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
	maxOutputTokens  int64
	reasoningEffort  string
	useCompletionMax bool
	profile          providers.RuntimeProfile
	capabilities     providers.ModelCapabilities
}

func New(_ context.Context, cfg Config) (providers.Runtime, error) {
	client, apiKey, baseURL, model, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	providerProfile := cfg.Profile
	if providerProfile == (providers.ProviderProfile{}) {
		providerProfile = providers.ProfileForProvider(providers.TypeOpenAICompat)
	}
	profile := providerProfile.RuntimeProfileWithOverrides(providers.RuntimeProfile{
		ToolUseMode: cfg.ToolUseMode,
	})
	reasoningEffort := ""
	if providerProfile.SupportsReasoningEffort {
		reasoningEffort = providers.NormalizeReasoningEffort(cfg.ReasoningEffort)
	}
	return &Runtime{
		client:           client,
		endpoint:         strings.TrimRight(baseURL, "/") + "/chat/completions",
		apiKey:           apiKey,
		model:            model,
		maxOutputTokens:  cfg.MaxOutputTokens,
		reasoningEffort:  reasoningEffort,
		useCompletionMax: useMaxCompletionTokens(baseURL, model),
		profile:          profile,
		capabilities:     providerProfile.Capabilities,
	}, nil
}

func (r *Runtime) RuntimeProfile() providers.RuntimeProfile {
	return r.profile
}

func (r *Runtime) ModelCapabilities() providers.ModelCapabilities {
	return r.capabilities
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
		client = &http.Client{Timeout: defaultTimeout}
	}
	return client, apiKey, baseURL, model, nil
}

func useMaxCompletionTokens(baseURL string, model string) bool {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-5") {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Host))
	switch host {
	case "api.openai.com", "api.openai.com:443":
		return true
	default:
		return false
	}
}
