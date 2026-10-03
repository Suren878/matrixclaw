package providers

import (
	"cmp"
	"errors"
	"net/http"
	"strings"
)

// RuntimeConfig is what every adapter is built from.
type RuntimeConfig struct {
	ProviderID      string
	CatalogID       string
	Type            string
	APIKey          string
	BaseURL         string
	Model           string
	MaxOutputTokens int64
	ReasoningEffort string
	ToolUseMode     ToolUseMode
	HTTPClient      *http.Client
}

// CatalogKey names the catalog entry the provider was set up from.
func (c RuntimeConfig) CatalogKey() string {
	return cmp.Or(strings.TrimSpace(c.CatalogID), strings.TrimSpace(c.ProviderID))
}

// RuntimeBase is the state every adapter keeps and the answers it gives the
// same way; adapters embed it.
type RuntimeBase struct {
	Client          *http.Client
	APIKey          string
	BaseURL         string
	Model           string
	MaxOutputTokens int64
	// MetadataID is the provider ID learned model metadata is kept under.
	MetadataID   string
	ProviderType string
	Capabilities ModelCapabilities
}

// NewRuntimeBase trims cfg, applies the adapter's defaults and resolves the
// model's capabilities; a user who disabled tool use gets no tool calling.
func NewRuntimeBase(cfg RuntimeConfig, providerType string, defaultModel string) RuntimeBase {
	model := cmp.Or(strings.TrimSpace(cfg.Model), defaultModel)
	capabilities := ResolveModelCapabilities(ModelCapabilityInput{
		ProviderID:   cfg.CatalogKey(),
		ProviderType: providerType,
		ModelID:      model,
	}).RuntimeCapabilities
	if NormalizeOptionalToolUseMode(cfg.ToolUseMode) == ToolUseDisabled {
		capabilities.ToolCalling = false
		capabilities.ParallelToolCalls = false
		capabilities.ReasoningWithTools = false
	}
	client := cfg.HTTPClient
	if client == nil {
		client = NewHTTPClient()
	}
	return RuntimeBase{
		Client:          client,
		APIKey:          strings.TrimSpace(cfg.APIKey),
		BaseURL:         strings.TrimSpace(cfg.BaseURL),
		Model:           model,
		MaxOutputTokens: cfg.MaxOutputTokens,
		MetadataID:      cmp.Or(strings.TrimSpace(cfg.ProviderID), strings.TrimSpace(cfg.CatalogID)),
		ProviderType:    providerType,
		Capabilities:    capabilities,
	}
}

// RequireKeyAndURL fails a base that lacks an API key or base URL.
func (b *RuntimeBase) RequireKeyAndURL(adapter string) error {
	if b.APIKey == "" {
		return errors.New(adapter + ": api key is required")
	}
	if b.BaseURL == "" {
		return errors.New(adapter + ": base url is required")
	}
	return nil
}

func (b *RuntimeBase) Identity() (string, string) {
	return b.ProviderType, b.Model
}

func (b *RuntimeBase) ModelCapabilities() ModelCapabilities {
	return b.Capabilities
}

// CatalogOutputLimits is OutputLimits for an adapter that sends the configured
// or catalog limit as is.
func (b *RuntimeBase) CatalogOutputLimits() (int64, int64) {
	current := ResolveMaxOutputTokens(0, b.MaxOutputTokens, b.MetadataID, b.ProviderType, b.Model)
	return current, int64(ResolveModelMetadata(b.MetadataID, b.ProviderType, b.Model).MaxOutputTokens)
}
