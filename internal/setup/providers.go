package setup

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func normalizeConfig(cfg Config) Config {
	cfg.Assistant = normalizeAssistantConfig(cfg.Assistant)
	cfg.Daemon.HTTPAddr = strings.TrimSpace(cfg.Daemon.HTTPAddr)
	cfg.Daemon.DBPath = strings.TrimSpace(cfg.Daemon.DBPath)
	cfg.Daemon.Timezone = strings.TrimSpace(cfg.Daemon.Timezone)
	cfg.Daemon.APIToken = strings.TrimSpace(cfg.Daemon.APIToken)
	if cfg.Daemon.Timezone == "" {
		cfg.Daemon.Timezone = defaultTimezone()
	}
	configured := make([]ProviderConfig, 0, len(cfg.Providers))
	seen := make(map[string]struct{}, len(cfg.Providers))

	appendProvider := func(provider ProviderConfig) {
		normalized, ok := normalizeProviderConfig(provider)
		if !ok {
			return
		}
		if _, exists := seen[normalized.ID]; exists {
			return
		}
		seen[normalized.ID] = struct{}{}
		configured = append(configured, normalized)
	}

	for _, provider := range cfg.Providers {
		appendProvider(provider)
	}

	cfg.Providers = configured
	if active, ok := activeProviderFromConfig(cfg); ok {
		cfg.ActiveProviderID = active.ID
	} else {
		cfg.ActiveProviderID = ""
	}
	cfg.Modules = normalizeModulesConfig(cfg.Modules)
	cfg.Version = CurrentVersion
	return cfg
}

func normalizeModulesConfig(modules ModulesConfig) ModulesConfig {
	if len(modules.ExternalAgents) == 0 {
		modules.ExternalAgents = nil
		return modules
	}
	normalized := make(map[string]ExternalAgentConfig, len(modules.ExternalAgents))
	for id, cfg := range modules.ExternalAgents {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		cfg.Path = strings.TrimSpace(cfg.Path)
		normalized[id] = cfg
	}
	if len(normalized) == 0 {
		modules.ExternalAgents = nil
		return modules
	}
	modules.ExternalAgents = normalized
	return modules
}

func normalizeAssistantConfig(assistant AssistantConfig) AssistantConfig {
	assistant.Name = strings.TrimSpace(assistant.Name)
	assistant.SystemPrompt = strings.TrimSpace(assistant.SystemPrompt)
	assistant.CustomInstructions = strings.TrimSpace(assistant.CustomInstructions)
	if assistant.Name == "" {
		assistant.Name = "matrixclaw"
	}
	if assistant.SystemPrompt == "" {
		assistant.SystemPrompt = DefaultAssistantSystemPrompt()
	}
	return assistant
}

func DefaultAssistantSystemPrompt() string {
	return "You are matrixclaw, a coding agent running through matrixclaw's background runtime with terminal, Telegram, tools, approvals, sessions, automation tasks, reminders, and SQLite-backed state. Prefer precise changes, preserve user files, ask for approval before destructive actions, and keep responses concise. In user-facing explanations, call the background runtime matrixclaw architect instead of daemon. When users ask for reminders or scheduled work, resolve exact time and timezone before creating automation."
}

func normalizeProviderConfig(provider ProviderConfig) (ProviderConfig, bool) {
	provider.ID = providers.NormalizeProviderID(provider.ID)
	provider.CatalogID = providers.NormalizeProviderID(provider.CatalogID)
	provider.Type = providers.NormalizeOptionalProviderType(provider.Type)
	provider.Name = strings.TrimSpace(provider.Name)
	provider.APIKey = strings.TrimSpace(provider.APIKey)
	provider.APIKeyEnv = strings.TrimSpace(provider.APIKeyEnv)
	provider.BaseURL = strings.TrimSpace(provider.BaseURL)
	provider.Model = strings.TrimSpace(provider.Model)
	provider.ToolUseMode = providers.NormalizeOptionalToolUseMode(provider.ToolUseMode)

	if provider.ID == "" {
		return ProviderConfig{}, false
	}
	if provider.CatalogID == "" {
		provider.CatalogID = provider.ID
	}
	if option, ok := lookupProviderOption(provider.CatalogID); ok {
		if provider.Name == "" {
			provider.Name = option.Name
		}
		if provider.Type == "" {
			provider.Type = option.Type
		}
		if provider.APIKeyEnv == "" {
			provider.APIKeyEnv = option.APIKeyEnv
		}
		if provider.BaseURL == "" {
			provider.BaseURL = option.DefaultBaseURL
		}
		if provider.Model == "" {
			provider.Model = option.DefaultModel
		}
	}
	provider.Model = providers.NormalizeModelID(provider.CatalogID, provider.Type, provider.Model)
	provider.ReasoningEffort = providers.NormalizeReasoningEffortForModel(provider.CatalogID, provider.Type, provider.Model, provider.ReasoningEffort)
	return provider, true
}

func activeProviderFromConfig(cfg Config) (ProviderConfig, bool) {
	activeID := providers.NormalizeProviderID(cfg.ActiveProviderID)
	if activeID != "" {
		for _, provider := range cfg.Providers {
			if sameProvider(provider.ID, activeID) {
				return provider, true
			}
		}
	}
	if len(cfg.Providers) == 0 {
		return ProviderConfig{}, false
	}
	return cfg.Providers[0], true
}

func ActiveProviderConfig(cfg Config) (ProviderConfig, bool) {
	cfg = normalizeConfig(cfg)
	if cfg.ActiveProviderID == "" {
		return ProviderConfig{}, false
	}
	for _, provider := range cfg.Providers {
		if sameProvider(provider.ID, cfg.ActiveProviderID) {
			return provider, true
		}
	}
	return ProviderConfig{}, false
}

func findProviderConfig(cfg Config, providerID string) (ProviderConfig, bool) {
	cfg = normalizeConfig(cfg)
	providerID = providers.NormalizeProviderID(providerID)
	for _, provider := range cfg.Providers {
		if sameProvider(provider.ID, providerID) {
			return provider, true
		}
	}
	return ProviderConfig{}, false
}

func ProviderDraftConfigured(provider ProviderDraft) bool {
	if strings.TrimSpace(provider.APIKey) != "" {
		return true
	}
	if strings.TrimSpace(providerAPIKeyFromEnvName(providerDraftAPIKeyEnvName(provider))) != "" {
		return true
	}
	return provider.HasStoredAPIKey && strings.TrimSpace(provider.StoredAPIKeyPreview) != ""
}

func FindProviderDraft(draft Draft, providerID string) (ProviderDraft, bool) {
	providerID = providers.NormalizeProviderID(providerID)
	for _, provider := range draft.Providers {
		if sameProvider(provider.ID, providerID) {
			return provider, true
		}
	}
	return ProviderDraft{}, false
}

func UpsertProviderDraft(draft Draft, provider ProviderDraft) Draft {
	provider.ID = providers.NormalizeProviderID(provider.ID)
	provider.CatalogID = providers.NormalizeProviderID(provider.CatalogID)
	next := make([]ProviderDraft, 0, len(draft.Providers)+1)
	replaced := false
	for _, existing := range draft.Providers {
		if sameProvider(existing.ID, provider.ID) {
			next = append(next, provider)
			replaced = true
			continue
		}
		next = append(next, existing)
	}
	if !replaced {
		next = append(next, provider)
	}
	draft.Providers = next
	return draft
}

func DeleteProviderDraft(draft Draft, providerID string) Draft {
	next := make([]ProviderDraft, 0, len(draft.Providers))
	for _, provider := range draft.Providers {
		if sameProvider(provider.ID, providerID) {
			continue
		}
		next = append(next, provider)
	}
	draft.Providers = next
	if sameProvider(draft.ActiveProviderID, providerID) {
		draft.ActiveProviderID = ""
		for _, provider := range ConfiguredProviders(draft) {
			draft.ActiveProviderID = provider.ID
			break
		}
	}
	return draft
}

func builtInProviderOptions() []ProviderOption {
	entries := providers.AvailableCatalog()
	options := make([]ProviderOption, 0, len(entries))
	for _, entry := range entries {
		options = append(options, ProviderOption{
			ID:              entry.ID,
			Name:            entry.Name,
			Type:            entry.Type,
			Implemented:     entry.Implemented,
			RequiresBaseURL: entry.RequiresBaseURL,
			Capabilities:    entry.Capabilities,
			DefaultBaseURL:  entry.DefaultBaseURL,
			BaseURLOptions:  append([]providers.BaseURLOption(nil), entry.BaseURLOptions...),
			DefaultModel:    entry.DefaultModel,
			APIKeyEnv:       entry.APIKeyEnv,
			Notes:           entry.Notes,
		})
	}
	return options
}

func ConfiguredProviders(draft Draft) []ProviderDraft {
	configured := make([]ProviderDraft, 0, len(draft.Providers))
	for _, provider := range draft.Providers {
		if ProviderDraftConfigured(provider) {
			configured = append(configured, provider)
		}
	}
	return configured
}

func availableBuiltInProviders(draft Draft, options []ProviderOption) []ProviderOption {
	available := make([]ProviderOption, 0, len(options))
	for _, option := range options {
		if _, ok := FindProviderDraft(draft, option.ID); ok {
			continue
		}
		available = append(available, option)
	}
	return available
}

func isCustomProviderDraft(provider ProviderDraft) bool {
	catalogID := providers.NormalizeProviderID(provider.CatalogID)
	if catalogID == "" {
		catalogID = providers.NormalizeProviderID(provider.ID)
	}
	_, builtIn := lookupProviderOption(catalogID)
	return !builtIn
}

func draftProviderFromOption(option ProviderOption) ProviderDraft {
	return ProviderDraft{
		ID:              option.ID,
		CatalogID:       option.ID,
		Name:            option.Name,
		Type:            option.Type,
		APIKeyEnv:       option.APIKeyEnv,
		BaseURL:         option.DefaultBaseURL,
		Model:           option.DefaultModel,
		ReasoningEffort: providers.DefaultReasoningEffortForModel(option.ID, option.Type, option.DefaultModel),
		HasStoredAPIKey: false,
	}
}

func draftProviderFromConfig(provider ProviderConfig) ProviderDraft {
	hasStoredAPIKey := strings.TrimSpace(provider.APIKey) != ""
	draft := ProviderDraft{
		ID:                  provider.ID,
		CatalogID:           provider.CatalogID,
		Name:                provider.Name,
		Type:                provider.Type,
		APIKey:              "",
		APIKeyEnv:           provider.APIKeyEnv,
		BaseURL:             provider.BaseURL,
		Model:               provider.Model,
		ToolUseMode:         provider.ToolUseMode,
		ReasoningEffort:     provider.ReasoningEffort,
		HasStoredAPIKey:     hasStoredAPIKey,
		StoredAPIKeyPreview: MaskSecret(provider.APIKey),
	}
	if provider.MaxOutputTokens > 0 {
		draft.MaxOutputTokens = strconv.FormatInt(provider.MaxOutputTokens, 10)
	}
	return draft
}

func ProviderConfigWithResolvedAPIKey(provider ProviderConfig) (ProviderConfig, bool) {
	resolved, ok := ResolvedProviderAPIKey(provider)
	provider.APIKey = resolved
	return provider, ok
}

func ResolvedProviderAPIKey(provider ProviderConfig) (string, bool) {
	if apiKey := strings.TrimSpace(provider.APIKey); apiKey != "" {
		return apiKey, true
	}
	if apiKey := strings.TrimSpace(providerAPIKeyFromEnvName(providerAPIKeyEnvName(provider))); apiKey != "" {
		return apiKey, true
	}
	return "", false
}

func ProviderAPIKeyPreview(provider ProviderConfig) string {
	if apiKey := strings.TrimSpace(provider.APIKey); apiKey != "" {
		return MaskSecret(apiKey)
	}
	envName := providerAPIKeyEnvName(provider)
	if envName == "" || strings.TrimSpace(providerAPIKeyFromEnvName(envName)) == "" {
		return ""
	}
	return "env:" + envName
}

func providerAPIKeyEnvName(provider ProviderConfig) string {
	return providerAPIKeyEnvNameFor(provider.APIKeyEnv, provider.CatalogID, provider.ID, provider.Type)
}

func providerDraftAPIKeyEnvName(provider ProviderDraft) string {
	return providerAPIKeyEnvNameFor(provider.APIKeyEnv, provider.CatalogID, provider.ID, provider.Type)
}

func providerAPIKeyEnvNameFor(explicit string, catalogID string, providerID string, providerType string) string {
	if envName := strings.TrimSpace(explicit); envName != "" {
		return envName
	}
	catalogID = providers.NormalizeProviderID(firstNonEmptyTrimmed(catalogID, providerID))
	if option, ok := lookupProviderOption(catalogID); ok {
		return strings.TrimSpace(option.APIKeyEnv)
	}
	switch providers.NormalizeOptionalProviderType(providerType) {
	case providers.TypeAnthropic:
		return "ANTHROPIC_API_KEY"
	case providers.TypeOpenAICompat:
		return "OPENAI_COMPAT_API_KEY"
	default:
		return ""
	}
}

func providerAPIKeyFromEnvName(envName string) string {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(envName))
}

func newCustomDraftProvider(baseType string, existing []ProviderDraft) ProviderDraft {
	baseType = providers.NormalizeOptionalProviderType(baseType)
	name := "Custom OpenAI-Compatible"
	baseURL := "https://api.example.com/v1"
	apiKeyEnv := "OPENAI_COMPAT_API_KEY"
	idBase := "custom-openai-compatible"
	if baseType == providers.TypeAnthropic {
		name = "Custom Anthropic-Compatible"
		baseURL = "https://api.example.com/v1"
		apiKeyEnv = "ANTHROPIC_API_KEY"
		idBase = "custom-anthropic-compatible"
	}
	return ProviderDraft{
		ID:              uniqueProviderID(idBase, existing),
		Name:            name,
		Type:            baseType,
		APIKeyEnv:       apiKeyEnv,
		BaseURL:         baseURL,
		Model:           "",
		ReasoningEffort: providers.DefaultReasoningEffortForModel("", baseType, ""),
		HasStoredAPIKey: false,
	}
}

func uniqueProviderID(base string, existing []ProviderDraft) string {
	base = providers.NormalizeProviderID(base)
	if base == "" {
		base = "custom-provider"
	}
	taken := make(map[string]struct{}, len(existing))
	for _, provider := range existing {
		taken[providers.NormalizeProviderID(provider.ID)] = struct{}{}
	}
	if _, exists := taken[base]; !exists {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, exists := taken[candidate]; !exists {
			return candidate
		}
	}
}

func lookupProviderOption(providerID string) (ProviderOption, bool) {
	providerID = providers.NormalizeProviderID(providerID)
	for _, option := range builtInProviderOptions() {
		if option.ID == providerID {
			return option, true
		}
	}
	return ProviderOption{}, false
}
