package providers

func init() {
	registerProvider(ProviderSpec{
		Order: 10,
		Entry: CatalogEntry{
			ID:              "openai",
			Name:            "OpenAI",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities: Capabilities{
				ModelDiscovery:  true,
				ReasoningEffort: true,
				ToolCalling:     true,
			},
			DefaultBaseURL: "https://api.openai.com/v1",
			DefaultModel:   DefaultOpenAICompatModel,
			APIKeyEnv:      "OPENAI_API_KEY",
			Notes:          "Standard OpenAI endpoint through the generic OpenAI-compatible path.",
		},
		Auth:                ProviderAuthAPIKey,
		Transport:           ProviderTransportOpenAIChat,
		RuntimeProviderType: TypeOpenAICompat,
		OpenAIChat: OpenAIChatOptions{
			MaxTokensField: OpenAIChatMaxCompletionTokens,
		},
	})
	registerProvider(ProviderSpec{
		Order: 20,
		Entry: CatalogEntry{
			ID:              "openai-codex",
			Name:            "OpenAI Codex Subscription",
			Type:            TypeOpenAICodex,
			Implemented:     true,
			RequiresBaseURL: false,
			Capabilities: Capabilities{
				ModelDiscovery:  true,
				ReasoningEffort: true,
				ToolCalling:     true,
			},
			DefaultBaseURL: "https://chatgpt.com/backend-api/codex",
			DefaultModel:   DefaultOpenAICodexModel,
			Notes:          "Uses ChatGPT/Codex subscription OAuth through the Codex backend, not OpenAI API-key billing.",
		},
		Auth:                ProviderAuthOAuth,
		Transport:           ProviderTransportOpenAIResponses,
		RuntimeProviderType: TypeOpenAICodex,
	})
}
