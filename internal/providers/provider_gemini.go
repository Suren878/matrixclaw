package providers

func init() {
	registerProvider(ProviderSpec{
		Order: 310,
		Entry: CatalogEntry{
			ID:              "gemini",
			Name:            "Google Gemini",
			Type:            TypeGemini,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities: Capabilities{
				ModelDiscovery: true,
				NormalizeModel: true,
				ToolCalling:    true,
			},
			DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta",
			DefaultModel:   DefaultGeminiModel,
			APIKeyEnv:      "GEMINI_API_KEY",
			Notes:          "Uses Google's native Gemini generateContent API.",
		},
		Aliases:   []string{"google-gemini"},
		Auth:      ProviderAuthAPIKey,
		Transport: ProviderTransportGeminiNative,
	})
}
