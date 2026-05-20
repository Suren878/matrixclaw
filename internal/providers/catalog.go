package providers

import "strings"

const (
	TypeOpenAICompat = "openai-compatible"
	TypeOpenAICodex  = "openai-codex"
	TypeAnthropic    = "anthropic-compatible"
	TypeGemini       = "gemini"

	DefaultOpenAICompatModel = "gpt-5.4-mini"
	DefaultOpenAICodexModel  = "gpt-5.4"
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
			Notes:          "Standard OpenAI endpoint through the generic OpenAI-compatible path.",
		},
		{
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
			Notes:           "OpenAI-compatible gateway for many hosted models; OpenRouter-specific reasoning output is not mapped by the generic runtime.",
		},
		{
			ID:              "xai",
			Name:            "xAI / Grok",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true, ToolCalling: true},
			DefaultBaseURL:  "https://api.x.ai/v1",
			DefaultModel:    "grok-4.3",
			APIKeyEnv:       "XAI_API_KEY",
			Notes:           "xAI Grok OpenAI-compatible endpoint.",
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
			Notes:           "Use this when Kimi is exposed through an OpenAI-compatible endpoint.",
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
	return ReasoningEffortsForModel(providerID, providerType, "")
}

func ReasoningEffortsForModel(providerID string, providerType string, modelID string) []string {
	return ResolveModelCapabilities(ModelCapabilityInput{
		ProviderID:   providerID,
		ProviderType: providerType,
		ModelID:      modelID,
	}).ReasoningEfforts
}

func DefaultReasoningEffortForProvider(providerID string, providerType string) string {
	return DefaultReasoningEffortForModel(providerID, providerType, "")
}

func DefaultReasoningEffortForModel(providerID string, providerType string, modelID string) string {
	return ResolveModelCapabilities(ModelCapabilityInput{
		ProviderID:   providerID,
		ProviderType: providerType,
		ModelID:      modelID,
	}).DefaultReasoningEffort
}

func NormalizeReasoningEffortForProvider(providerID string, providerType string, value string) string {
	return NormalizeReasoningEffortForModel(providerID, providerType, "", value)
}

func NormalizeReasoningEffortForModel(providerID string, providerType string, modelID string, value string) string {
	efforts := ReasoningEffortsForModel(providerID, providerType, modelID)
	if len(efforts) == 0 {
		return ""
	}
	value = strings.ToLower(strings.TrimSpace(value))
	for _, effort := range efforts {
		if value == effort {
			return effort
		}
	}
	return DefaultReasoningEffortForModel(providerID, providerType, modelID)
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
	providerType = NormalizeProviderType(providerType)
	if entry, ok := CatalogEntryByID(providerID); ok {
		return entry.Capabilities
	}
	switch providerType {
	case TypeOpenAICompat, TypeOpenAICodex:
		return Capabilities{ModelDiscovery: true, ReasoningEffort: providerType == TypeOpenAICodex, ToolCalling: true}
	case TypeAnthropic:
		return Capabilities{ModelDiscovery: true}
	case TypeGemini:
		return Capabilities{ModelDiscovery: true, NormalizeModel: true, ToolCalling: true}
	default:
		return Capabilities{}
	}
}
