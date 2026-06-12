package setup

import (
	"fmt"
	"os"
	"sort"
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
	modules.TextToSpeech = normalizeVoiceModuleConfig("tts", modules.TextToSpeech)
	modules.SpeechToText = normalizeVoiceModuleConfig("stt", modules.SpeechToText)
	modules.RealtimeVoice = normalizeVoiceModuleConfig("realtime_voice", modules.RealtimeVoice)
	modules.MCP = normalizeMCPConfig(modules.MCP)
	modules.Browser = normalizeBrowserConfig(modules.Browser)
	modules.Skills = normalizeSkillsConfig(modules.Skills)
	if len(modules.ExternalAgents) == 0 {
		modules.ExternalAgents = nil
		return modules
	}
	ids := make([]string, 0, len(modules.ExternalAgents))
	for id := range modules.ExternalAgents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	normalized := make(map[string]ExternalAgentConfig, len(modules.ExternalAgents))
	for _, rawID := range ids {
		cfg := modules.ExternalAgents[rawID]
		id := normalizeExternalAgentID(rawID)
		if id == "" {
			continue
		}
		cfg.Path = strings.TrimSpace(cfg.Path)
		if _, exists := normalized[id]; exists && id != strings.ToLower(strings.TrimSpace(rawID)) {
			continue
		}
		normalized[id] = cfg
	}
	if len(normalized) == 0 {
		modules.ExternalAgents = nil
		return modules
	}
	modules.ExternalAgents = normalized
	return modules
}

func normalizeSkillsConfig(cfg SkillsConfig) SkillsConfig {
	if strings.TrimSpace(cfg.TrustPolicy) == "" {
		cfg.TrustPolicy = "quarantine"
	}
	if strings.TrimSpace(cfg.SelfImprove) == "" {
		cfg.SelfImprove = "drafts"
	}
	if !cfg.Enabled {
		cfg.Enabled = true
	}
	if !cfg.AutoInvoke {
		cfg.AutoInvoke = true
	}
	return cfg
}

func normalizeMCPConfig(cfg MCPConfig) MCPConfig {
	servers := make([]MCPServerConfig, 0, len(cfg.Servers))
	seen := map[string]struct{}{}
	for _, server := range cfg.Servers {
		server.ID = normalizeMCPID(server.ID)
		server.Name = strings.TrimSpace(server.Name)
		server.Transport = normalizeMCPTransport(server.Transport)
		server.Command = strings.TrimSpace(server.Command)
		server.Endpoint = strings.TrimRight(strings.TrimSpace(server.Endpoint), "/")
		server.ToolPrefix = normalizeMCPID(server.ToolPrefix)
		if server.ToolPrefix == "" {
			server.ToolPrefix = server.ID
		}
		if server.TimeoutSeconds < 0 {
			server.TimeoutSeconds = 0
		}
		server.Args = trimStringSlice(server.Args)
		server.Env = trimStringMap(server.Env)
		if server.ID == "" {
			continue
		}
		if _, ok := seen[server.ID]; ok {
			continue
		}
		if server.Transport == "http" {
			if server.Endpoint == "" {
				continue
			}
		} else if server.Command == "" {
			continue
		}
		seen[server.ID] = struct{}{}
		servers = append(servers, server)
	}
	cfg.Servers = servers
	return cfg
}

func normalizeMCPID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func normalizeMCPTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "http", "streamable_http", "streamable-http":
		return "http"
	default:
		return "stdio"
	}
}

func trimStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func trimStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
	return "You are matrixclaw, a personal AI operator in matrixclaw's local background runtime across terminal and Telegram durable sessions. Use available tools only; risky mutations require approval. Keep replies concise, preserve user files, and update visible plans for multi-step work. Use skills when helpful: skill_search finds trusted workflows, skill_use activates one for the session, and skill_manage creates/edits skills only after approval; for AI-created skills, discuss and revise the draft in chat, then call skill_manage create only after explicit user confirmation. Explain slash-command control-plane features when useful, but do not claim you can run them unless exposed as tools. For reminders or scheduled work, resolve exact time and timezone first. In user-facing text call the background runtime matrixclaw architect, not daemon."
}

func normalizeProviderConfig(provider ProviderConfig) (ProviderConfig, bool) {
	provider.ID = providers.NormalizeProviderID(provider.ID)
	provider.CatalogID = providers.NormalizeProviderID(provider.CatalogID)
	provider.Type = providers.NormalizeOptionalProviderType(provider.Type)
	provider.Name = strings.TrimSpace(provider.Name)
	provider.APIKey = normalizeProviderAPIKey(provider.APIKey)
	provider.APIKeyEnv = strings.TrimSpace(provider.APIKeyEnv)
	provider.BaseURL = strings.TrimSpace(provider.BaseURL)
	provider.Model = strings.TrimSpace(provider.Model)
	if provider.ContextWindow < 0 {
		provider.ContextWindow = 0
	}
	provider.ToolUseMode = providers.NormalizeOptionalToolUseMode(provider.ToolUseMode)

	if provider.ID == "" {
		return ProviderConfig{}, false
	}
	if provider.CatalogID == "" {
		provider.CatalogID = provider.ID
	}
	if policy := providers.PolicyForProvider(provider.CatalogID, provider.Type); policy.Known {
		provider.ID = policy.CatalogID
		provider.CatalogID = policy.CatalogID
		if provider.Name == "" {
			provider.Name = policy.Name
		}
		if provider.Type == "" {
			provider.Type = policy.Type
		}
		if provider.APIKeyEnv == "" {
			provider.APIKeyEnv = policy.APIKeyEnv
		}
		if provider.BaseURL == "" {
			provider.BaseURL = policy.DefaultBaseURL
		}
		if provider.Model == "" {
			provider.Model = policy.DefaultModel
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
	if !providerPolicyForDraft(provider).RequiresAPIKey {
		return strings.TrimSpace(provider.Model) != ""
	}
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
	specs := providers.ProviderSpecs()
	options := make([]ProviderOption, 0, len(specs))
	for _, spec := range specs {
		policy := providers.PolicyForProvider(spec.Entry.ID, spec.Entry.Type)
		if !policy.Implemented {
			continue
		}
		options = append(options, providerOptionFromPolicy(policy))
	}
	return options
}

func providerOptionFromPolicy(policy providers.ProviderPolicy) ProviderOption {
	return ProviderOption{
		ID:              policy.CatalogID,
		Name:            policy.Name,
		Type:            policy.Type,
		Implemented:     policy.Implemented,
		RequiresBaseURL: policy.RequiresBaseURL,
		Capabilities:    policy.Capabilities,
		DefaultBaseURL:  policy.DefaultBaseURL,
		BaseURLOptions:  append([]providers.BaseURLOption(nil), policy.BaseURLOptions...),
		DefaultModel:    policy.DefaultModel,
		APIKeyEnv:       policy.APIKeyEnv,
		Notes:           policy.Notes,
	}
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
	return !providerPolicyForDraft(provider).Known
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
	if provider.ContextWindow > 0 {
		draft.ContextWindow = strconv.Itoa(provider.ContextWindow)
	}
	if provider.MaxOutputTokens > 0 {
		draft.MaxOutputTokens = strconv.FormatInt(provider.MaxOutputTokens, 10)
	}
	return draft
}

func ProviderConfigWithResolvedAPIKey(provider ProviderConfig) (ProviderConfig, bool) {
	if !providerPolicyForConfig(provider).RequiresAPIKey {
		provider.APIKey = ""
		return provider, true
	}
	resolved, ok := ResolvedProviderAPIKey(provider)
	provider.APIKey = resolved
	return provider, ok
}

func ResolvedProviderAPIKey(provider ProviderConfig) (string, bool) {
	if apiKey := normalizeProviderAPIKey(provider.APIKey); apiKey != "" {
		return apiKey, true
	}
	if apiKey := normalizeProviderAPIKey(providerAPIKeyFromEnvName(providerAPIKeyEnvName(provider))); apiKey != "" {
		return apiKey, true
	}
	return "", false
}

func ProviderAPIKeyPreview(provider ProviderConfig) string {
	if policy := providerPolicyForConfig(provider); !policy.RequiresAPIKey {
		return policy.AuthStatusLabel
	}
	if apiKey := normalizeProviderAPIKey(provider.APIKey); apiKey != "" {
		return MaskSecret(apiKey)
	}
	envName := providerAPIKeyEnvName(provider)
	if envName == "" || normalizeProviderAPIKey(providerAPIKeyFromEnvName(envName)) == "" {
		return ""
	}
	return "env:" + envName
}

func normalizeProviderAPIKey(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'")
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		value = strings.TrimSpace(value[len("bearer "):])
	}
	fields := strings.Fields(value)
	if len(fields) <= 1 {
		return value
	}
	for _, field := range fields {
		field = strings.Trim(field, "\"'`,;")
		if strings.HasPrefix(field, "sk-") {
			return field
		}
	}
	return value
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
	policy := providers.PolicyForProvider(firstNonEmptyTrimmed(catalogID, providerID), providerType)
	if policy.Known {
		return strings.TrimSpace(policy.APIKeyEnv)
	}
	if !policy.RequiresAPIKey {
		return ""
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
	return normalizeProviderAPIKey(os.Getenv(envName))
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
	policy := providers.PolicyForProvider(providerID, "")
	if policy.Known && policy.Implemented {
		return providerOptionFromPolicy(policy), true
	}
	return ProviderOption{}, false
}

func providerPolicyForDraft(provider ProviderDraft) providers.ProviderPolicy {
	return providers.PolicyForProvider(firstNonEmptyTrimmed(provider.CatalogID, provider.ID), provider.Type)
}

func providerPolicyForConfig(provider ProviderConfig) providers.ProviderPolicy {
	return providers.PolicyForProvider(firstNonEmptyTrimmed(provider.CatalogID, provider.ID), provider.Type)
}
