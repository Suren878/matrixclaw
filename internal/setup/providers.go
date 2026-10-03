package setup

import (
	"os"
	"sort"
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
	modules.Telephony = normalizeTelephonyConfig(modules.Telephony)
	modules.MCP = normalizeMCPConfig(modules.MCP)
	modules.Browser = normalizeBrowserConfig(modules.Browser)
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

func normalizeMCPConfig(cfg MCPConfig) MCPConfig {
	servers := make([]MCPServerConfig, 0, len(cfg.Servers))
	seen := map[string]struct{}{}
	for _, server := range cfg.Servers {
		server.ID = slugID(server.ID)
		server.Name = strings.TrimSpace(server.Name)
		server.Transport = normalizeMCPTransport(server.Transport)
		server.Command = strings.TrimSpace(server.Command)
		server.Endpoint = strings.TrimRight(strings.TrimSpace(server.Endpoint), "/")
		server.ToolPrefix = slugID(server.ToolPrefix)
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

// slugID lowercases value and joins its letter and digit runs with "_".
func slugID(value string) string {
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
	assistant.SystemPrompt = userSystemPrompt(assistant.SystemPrompt)
	assistant.CustomInstructions = strings.TrimSpace(assistant.CustomInstructions)
	if assistant.Name == "" {
		assistant.Name = "matrixclaw"
	}
	return assistant
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

func providerAPIKeyFromEnvName(envName string) string {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return ""
	}
	return normalizeProviderAPIKey(os.Getenv(envName))
}
