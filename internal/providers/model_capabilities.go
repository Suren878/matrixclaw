package providers

import "strings"

type ReasoningMode string

const (
	ReasoningModeNone              ReasoningMode = ""
	ReasoningModeOpenAIEffort      ReasoningMode = "openai_effort"
	ReasoningModeAnthropicThinking ReasoningMode = "anthropic_thinking"
	ReasoningModeGeminiThinking    ReasoningMode = "gemini_thinking"
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

func ResolveModelCapabilities(providerID string, providerType string, modelID string) ModelCapabilities {
	providerID = NormalizeProviderID(providerID)
	providerType = normalizeProviderTypeForProfile(providerType)
	_ = strings.TrimSpace(modelID)

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
		capabilities.ThoughtSignatures = providerCapabilities.ToolCalling
	case TypeAnthropic:
		capabilities.ReasoningMode = ReasoningModeAnthropicThinking
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
