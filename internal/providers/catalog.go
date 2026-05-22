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
	CatalogID       string          `json:"catalog_id,omitempty"`
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
	specs := ProviderSpecs()
	entries := make([]CatalogEntry, 0, len(specs))
	for _, spec := range specs {
		entries = append(entries, spec.Entry)
	}
	return entries
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
	spec, ok := ProviderSpecByID(providerID)
	if !ok {
		return CatalogEntry{}, false
	}
	return spec.Entry, true
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
	return PolicyForProvider(providerID, providerType).Capabilities
}

func defaultCapabilitiesForCustomProviderType(providerType string) Capabilities {
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
