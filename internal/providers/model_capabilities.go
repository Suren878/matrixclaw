package providers

import "strings"

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

type ModelCapabilityInput struct {
	ProviderID   string
	ProviderType string
	ModelID      string
}

type ModelCapabilitySet struct {
	ProviderCapabilities   Capabilities
	RuntimeCapabilities    ModelCapabilities
	ReasoningEfforts       []string
	DefaultReasoningEffort string
}

func ResolveModelCapabilities(input ModelCapabilityInput) ModelCapabilitySet {
	providerID := NormalizeProviderID(input.ProviderID)
	providerType := NormalizeProviderType(input.ProviderType)
	modelID := strings.ToLower(strings.TrimSpace(input.ModelID))

	providerCapabilities := ProviderCapabilities(providerID, providerType)
	runtimeCapabilities := runtimeCapabilitiesFromProvider(providerCapabilities, providerType)
	reasoningEfforts := reasoningEffortsForProviderModel(providerID, providerType, modelID, runtimeCapabilities)
	if runtimeCapabilities.ReasoningEffort && len(reasoningEfforts) == 0 {
		runtimeCapabilities.ReasoningEffort = false
		runtimeCapabilities.ReasoningMode = ReasoningModeNone
		runtimeCapabilities.ReasoningWithTools = false
	}
	defaultReasoning := ""
	if len(reasoningEfforts) > 0 {
		defaultReasoning = DefaultReasoningEffort
	}

	return ModelCapabilitySet{
		ProviderCapabilities:   providerCapabilitiesFromRuntime(providerCapabilities, runtimeCapabilities),
		RuntimeCapabilities:    runtimeCapabilities,
		ReasoningEfforts:       reasoningEfforts,
		DefaultReasoningEffort: defaultReasoning,
	}
}

func ProviderRuntimeCapabilities(providerID string, providerType string) ModelCapabilities {
	return ModelRuntimeCapabilities(providerID, providerType, "")
}

func ModelRuntimeCapabilities(providerID string, providerType string, modelID string) ModelCapabilities {
	return ResolveModelCapabilities(ModelCapabilityInput{
		ProviderID:   providerID,
		ProviderType: providerType,
		ModelID:      modelID,
	}).RuntimeCapabilities
}

func runtimeCapabilitiesFromProvider(providerCapabilities Capabilities, providerType string) ModelCapabilities {
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
	case TypeOpenAICompat, TypeOpenAICodex:
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

func providerCapabilitiesFromRuntime(providerCapabilities Capabilities, runtimeCapabilities ModelCapabilities) Capabilities {
	return Capabilities{
		ModelDiscovery:  providerCapabilities.ModelDiscovery,
		ReasoningEffort: runtimeCapabilities.ReasoningEffort,
		ToolCalling:     runtimeCapabilities.ToolCalling,
		NormalizeModel:  providerCapabilities.NormalizeModel,
	}
}

func reasoningEffortsForProviderModel(providerID string, providerType string, modelID string, capabilities ModelCapabilities) []string {
	if !capabilities.ReasoningEffort {
		return nil
	}
	if providerID == "openai" || providerID == "openai-codex" {
		if modelID != "" && !openAIModelSupportsReasoningEffort(modelID) {
			return nil
		}
		return copyStrings(openAIReasoningEfforts)
	}
	return ReasoningEfforts()
}

func openAIModelSupportsReasoningEffort(modelID string) bool {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	if modelID == "" {
		return true
	}
	for _, prefix := range []string{"gpt-5", "o1", "o3", "o4"} {
		if strings.HasPrefix(modelID, prefix) {
			return true
		}
	}
	return false
}
