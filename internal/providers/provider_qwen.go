package providers

func init() {
	registerProvider(ProviderSpec{
		Order: 170,
		Entry: CatalogEntry{
			ID:              "qwen",
			Name:            "Qwen / DashScope",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true, ToolCalling: true},
			DefaultBaseURL:  "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
			BaseURLOptions: []BaseURLOption{
				{ID: "singapore", Name: "Singapore / International", URL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"},
				{ID: "us-virginia", Name: "US (Virginia)", URL: "https://dashscope-us.aliyuncs.com/compatible-mode/v1"},
				{ID: "china-beijing", Name: "China (Beijing)", URL: "https://dashscope.aliyuncs.com/compatible-mode/v1"},
				{ID: "hong-kong", Name: "Hong Kong (China)", URL: "https://cn-hongkong.dashscope.aliyuncs.com/compatible-mode/v1"},
			},
			DefaultModel: "qwen-plus",
			APIKeyEnv:    "DASHSCOPE_API_KEY",
			Notes:        "Alibaba Cloud Model Studio OpenAI-compatible endpoint. API keys are region-specific; select the endpoint matching the key region.",
		},
		Aliases:   []string{"dashscope", "alibaba", "alibaba-cloud", "qwen-dashscope"},
		Auth:      ProviderAuthAPIKey,
		Transport: ProviderTransportOpenAIChat,
	})
}
