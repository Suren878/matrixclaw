package providers

import "strings"

// resolveStaticImageInput is deliberately fail-closed. OpenAI-compatible APIs
// do not have one universal capability endpoint, so an image is allowed only
// when the selected model belongs to a known multimodal family. Live model
// metadata can override this result through ModelMetadataRegistration.
func resolveStaticImageInput(providerID string, providerType string, modelID string, providerDefault bool) bool {
	providerID = NormalizeProviderID(providerID)
	providerType = NormalizeProviderType(providerType)
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	if modelID == "" {
		return providerDefault
	}

	for _, marker := range []string{
		"vision",
		"pixtral",
		"llava",
		"qwen-vl",
		"qwen2-vl",
		"qwen2.5-vl",
		"qwen3-vl",
		"qwen3.5-vl",
		"-omni",
		"/omni",
		"glm-4v",
		"glm-4.5v",
		"grok-2-vision",
	} {
		if strings.Contains(modelID, marker) {
			return true
		}
	}

	switch providerType {
	case TypeOpenAICodex:
		return hasAnyModelPrefix(modelID, "gpt-4o", "gpt-4.1", "gpt-5", "o1", "o3", "o4")
	case TypeAnthropic:
		return strings.HasPrefix(modelID, "claude-")
	case TypeGemini:
		return strings.HasPrefix(modelID, "gemini-")
	case TypeOpenAICompat:
		switch providerID {
		case "openai":
			return hasAnyModelPrefix(modelID, "gpt-4o", "gpt-4.1", "gpt-5", "o1", "o3", "o4")
		case "xai":
			return hasAnyModelPrefix(modelID, "grok-4")
		}
	}
	return false
}

func hasAnyModelPrefix(modelID string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(modelID, prefix) {
			return true
		}
	}
	return false
}
