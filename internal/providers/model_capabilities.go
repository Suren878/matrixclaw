package providers

import "strings"

// ModelCapabilities is what a runtime's model accepts.
type ModelCapabilities struct {
	ToolCalling     bool
	ImageInput      bool
	ReasoningEffort bool
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
	modelID := strings.TrimSpace(input.ModelID)
	metadata := ResolveModelMetadata(providerID, providerType, modelID)
	policy := PolicyForProvider(providerID, providerType)
	providerCapabilities := policy.Capabilities
	runtimeCapabilities := ModelCapabilities{
		ToolCalling:     metadata.ToolCalling,
		ImageInput:      metadata.ImageInput,
		ReasoningEffort: metadata.ReasoningEffort,
	}

	return ModelCapabilitySet{
		ProviderCapabilities:   providerCapabilitiesFromRuntime(providerCapabilities, runtimeCapabilities),
		RuntimeCapabilities:    runtimeCapabilities,
		ReasoningEfforts:       metadata.ReasoningEfforts,
		DefaultReasoningEffort: metadata.DefaultReasoningEffort,
	}
}

func providerCapabilitiesFromRuntime(providerCapabilities Capabilities, runtimeCapabilities ModelCapabilities) Capabilities {
	return Capabilities{
		ModelDiscovery:  providerCapabilities.ModelDiscovery,
		ReasoningEffort: runtimeCapabilities.ReasoningEffort,
		ToolCalling:     runtimeCapabilities.ToolCalling,
		ImageInput:      runtimeCapabilities.ImageInput,
		NormalizeModel:  providerCapabilities.NormalizeModel,
	}
}

func reasoningEffortsForProviderModel(providerID string, modelID string, capabilities ModelCapabilities) []string {
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
