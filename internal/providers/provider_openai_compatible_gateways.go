package providers

func init() {
	specs := []ProviderSpec{
		openAICompatibleProvider(30, "deepseek", "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "DEEPSEEK_API_KEY", "Known-good third-party OpenAI-compatible gateway configuration.", "deepseek-chat"),
		openAICompatibleProvider(40, "openrouter", "OpenRouter", "https://openrouter.ai/api/v1", "qwen/qwen3-coder-next", "OPENROUTER_API_KEY", "OpenAI-compatible gateway for many hosted models; OpenRouter-specific reasoning output is not mapped by the generic runtime.", "or"),
		openAICompatibleProvider(50, "ai-gateway", "Vercel AI Gateway", "https://ai-gateway.vercel.sh/v1", "google/gemini-3-flash", "AI_GATEWAY_API_KEY", "Vercel AI Gateway OpenAI-compatible endpoint for routing across supported hosted models.", "vercel", "vercel-ai-gateway", "ai_gateway"),
		openAICompatibleProvider(70, "nvidia", "NVIDIA NIM", "https://integrate.api.nvidia.com/v1", "nvidia/llama-3.3-70b-instruct", "NVIDIA_API_KEY", "NVIDIA NIM OpenAI-compatible endpoint.", "nvidia-nim"),
		openAICompatibleProvider(80, "huggingface", "Hugging Face", "https://router.huggingface.co/v1", "Qwen/Qwen3.5-72B-Instruct", "HF_TOKEN", "Hugging Face router OpenAI-compatible endpoint.", "hf", "hugging-face", "huggingface-hub"),
		openAICompatibleProvider(90, "novita", "NovitaAI", "https://api.novita.ai/openai/v1", "deepseek/deepseek-v3-0324", "NOVITA_API_KEY", "NovitaAI OpenAI-compatible endpoint.", "novita-ai", "novitaai"),
		openAICompatibleProvider(100, "gmi", "GMI Cloud", "https://api.gmi-serving.com/v1", "zai-org/GLM-5.1-FP8", "GMI_API_KEY", "GMI Cloud OpenAI-compatible endpoint.", "gmi-cloud", "gmicloud"),
		openAICompatibleProvider(110, "stepfun", "StepFun", "https://api.stepfun.ai/step_plan/v1", "step-3.5-flash", "STEPFUN_API_KEY", "StepFun OpenAI-compatible endpoint.", "step", "stepfun-coding-plan"),
		openAICompatibleProvider(120, "ollama-cloud", "Ollama Cloud", "https://ollama.com/v1", "nemotron-3-nano:30b", "OLLAMA_API_KEY", "Ollama Cloud OpenAI-compatible endpoint.", "ollama_cloud"),
		openAICompatibleProvider(130, "kilocode", "Kilo Code", "https://api.kilo.ai/api/gateway", "google/gemini-3-flash-preview", "KILOCODE_API_KEY", "Kilo Code OpenAI-compatible gateway.", "kilo-code", "kilo", "kilo-gateway"),
		openAICompatibleProvider(140, "xai", "xAI / Grok", "https://api.x.ai/v1", "grok-4.3", "XAI_API_KEY", "xAI Grok OpenAI-compatible endpoint.", "grok", "x-ai", "x.ai"),
		openAICompatibleProvider(150, "zai", "Z.AI / GLM", "https://api.z.ai/api/paas/v4", "glm-5", "ZAI_API_KEY", "Uses the OpenAI-compatible path; the coding endpoint is also possible.", "glm", "z-ai", "z.ai", "zhipu"),
		openAICompatibleProvider(160, "minimax", "MiniMax", "https://api.minimax.io/v1", "MiniMax-M2.7", "MINIMAX_API_KEY", "MiniMax OpenAI-compatible endpoint with model listing and tool use support.", "mini-max"),
		openAICompatibleProvider(180, "kimi", "Kimi", "https://api.moonshot.ai/v1", "kimi-k2-turbo-preview", "KIMI_API_KEY", "Kimi / Moonshot OpenAI-compatible endpoint.", "moonshot", "kimi-coding"),
		openAICompatibleProvider(190, "aihubmix", "AiHubMix", "", "YOUR_MODEL_ID", "AIHUBMIX_API_KEY", "Generic OpenAI-compatible gateway; exact base URL depends on the account."),
	}
	for i := range specs {
		switch specs[i].Entry.ID {
		case "ai-gateway":
			specs[i].OpenAIChat.DefaultHeaders = map[string]string{
				"HTTP-Referer": "https://github.com/Suren878/matrixclaw",
				"X-Title":      "Matrixclaw",
			}
			specs[i].PublicModelCatalog = true
		case "gmi":
			specs[i].OpenAIChat.DefaultHeaders = map[string]string{
				"User-Agent": "Matrixclaw",
			}
		case "kimi":
			specs[i].OpenAIChat.DefaultHeaders = map[string]string{
				"User-Agent": "Matrixclaw",
			}
		case "openrouter":
			specs[i].OpenAIChat.DefaultHeaders = map[string]string{
				"HTTP-Referer": "https://github.com/Suren878/matrixclaw",
				"X-Title":      "Matrixclaw",
			}
			specs[i].PublicModelCatalog = true
		case "huggingface", "kilocode", "novita", "nvidia", "ollama-cloud":
			specs[i].PublicModelCatalog = true
		}
	}
	for _, spec := range specs {
		registerProvider(spec)
	}
}
