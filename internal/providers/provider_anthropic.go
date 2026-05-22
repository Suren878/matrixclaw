package providers

func init() {
	registerProvider(ProviderSpec{
		Order: 300,
		Entry: CatalogEntry{
			ID:              "anthropic",
			Name:            "Anthropic",
			Type:            TypeAnthropic,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true},
			DefaultBaseURL:  "https://api.anthropic.com/v1",
			DefaultModel:    DefaultAnthropicModel,
			APIKeyEnv:       "ANTHROPIC_API_KEY",
			Notes:           "Native Anthropic path with Messages API semantics.",
		},
		Auth:      ProviderAuthAPIKey,
		Transport: ProviderTransportAnthropicMessage,
	})
}
