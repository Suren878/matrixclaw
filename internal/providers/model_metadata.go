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
	ParallelToolCalls      bool                `json:"parallel_tool_calls,omitempty"`
	ImageInput             bool                `json:"image_input,omitempty"`
	ReasoningEffort        bool                `json:"reasoning_effort,omitempty"`
	ReasoningMode          ReasoningMode       `json:"reasoning_mode,omitempty"`
	ReasoningWithTools     bool                `json:"reasoning_with_tools,omitempty"`
	ReasoningEfforts       []string            `json:"reasoning_efforts,omitempty"`
	DefaultReasoningEffort string              `json:"default_reasoning_effort,omitempty"`
	SupportedParameters    []string            `json:"supported_parameters,omitempty"`
	Source                 ModelMetadataSource `json:"source,omitempty"`
	ContextSource          ModelMetadataSource `json:"context_source,omitempty"`
	CapabilitySource       ModelMetadataSource `json:"capability_source,omitempty"`
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

var modelMetadataOverrides = struct {
	sync.RWMutex
	values map[string]ModelMetadataRegistration
}{values: map[string]ModelMetadataRegistration{}}

func RegisterModelMetadata(providerID string, providerType string, modelID string, metadata ModelMetadataRegistration) {
	if !modelMetadataKnown(metadata) {
		return
	}
	modelMetadataOverrides.Lock()
	defer modelMetadataOverrides.Unlock()
	for _, key := range contextWindowKeys(providerID, providerType, modelID) {
		if key != "" {
			modelMetadataOverrides.values[key] = mergeModelMetadata(modelMetadataOverrides.values[key], metadata)
		}
	}
}

func ResolveModelMetadata(providerID string, providerType string, modelID string) ModelMetadata {
	providerID = NormalizeProviderID(providerID)
	providerType = NormalizeProviderType(providerType)
	modelID = strings.TrimSpace(modelID)
	normalizedModelID := NormalizeModelID(providerID, providerType, modelID)

	policy := PolicyForProvider(providerID, providerType)
	providerCapabilities := policy.Capabilities
	runtimeCapabilities := runtimeCapabilitiesFromProvider(providerCapabilities, policy.RuntimeProviderType)
	runtimeCapabilities.ImageInput = resolveStaticImageInput(providerID, policy.RuntimeProviderType, normalizedModelID, providerCapabilities.ImageInput)

	liveMetadata, liveOK := lookupCachedModelMetadata(providerID, providerType, normalizedModelID)
	if liveOK {
		runtimeCapabilities = applyCachedCapabilities(runtimeCapabilities, liveMetadata, policy.RuntimeProviderType)
	}

	reasoningEfforts := cleanReasoningEfforts(liveMetadata.ReasoningEfforts)
	if len(reasoningEfforts) == 0 {
		reasoningEfforts = reasoningEffortsForProviderModel(providerID, normalizedModelID, runtimeCapabilities)
	}
	if runtimeCapabilities.ReasoningEffort && len(reasoningEfforts) == 0 {
		runtimeCapabilities.ReasoningEffort = false
		runtimeCapabilities.ReasoningMode = ReasoningModeNone
		runtimeCapabilities.ReasoningWithTools = false
	}
	defaultReasoning := ""
	if len(reasoningEfforts) > 0 {
		defaultReasoning = DefaultReasoningEffort
	}

	contextWindow := liveMetadata.ContextWindow
	var contextSource ModelMetadataSource
	if contextWindow > 0 {
		contextSource = ModelMetadataSourceLiveCatalog
	} else {
		contextWindow, contextSource = resolveStaticContextWindowTokens(providerID, policy.RuntimeProviderType, normalizedModelID)
	}

	capabilitySource := ModelMetadataSourceStaticRule
	if liveOK && (liveMetadata.ToolCalling != nil || liveMetadata.ImageInput != nil || liveMetadata.ReasoningEffort != nil || len(liveMetadata.ReasoningEfforts) > 0 || len(liveMetadata.SupportedParameters) > 0) {
		capabilitySource = ModelMetadataSourceLiveCatalog
	}
	source := metadataSource(contextSource, capabilitySource, liveOK)

	return ModelMetadata{
		ID:                     normalizedModelID,
		ContextWindow:          contextWindow,
		MaxOutputTokens:        liveMetadata.MaxOutputTokens,
		ToolCalling:            runtimeCapabilities.ToolCalling,
		ParallelToolCalls:      runtimeCapabilities.ParallelToolCalls,
		ImageInput:             runtimeCapabilities.ImageInput,
		ReasoningEffort:        runtimeCapabilities.ReasoningEffort,
		ReasoningMode:          runtimeCapabilities.ReasoningMode,
		ReasoningWithTools:     runtimeCapabilities.ReasoningWithTools,
		ReasoningEfforts:       reasoningEfforts,
		DefaultReasoningEffort: defaultReasoning,
		SupportedParameters:    copyStrings(liveMetadata.SupportedParameters),
		Source:                 source,
		ContextSource:          contextSource,
		CapabilitySource:       capabilitySource,
	}
}

func lookupCachedModelMetadata(providerID string, providerType string, modelID string) (ModelMetadataRegistration, bool) {
	var out ModelMetadataRegistration
	found := false
	for _, key := range contextWindowKeys(providerID, providerType, modelID) {
		modelMetadataOverrides.RLock()
		metadata, ok := modelMetadataOverrides.values[key]
		modelMetadataOverrides.RUnlock()
		if !ok {
			continue
		}
		out = mergeModelMetadata(out, metadata)
		found = true
	}
	return out, found
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

func applyCachedCapabilities(capabilities ModelCapabilities, metadata ModelMetadataRegistration, providerType string) ModelCapabilities {
	if metadata.ToolCalling != nil {
		capabilities.ToolCalling = *metadata.ToolCalling
		capabilities.ParallelToolCalls = *metadata.ToolCalling
	}
	if metadata.ImageInput != nil {
		capabilities.ImageInput = *metadata.ImageInput
	}
	if metadata.ReasoningEffort != nil {
		capabilities.ReasoningEffort = *metadata.ReasoningEffort
		if *metadata.ReasoningEffort {
			capabilities.ReasoningMode = reasoningModeForProvider(providerType)
			capabilities.ReasoningWithTools = capabilities.ToolCalling
		} else {
			capabilities.ReasoningMode = ReasoningModeNone
			capabilities.ReasoningWithTools = false
		}
	}
	if !capabilities.ToolCalling {
		capabilities.ParallelToolCalls = false
		capabilities.ReasoningWithTools = false
	}
	return capabilities
}

func reasoningModeForProvider(providerType string) ReasoningMode {
	switch NormalizeProviderType(providerType) {
	case TypeGemini:
		return ReasoningModeGeminiThinking
	case TypeOpenAICompat, TypeOpenAICodex:
		return ReasoningModeOpenAIEffort
	default:
		return ReasoningModeNone
	}
}

func metadataSource(contextSource ModelMetadataSource, capabilitySource ModelMetadataSource, liveOK bool) ModelMetadataSource {
	if liveOK || contextSource == ModelMetadataSourceLiveCatalog || capabilitySource == ModelMetadataSourceLiveCatalog {
		return ModelMetadataSourceLiveCatalog
	}
	if contextSource == ModelMetadataSourceFallback {
		return ModelMetadataSourceFallback
	}
	return ModelMetadataSourceStaticRule
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
