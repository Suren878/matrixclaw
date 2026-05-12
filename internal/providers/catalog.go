package providers

import "strings"

const (
	TypeOpenAICompat = "openai-compatible"
	TypeAnthropic    = "anthropic-compatible"
	TypeGemini       = "gemini"

	DefaultOpenAICompatModel = "gpt-5.4-mini"
	DefaultAnthropicModel    = "claude-sonnet-4-5"
	DefaultGeminiModel       = "gemini-2.5-flash"
	DefaultReasoningEffort   = "medium"
)

const (
	ReasoningEffortNone    = "none"
	ReasoningEffortMinimal = "minimal"
	ReasoningEffortLow     = "low"
	ReasoningEffortMedium  = DefaultReasoningEffort
	ReasoningEffortHigh    = "high"
	ReasoningEffortXHigh   = "xhigh"
)

var reasoningEfforts = []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}
var openAIReasoningEfforts = []string{
	ReasoningEffortNone,
	ReasoningEffortMinimal,
	ReasoningEffortLow,
	ReasoningEffortMedium,
	ReasoningEffortHigh,
	ReasoningEffortXHigh,
}

type CatalogEntry struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Type            string          `json:"type"`
	Implemented     bool            `json:"implemented"`
	RequiresBaseURL bool            `json:"requires_base_url"`
	Capabilities    Capabilities    `json:"capabilities"`
	DefaultBaseURL  string          `json:"default_base_url,omitempty"`
	BaseURLOptions  []BaseURLOption `json:"base_url_options,omitempty"`
	DefaultModel    string          `json:"default_model,omitempty"`
	APIKeyEnv       string          `json:"api_key_env,omitempty"`
	ConfigPath      string          `json:"config_path,omitempty"`
	Notes           string          `json:"notes,omitempty"`
}

type BaseURLOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Capabilities struct {
	ModelDiscovery  bool `json:"model_discovery,omitempty"`
	ReasoningEffort bool `json:"reasoning_effort,omitempty"`
	ToolCalling     bool `json:"tool_calling,omitempty"`
	NormalizeModel  bool `json:"normalize_model,omitempty"`
}

func Catalog() []CatalogEntry {
	return []CatalogEntry{
		{
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
			ConfigPath:     "internal/providers/ai/openaicompat/configs/openai/config.example.json",
			Notes:          "Standard OpenAI endpoint through the generic OpenAI-compatible path.",
		},
		{
			ID:              "deepseek",
			Name:            "DeepSeek",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true, ToolCalling: true},
			DefaultBaseURL:  "https://api.deepseek.com/v1",
			DefaultModel:    "deepseek-chat",
			APIKeyEnv:       "DEEPSEEK_API_KEY",
			ConfigPath:      "internal/providers/ai/openaicompat/configs/deepseek/config.example.json",
			Notes:           "Known-good third-party OpenAI-compatible gateway configuration.",
		},
		{
			ID:              "openrouter",
			Name:            "OpenRouter",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true, ToolCalling: true},
			DefaultBaseURL:  "https://openrouter.ai/api/v1",
			DefaultModel:    "qwen/qwen3-coder-next",
			APIKeyEnv:       "OPENROUTER_API_KEY",
			ConfigPath:      "internal/providers/ai/openaicompat/configs/openrouter/config.example.json",
			Notes:           "OpenAI-compatible gateway for many hosted models; OpenRouter-specific reasoning output is not mapped by the generic runtime.",
		},
		{
			ID:              "xai",
			Name:            "xAI",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true, ToolCalling: true},
			DefaultBaseURL:  "https://api.x.ai/v1",
			DefaultModel:    "grok-4",
			APIKeyEnv:       "XAI_API_KEY",
			ConfigPath:      "internal/providers/ai/openaicompat/configs/xai/config.example.json",
			Notes:           "Works when the endpoint exposes an OpenAI-compatible API.",
		},
		{
			ID:              "zai",
			Name:            "Z.AI / GLM",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true, ToolCalling: true},
			DefaultBaseURL:  "https://api.z.ai/api/paas/v4",
			DefaultModel:    "glm-5",
			APIKeyEnv:       "ZAI_API_KEY",
			ConfigPath:      "internal/providers/ai/openaicompat/configs/zai/config.example.json",
			Notes:           "Uses the OpenAI-compatible path; the coding endpoint is also possible.",
		},
		{
			ID:              "minimax",
			Name:            "MiniMax",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true, ToolCalling: true},
			DefaultBaseURL:  "https://api.minimax.io/v1",
			DefaultModel:    "MiniMax-M2.7",
			APIKeyEnv:       "MINIMAX_API_KEY",
			ConfigPath:      "internal/providers/ai/openaicompat/configs/minimax/config.example.json",
			Notes:           "MiniMax OpenAI-compatible endpoint with model listing and tool use support.",
		},
		{
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
			ConfigPath:   "internal/providers/ai/openaicompat/configs/qwen/config.example.json",
			Notes:        "Alibaba Cloud Model Studio OpenAI-compatible endpoint. API keys are region-specific; select the endpoint matching the key region.",
		},
		{
			ID:              "kimi",
			Name:            "Kimi",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true, ToolCalling: true},
			DefaultModel:    "YOUR_KIMI_MODEL",
			APIKeyEnv:       "KIMI_API_KEY",
			ConfigPath:      "internal/providers/ai/openaicompat/configs/kimi/config.example.json",
			Notes:           "Use this when Kimi is exposed through an OpenAI-compatible endpoint.",
		},
		{
			ID:              "kimi-subscription",
			Name:            "Kimi (Subscription)",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ToolCalling: true},
			DefaultBaseURL:  "https://api.kimi.com/coding/v1",
			DefaultModel:    "kimi-for-coding",
			APIKeyEnv:       "KIMI_CODE_API_KEY",
			ConfigPath:      "internal/providers/ai/openaicompat/configs/kimi-subscription/config.example.json",
			Notes:           "Kimi Code / Subscription OpenAI-compatible coding endpoint with separate keys and quota from Moonshot Open Platform.",
		},
		{
			ID:              "aihubmix",
			Name:            "AiHubMix",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true, ToolCalling: true},
			DefaultModel:    "YOUR_MODEL_ID",
			APIKeyEnv:       "AIHUBMIX_API_KEY",
			ConfigPath:      "internal/providers/ai/openaicompat/configs/aihubmix/config.example.json",
			Notes:           "Generic OpenAI-compatible gateway; exact base URL depends on the account.",
		},
		{
			ID:              "anthropic",
			Name:            "Anthropic",
			Type:            TypeAnthropic,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true},
			DefaultBaseURL:  "https://api.anthropic.com/v1",
			DefaultModel:    DefaultAnthropicModel,
			APIKeyEnv:       "ANTHROPIC_API_KEY",
			ConfigPath:      "internal/providers/ai/anthropiccompat/configs/anthropic/config.example.json",
			Notes:           "Native Anthropic path with Messages API semantics.",
		},
		{
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
			ConfigPath:     "internal/providers/ai/gemini/configs/gemini/config.example.json",
			Notes:          "Uses Google's native Gemini generateContent API.",
		},
	}
}

func AvailableCatalog() []CatalogEntry {
	all := Catalog()
	available := make([]CatalogEntry, 0, len(all))
	for _, entry := range all {
		if entry.Implemented {
			available = append(available, entry)
		}
	}
	return available
}

func CatalogEntryByID(providerID string) (CatalogEntry, bool) {
	providerID = NormalizeProviderID(providerID)
	if providerID == "" {
		return CatalogEntry{}, false
	}
	for _, entry := range Catalog() {
		if NormalizeProviderID(entry.ID) == providerID {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}

func NormalizeProviderID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizeModelID(providerID string, providerType string, modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if providerCapabilities(providerID, providerType).NormalizeModel {
		modelID = strings.TrimPrefix(modelID, "models/")
	}
	return modelID
}

func ProviderCapabilities(providerID string, providerType string) Capabilities {
	return providerCapabilities(providerID, providerType)
}

func NormalizeReasoningEffort(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, effort := range allReasoningEfforts() {
		if value == effort {
			return effort
		}
	}
	return ""
}

func ReasoningEfforts() []string {
	return copyStrings(reasoningEfforts)
}

func ReasoningEffortsForProvider(providerID string, providerType string) []string {
	if !providerCapabilities(providerID, providerType).ReasoningEffort {
		return nil
	}
	if NormalizeProviderID(providerID) == "openai" {
		return copyStrings(openAIReasoningEfforts)
	}
	return ReasoningEfforts()
}

func DefaultReasoningEffortForProvider(providerID string, providerType string) string {
	if !providerCapabilities(providerID, providerType).ReasoningEffort {
		return ""
	}
	return DefaultReasoningEffort
}

func NormalizeReasoningEffortForProvider(providerID string, providerType string, value string) string {
	if !providerCapabilities(providerID, providerType).ReasoningEffort {
		return ""
	}
	value = strings.ToLower(strings.TrimSpace(value))
	for _, effort := range ReasoningEffortsForProvider(providerID, providerType) {
		if value == effort {
			return effort
		}
	}
	return DefaultReasoningEffort
}

func allReasoningEfforts() []string {
	seen := map[string]bool{}
	efforts := make([]string, 0, len(reasoningEfforts)+len(openAIReasoningEfforts))
	for _, effort := range append(copyStrings(reasoningEfforts), openAIReasoningEfforts...) {
		if effort == "" || seen[effort] {
			continue
		}
		seen[effort] = true
		efforts = append(efforts, effort)
	}
	return efforts
}

func copyStrings(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func providerCapabilities(providerID string, providerType string) Capabilities {
	providerID = NormalizeProviderID(providerID)
	providerType = strings.TrimSpace(providerType)
	if entry, ok := CatalogEntryByID(providerID); ok {
		return entry.Capabilities
	}
	switch providerType {
	case TypeOpenAICompat:
		return Capabilities{ModelDiscovery: true, ToolCalling: true}
	case TypeAnthropic:
		return Capabilities{ModelDiscovery: true}
	case TypeGemini:
		return Capabilities{ModelDiscovery: true, NormalizeModel: true, ToolCalling: true}
	default:
		return Capabilities{}
	}
}
