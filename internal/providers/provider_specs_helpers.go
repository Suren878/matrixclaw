package providers

func openAICompatibleProvider(order int, id string, name string, baseURL string, model string, apiKeyEnv string, notes string, aliases ...string) ProviderSpec {
	return ProviderSpec{
		Order: order,
		Entry: CatalogEntry{
			ID:              id,
			Name:            name,
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true, ToolCalling: true},
			DefaultBaseURL:  baseURL,
			DefaultModel:    model,
			APIKeyEnv:       apiKeyEnv,
			Notes:           notes,
		},
		Aliases:   aliases,
		Auth:      ProviderAuthAPIKey,
		Transport: ProviderTransportOpenAIChat,
	}
}
