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

var reasoningEfforts = []string{"low", DefaultReasoningEffort, "high"}

type CatalogEntry struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Type            string       `json:"type"`
	Implemented     bool         `json:"implemented"`
	RequiresBaseURL bool         `json:"requires_base_url"`
	Capabilities    Capabilities `json:"capabilities"`
	DefaultBaseURL  string       `json:"default_base_url,omitempty"`
	DefaultModel    string       `json:"default_model,omitempty"`
	APIKeyEnv       string       `json:"api_key_env,omitempty"`
	ConfigPath      string       `json:"config_path,omitempty"`
	Notes           string       `json:"notes,omitempty"`
}

type Capabilities struct {
	ModelDiscovery  bool `json:"model_discovery,omitempty"`
	ReasoningEffort bool `json:"reasoning_effort,omitempty"`
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
			Capabilities:    Capabilities{ModelDiscovery: true},
			DefaultBaseURL:  "https://api.deepseek.com/v1",
			DefaultModel:    "deepseek-chat",
			APIKeyEnv:       "DEEPSEEK_API_KEY",
			ConfigPath:      "internal/providers/ai/openaicompat/configs/deepseek/config.example.json",
			Notes:           "Known-good third-party OpenAI-compatible gateway configuration.",
		},
		{
			ID:              "xai",
			Name:            "xAI",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true},
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
			Capabilities:    Capabilities{ModelDiscovery: true},
			DefaultBaseURL:  "https://api.z.ai/api/paas/v4",
			DefaultModel:    "glm-5",
			APIKeyEnv:       "ZAI_API_KEY",
			ConfigPath:      "internal/providers/ai/openaicompat/configs/zai/config.example.json",
			Notes:           "Uses the OpenAI-compatible path; the coding endpoint is also possible.",
		},
		{
			ID:              "kimi",
			Name:            "Kimi",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true},
			DefaultModel:    "YOUR_KIMI_MODEL",
			APIKeyEnv:       "KIMI_API_KEY",
			ConfigPath:      "internal/providers/ai/openaicompat/configs/kimi/config.example.json",
			Notes:           "Use this when Kimi is exposed through an OpenAI-compatible endpoint.",
		},
		{
			ID:              "aihubmix",
			Name:            "AiHubMix",
			Type:            TypeOpenAICompat,
			Implemented:     true,
			RequiresBaseURL: true,
			Capabilities:    Capabilities{ModelDiscovery: true},
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

func NormalizeReasoningEffort(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, effort := range reasoningEfforts {
		if value == effort {
			return effort
		}
	}
	return ""
}

func ReasoningEfforts() []string {
	out := make([]string, len(reasoningEfforts))
	copy(out, reasoningEfforts)
	return out
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
	if effort := NormalizeReasoningEffort(value); effort != "" {
		return effort
	}
	return DefaultReasoningEffort
}

func providerCapabilities(providerID string, providerType string) Capabilities {
	providerID = NormalizeProviderID(providerID)
	providerType = strings.TrimSpace(providerType)
	if entry, ok := CatalogEntryByID(providerID); ok {
		return entry.Capabilities
	}
	switch providerType {
	case TypeOpenAICompat:
		return Capabilities{ModelDiscovery: true}
	case TypeAnthropic:
		return Capabilities{ModelDiscovery: true}
	case TypeGemini:
		return Capabilities{ModelDiscovery: true, NormalizeModel: true}
	default:
		return Capabilities{}
	}
}
