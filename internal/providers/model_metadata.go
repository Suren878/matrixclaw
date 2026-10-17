package providers

import (
	"sort"
	"strings"
	"sync"
)

type ModelMetadataSource string

const (
	ModelMetadataSourceLiveCatalog ModelMetadataSource = "live_catalog"
	ModelMetadataSourceStaticRule  ModelMetadataSource = "static_rule"
	ModelMetadataSourceFallback    ModelMetadataSource = "fallback"
)

type ModelMetadata struct {
	ID                     string              `json:"id,omitempty"`
	ContextWindow          int                 `json:"context_window,omitempty"`
	MaxOutputTokens        int                 `json:"max_output_tokens,omitempty"`
	ToolCalling            bool                `json:"tool_calling,omitempty"`
	ImageInput             bool                `json:"image_input,omitempty"`
	ReasoningEffort        bool                `json:"reasoning_effort,omitempty"`
	ReasoningEfforts       []string            `json:"reasoning_efforts,omitempty"`
	DefaultReasoningEffort string              `json:"default_reasoning_effort,omitempty"`
	SupportedParameters    []string            `json:"supported_parameters,omitempty"`
	Source                 ModelMetadataSource `json:"source,omitempty"`
}

// ModelMetadataRegistration is what a live listing reports for one model; nil
// and zero fields are unknown.
type ModelMetadataRegistration struct {
	ContextWindow       int      `json:"context_window,omitempty"`
	MaxOutputTokens     int      `json:"max_output_tokens,omitempty"`
	ToolCalling         *bool    `json:"tool_calling,omitempty"`
	ImageInput          *bool    `json:"image_input,omitempty"`
	ReasoningEffort     *bool    `json:"reasoning_effort,omitempty"`
	ReasoningEfforts    []string `json:"reasoning_efforts,omitempty"`
	SupportedParameters []string `json:"supported_parameters,omitempty"`
}

// learnedMetadata is what live listings reported, by provider ID and then by
// the model ID as listed (vendor prefix kept, lower case): one provider's
// listing never answers for another's model of the same name.
var learnedMetadata = struct {
	sync.RWMutex
	values map[string]map[string]ModelMetadataRegistration
}{values: map[string]map[string]ModelMetadataRegistration{}}

func learnedModelKey(modelID string) string {
	return strings.ToLower(strings.TrimSpace(modelID))
}

// RegisterModelMetadata records what providerID's live listing reported for modelID.
func RegisterModelMetadata(providerID string, modelID string, metadata ModelMetadataRegistration) {
	providerID, modelID = NormalizeProviderID(providerID), learnedModelKey(modelID)
	if providerID == "" || modelID == "" || !modelMetadataKnown(metadata) {
		return
	}
	learnedMetadata.Lock()
	defer learnedMetadata.Unlock()
	models := learnedMetadata.values[providerID]
	if models == nil {
		models = map[string]ModelMetadataRegistration{}
		learnedMetadata.values[providerID] = models
	}
	models[modelID] = mergeModelMetadata(models[modelID], metadata)
}

func lookupLearnedMetadata(providerID string, modelID string) (ModelMetadataRegistration, bool) {
	learnedMetadata.RLock()
	defer learnedMetadata.RUnlock()
	metadata, ok := learnedMetadata.values[NormalizeProviderID(providerID)][learnedModelKey(modelID)]
	return metadata, ok
}

// ResolveModelMetadata is what a provider's model can do: the provider's live
// listing first, then the static rules for its type, then the fallback.
func ResolveModelMetadata(providerID string, providerType string, modelID string) ModelMetadata {
	providerID = NormalizeProviderID(providerID)
	providerType = NormalizeProviderType(providerType)
	modelID = NormalizeModelID(providerID, providerType, modelID)

	policy := PolicyForProvider(providerID, providerType)
	capabilities := ModelCapabilities{
		ToolCalling:     policy.Capabilities.ToolCalling,
		ImageInput:      resolveStaticImageInput(providerID, policy.Type, modelID, policy.Capabilities.ImageInput),
		ReasoningEffort: policy.Capabilities.ReasoningEffort,
	}
	learned, learnedOK := lookupLearnedMetadata(providerID, modelID)
	if learnedOK {
		capabilities = applyLearnedCapabilities(capabilities, learned)
	}

	reasoningEfforts := cleanReasoningEfforts(learned.ReasoningEfforts)
	if len(reasoningEfforts) == 0 {
		reasoningEfforts = reasoningEffortsForProviderModel(providerID, modelID, capabilities)
	}
	if len(reasoningEfforts) == 0 {
		capabilities.ReasoningEffort = false
	}
	defaultReasoning := ""
	if len(reasoningEfforts) > 0 {
		defaultReasoning = DefaultReasoningEffort
	}

	source := ModelMetadataSourceLiveCatalog
	contextWindow := learned.ContextWindow
	if contextWindow <= 0 {
		var staticSource ModelMetadataSource
		contextWindow, staticSource = resolveStaticContextWindowTokens(providerID, policy.Type, modelID)
		if !learnedOK {
			source = staticSource
		}
	}

	return ModelMetadata{
		ID:                     modelID,
		ContextWindow:          contextWindow,
		MaxOutputTokens:        learned.MaxOutputTokens,
		ToolCalling:            capabilities.ToolCalling,
		ImageInput:             capabilities.ImageInput,
		ReasoningEffort:        capabilities.ReasoningEffort,
		ReasoningEfforts:       reasoningEfforts,
		DefaultReasoningEffort: defaultReasoning,
		SupportedParameters:    copyStrings(learned.SupportedParameters),
		Source:                 source,
	}
}

func mergeModelMetadata(existing ModelMetadataRegistration, next ModelMetadataRegistration) ModelMetadataRegistration {
	if next.ContextWindow > 0 {
		existing.ContextWindow = next.ContextWindow
	}
	if next.MaxOutputTokens > 0 {
		existing.MaxOutputTokens = next.MaxOutputTokens
	}
	if next.ToolCalling != nil {
		value := *next.ToolCalling
		existing.ToolCalling = &value
	}
	if next.ImageInput != nil {
		value := *next.ImageInput
		existing.ImageInput = &value
	}
	if next.ReasoningEffort != nil {
		value := *next.ReasoningEffort
		existing.ReasoningEffort = &value
	}
	if len(next.ReasoningEfforts) > 0 {
		existing.ReasoningEfforts = cleanReasoningEfforts(next.ReasoningEfforts)
	}
	if len(next.SupportedParameters) > 0 {
		existing.SupportedParameters = cleanModelParameters(next.SupportedParameters)
	}
	return existing
}

func modelMetadataKnown(metadata ModelMetadataRegistration) bool {
	return metadata.ContextWindow > 0 ||
		metadata.MaxOutputTokens > 0 ||
		metadata.ToolCalling != nil ||
		metadata.ImageInput != nil ||
		metadata.ReasoningEffort != nil ||
		len(metadata.ReasoningEfforts) > 0 ||
		len(metadata.SupportedParameters) > 0
}

func applyLearnedCapabilities(capabilities ModelCapabilities, metadata ModelMetadataRegistration) ModelCapabilities {
	if metadata.ToolCalling != nil {
		capabilities.ToolCalling = *metadata.ToolCalling
	}
	if metadata.ImageInput != nil {
		capabilities.ImageInput = *metadata.ImageInput
	}
	if metadata.ReasoningEffort != nil {
		capabilities.ReasoningEffort = *metadata.ReasoningEffort
	}
	return capabilities
}

func cleanReasoningEfforts(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = NormalizeReasoningEffort(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func cleanModelParameters(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
