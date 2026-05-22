package providers

import (
	"strings"
	"sync"
)

const DefaultFallbackContextWindowTokens = 256_000

var codexSubscriptionContextWindows = []contextWindowPattern{
	{Pattern: "gpt-5.3-codex-spark", Tokens: 128_000},
	{Pattern: "gpt-5.1-codex-max", Tokens: 272_000},
	{Pattern: "gpt-5.1-codex-mini", Tokens: 272_000},
	{Pattern: "gpt-5.3-codex", Tokens: 272_000},
	{Pattern: "gpt-5.2-codex", Tokens: 272_000},
	{Pattern: "gpt-5.4-mini", Tokens: 272_000},
	{Pattern: "gpt-5.5", Tokens: 272_000},
	{Pattern: "gpt-5.4", Tokens: 272_000},
	{Pattern: "gpt-5.2", Tokens: 272_000},
	{Pattern: "gpt-5", Tokens: 272_000},
}

var providerContextWindows = []contextWindowPattern{
	{Pattern: "gpt-5.5", Tokens: 1_050_000},
	{Pattern: "gpt-5.4-nano", Tokens: 400_000},
	{Pattern: "gpt-5.4-mini", Tokens: 400_000},
	{Pattern: "gpt-5.4", Tokens: 1_050_000},
	{Pattern: "gpt-5.3-codex-spark", Tokens: 128_000},
	{Pattern: "gpt-5.1-chat", Tokens: 128_000},
	{Pattern: "gpt-5", Tokens: 400_000},
	{Pattern: "gpt-4.1", Tokens: 1_047_576},
	{Pattern: "gpt-4", Tokens: 128_000},
	{Pattern: "claude-opus-4.7", Tokens: 1_000_000},
	{Pattern: "claude-opus-4-7", Tokens: 1_000_000},
	{Pattern: "claude-opus-4.6", Tokens: 1_000_000},
	{Pattern: "claude-opus-4-6", Tokens: 1_000_000},
	{Pattern: "claude-sonnet-4.6", Tokens: 1_000_000},
	{Pattern: "claude-sonnet-4-6", Tokens: 1_000_000},
	{Pattern: "claude", Tokens: 200_000},
	{Pattern: "gemini", Tokens: 1_048_576},
	{Pattern: "deepseek-v4-pro", Tokens: 1_000_000},
	{Pattern: "deepseek-v4-flash", Tokens: 1_000_000},
	{Pattern: "deepseek-chat", Tokens: 1_000_000},
	{Pattern: "deepseek-reasoner", Tokens: 1_000_000},
	{Pattern: "deepseek", Tokens: 128_000},
	{Pattern: "grok-4-1-fast", Tokens: 2_000_000},
	{Pattern: "grok-4-fast", Tokens: 2_000_000},
	{Pattern: "grok-4.20", Tokens: 2_000_000},
	{Pattern: "grok-4.3", Tokens: 1_000_000},
	{Pattern: "grok-4", Tokens: 256_000},
	{Pattern: "grok-3", Tokens: 131_072},
	{Pattern: "grok-2", Tokens: 131_072},
	{Pattern: "grok", Tokens: 131_072},
	{Pattern: "qwen3.6-plus", Tokens: 1_048_576},
	{Pattern: "qwen3-coder-plus", Tokens: 1_000_000},
	{Pattern: "qwen3-coder", Tokens: 262_144},
	{Pattern: "qwen", Tokens: 131_072},
	{Pattern: "kimi", Tokens: 262_144},
	{Pattern: "minimax", Tokens: 204_800},
	{Pattern: "glm", Tokens: 202_752},
	{Pattern: "llama", Tokens: 131_072},
}

type contextWindowPattern struct {
	Pattern string
	Tokens  int
}

var contextWindowOverrides = struct {
	sync.RWMutex
	values map[string]int
}{values: map[string]int{}}

var contextWindowCacheOnce sync.Once

func RegisterContextWindowTokens(providerID string, providerType string, modelID string, tokens int) {
	if tokens <= 0 {
		return
	}
	contextWindowCacheOnce.Do(loadContextWindowCache)
	for _, key := range contextWindowKeys(providerID, providerType, modelID) {
		if key == "" {
			continue
		}
		contextWindowOverrides.Lock()
		contextWindowOverrides.values[key] = tokens
		contextWindowOverrides.Unlock()
		modelMetadataOverrides.Lock()
		existing := modelMetadataOverrides.values[key]
		existing.ContextWindow = tokens
		modelMetadataOverrides.values[key] = existing
		modelMetadataOverrides.Unlock()
	}
	saveContextWindowCache()
}

func ResolveContextWindowTokens(providerID string, providerType string, modelID string) int {
	return ResolveModelMetadata(providerID, providerType, modelID).ContextWindow
}

func resolveStaticContextWindowTokens(providerID string, providerType string, modelID string) (int, ModelMetadataSource) {
	model := normalizeContextModelID(modelID)
	if model == "" {
		return 0, ""
	}
	providerID = NormalizeProviderID(providerID)
	providerType = NormalizeProviderType(providerType)
	if providerID == "openai-codex" || providerType == TypeOpenAICodex {
		if tokens := matchContextWindow(model, codexSubscriptionContextWindows); tokens > 0 {
			return tokens, ModelMetadataSourceStaticRule
		}
		return DefaultFallbackContextWindowTokens, ModelMetadataSourceFallback
	}
	if tokens := matchContextWindow(model, providerContextWindows); tokens > 0 {
		return tokens, ModelMetadataSourceStaticRule
	}
	return DefaultFallbackContextWindowTokens, ModelMetadataSourceFallback
}

func lookupContextWindowOverride(providerID string, providerType string, modelID string) int {
	for _, key := range contextWindowKeys(providerID, providerType, modelID) {
		contextWindowOverrides.RLock()
		tokens := contextWindowOverrides.values[key]
		contextWindowOverrides.RUnlock()
		if tokens > 0 {
			return tokens
		}
	}
	return 0
}

func contextWindowKeys(providerID string, providerType string, modelID string) []string {
	model := normalizeContextModelID(modelID)
	if model == "" {
		return nil
	}
	providerID = NormalizeProviderID(providerID)
	providerType = NormalizeOptionalProviderType(providerType)
	keys := make([]string, 0, 2)
	if providerID != "" {
		keys = append(keys, contextWindowProviderKey(providerID, model))
	}
	if providerType != "" {
		keys = append(keys, contextWindowTypeKey(providerType, model))
	}
	return keys
}

func contextWindowProviderKey(providerID string, modelID string) string {
	providerID = NormalizeProviderID(providerID)
	modelID = normalizeContextModelID(modelID)
	if providerID == "" || modelID == "" {
		return ""
	}
	return providerID + "\x00" + modelID
}

func contextWindowTypeKey(providerType string, modelID string) string {
	providerType = NormalizeOptionalProviderType(providerType)
	modelID = normalizeContextModelID(modelID)
	if providerType == "" || modelID == "" {
		return ""
	}
	return "type:" + providerType + "\x00" + modelID
}

func normalizeContextModelID(modelID string) string {
	model := strings.ToLower(strings.TrimSpace(modelID))
	if model == "" {
		return ""
	}
	if idx := strings.LastIndex(model, "/"); idx >= 0 && idx+1 < len(model) {
		model = model[idx+1:]
	}
	return model
}

func matchContextWindow(model string, patterns []contextWindowPattern) int {
	for _, pattern := range patterns {
		if strings.Contains(model, strings.ToLower(pattern.Pattern)) {
			return pattern.Tokens
		}
	}
	return 0
}
