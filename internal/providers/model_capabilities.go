package providers

type ReasoningMode string

const (
	ReasoningModeNone           ReasoningMode = ""
	ReasoningModeOpenAIEffort   ReasoningMode = "openai_effort"
	ReasoningModeGeminiThinking ReasoningMode = "gemini_thinking"
)

type ModelCapabilities struct {
	ToolCalling        bool
	ToolSchemaDialect  ToolSchemaDialect
	ParallelToolCalls  bool
	ReasoningEffort    bool
	ReasoningMode      ReasoningMode
	ReasoningWithTools bool
	ThoughtSignatures  bool
	NormalizeModel     bool
}

// ProviderRuntimeCapabilities resolves runtime behavior from the provider catalog.
// The catalog is currently provider-level; model-specific capabilities should be
// added here only when the catalog carries real model data for them.
func ProviderRuntimeCapabilities(providerID string, providerType string) ModelCapabilities {
	providerID = NormalizeProviderID(providerID)
	providerType = normalizeProviderTypeForProfile(providerType)

	providerCapabilities := ProviderCapabilities(providerID, providerType)
	capabilities := ModelCapabilities{
		ToolCalling:       providerCapabilities.ToolCalling,
		ToolSchemaDialect: ToolSchemaJSONSchema,
		ParallelToolCalls: providerCapabilities.ToolCalling,
		ReasoningEffort:   providerCapabilities.ReasoningEffort,
		NormalizeModel:    providerCapabilities.NormalizeModel,
	}

	switch providerType {
	case TypeGemini:
		capabilities.ToolSchemaDialect = ToolSchemaGemini
		capabilities.ReasoningMode = ReasoningModeGeminiThinking
	case TypeOpenAICompat:
		if providerCapabilities.ReasoningEffort {
			capabilities.ReasoningMode = ReasoningModeOpenAIEffort
			capabilities.ReasoningWithTools = providerCapabilities.ToolCalling
		}
	}

	if !capabilities.ToolCalling {
		capabilities.ParallelToolCalls = false
		capabilities.ReasoningWithTools = false
		capabilities.ThoughtSignatures = false
	}
	return capabilities
}
