package providers

import (
	"cmp"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// RuntimeConfig is what every adapter is built from. ProviderID is the
// configured provider's canonical id; learned model metadata is kept under it.
type RuntimeConfig struct {
	ProviderID      string
	Type            string
	APIKey          string
	BaseURL         string
	Model           string
	MaxOutputTokens int64
	ReasoningEffort string
	ToolUseMode     ToolUseMode
	HTTPClient      *http.Client
}

// RuntimeBase is the state every adapter keeps and the answers it gives the
// same way; adapters embed it.
type RuntimeBase struct {
	Client          *http.Client
	APIKey          string
	BaseURL         string
	Model           string
	MaxOutputTokens int64
	ProviderID      string
	ProviderType    string
	Capabilities    ModelCapabilities
}

// NewRuntimeBase trims cfg, applies the adapter's defaults and resolves the
// model's capabilities; a user who disabled tool use gets no tool calling.
func NewRuntimeBase(cfg RuntimeConfig, providerType string, defaultModel string) RuntimeBase {
	model := cmp.Or(strings.TrimSpace(cfg.Model), defaultModel)
	providerID := NormalizeProviderID(cfg.ProviderID)
	capabilities := ResolveModelCapabilities(ModelCapabilityInput{
		ProviderID:   providerID,
		ProviderType: providerType,
		ModelID:      model,
	}).RuntimeCapabilities
	if NormalizeOptionalToolUseMode(cfg.ToolUseMode) == ToolUseDisabled {
		capabilities.ToolCalling = false
	}
	client := cfg.HTTPClient
	if client == nil {
		client = newHTTPClient(defaultResponseHeaderTimeout, streamIdleTimeout(cfg.ReasoningEffort))
	}
	return RuntimeBase{
		Client:          client,
		APIKey:          strings.TrimSpace(cfg.APIKey),
		BaseURL:         strings.TrimSpace(cfg.BaseURL),
		Model:           model,
		MaxOutputTokens: cfg.MaxOutputTokens,
		ProviderID:      providerID,
		ProviderType:    providerType,
		Capabilities:    capabilities,
	}
}

// RequireKeyAndURL fails a base that lacks a base URL, or an API key for a
// server other than a local one.
func (b *RuntimeBase) RequireKeyAndURL(adapter string) error {
	if b.BaseURL == "" {
		return errors.New(adapter + ": base url is required")
	}
	if b.APIKey == "" && !IsLoopbackURL(b.BaseURL) {
		return errors.New(adapter + ": api key is required")
	}
	return nil
}

// IsLoopbackURL reports a base URL on this machine (localhost or a loopback
// IP), where model servers usually take no API key.
func IsLoopbackURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
	current := ResolveMaxOutputTokens(0, b.MaxOutputTokens, b.ProviderID, b.ProviderType, b.Model)
	return current, int64(ResolveModelMetadata(b.ProviderID, b.ProviderType, b.Model).MaxOutputTokens)
}
