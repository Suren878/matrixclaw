package setup

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func SummaryFromConfig(cfg Config) Summary {
	active, _ := ActiveProviderConfig(cfg)
	return Summary{
		Assistant: AssistantSummary{
			Name:   firstNonEmptyStatus(cfg.Assistant.Name, "matrixclaw"),
			Status: assistantStatus(cfg.Assistant),
		},
		Provider: ProviderSummary{
			ID:            active.ID,
			Name:          active.Name,
			Model:         active.Model,
			Status:        providerStatus(providerConfigHasAPIKey(active)),
			APIKeyPreview: ProviderAPIKeyPreview(active),
		},
		Daemon: DaemonSummary{
			Status:        daemonStatus(cfg.Daemon.HTTPAddr, cfg.Daemon.DBPath),
			HTTPAddr:      cfg.Daemon.HTTPAddr,
			DBPath:        cfg.Daemon.DBPath,
			Timezone:      cfg.Daemon.Timezone,
			Autostart:     cfg.Daemon.AutostartOnBoot,
			RuntimeStatus: "Unknown",
		},
		Telegram: TelegramSummary{
			Status: telegramStatus(
				cfg.Clients.Telegram.Enabled,
				cfg.Clients.Telegram.BotToken,
				cfg.Clients.Telegram.AllowedUserID,
			),
		},
	}
}

func SummaryFromDraft(d Draft) Summary {
	active, ok := FindProviderDraft(d, d.ActiveProviderID)
	if !ok || !ProviderDraftConfigured(active) {
		active = ProviderDraft{}
		ok = false
	}
	if !ok {
		for _, provider := range ConfiguredProviders(d) {
			active = provider
			ok = true
			break
		}
	}
	name := ""
	if ok {
		name = active.Name
	}

	return Summary{
		Assistant: AssistantSummary{
			Name:   firstNonEmptyStatus(d.AssistantName, "matrixclaw"),
			Status: assistantDraftStatus(d),
		},
		Provider: ProviderSummary{
			ID:            active.ID,
			Name:          name,
			Model:         active.Model,
			Status:        providerStatus(ProviderDraftConfigured(active)),
			APIKeyPreview: currentDraftAPIKeyPreview(active),
		},
		Daemon: DaemonSummary{
			Status:        daemonStatus(d.HTTPAddr, d.DBPath),
			HTTPAddr:      d.HTTPAddr,
			DBPath:        d.DBPath,
			Timezone:      d.Timezone,
			Autostart:     ParseBool(d.AutostartOnBoot),
			RuntimeStatus: "Not applied",
		},
		Telegram: TelegramSummary{
			Status: telegramStatus(
				ParseBool(d.TelegramEnabled),
				d.TelegramBotToken,
				d.TelegramAllowedUID,
			),
		},
	}
}

func MaskSecret(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= 4 {
		return "****"
	}
	return "****" + string(runes[len(runes)-4:])
}

func providerStatus(configured bool) string {
	if configured {
		return "Configured"
	}
	return "Not configured"
}

func daemonStatus(httpAddr string, dbPath string) string {
	if strings.TrimSpace(httpAddr) == "" || strings.TrimSpace(dbPath) == "" {
		return "Not configured"
	}
	return "Configured"
}

func telegramStatus(enabled bool, token string, allowedUserID string) string {
	if !enabled {
		return "Disabled"
	}
	if strings.TrimSpace(token) == "" || strings.TrimSpace(allowedUserID) == "" {
		return "Incomplete"
	}
	return "Configured"
}

func assistantStatus(assistant AssistantConfig) string {
	if strings.TrimSpace(assistant.SystemPrompt) == "" {
		return "Incomplete"
	}
	if strings.TrimSpace(assistant.CustomInstructions) != "" {
		return "Configured · Custom"
	}
	return "Configured"
}

func assistantDraftStatus(d Draft) string {
	if strings.TrimSpace(d.AssistantSystemPrompt) == "" {
		return "Incomplete"
	}
	if strings.TrimSpace(d.AssistantCustomPrompt) != "" {
		return "Configured · Custom"
	}
	return "Configured"
}

func firstNonEmptyStatus(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func currentDraftAPIKeyPreview(provider ProviderDraft) string {
	if providers.NormalizeProviderType(provider.Type) == providers.TypeOpenAICodex {
		return "OAuth"
	}
	if provider.HasStoredAPIKey {
		return provider.StoredAPIKeyPreview
	}
	if strings.TrimSpace(provider.APIKey) != "" {
		return MaskSecret(provider.APIKey)
	}
	envName := providerDraftAPIKeyEnvName(provider)
	if envName != "" && strings.TrimSpace(providerAPIKeyFromEnvName(envName)) != "" {
		return "env:" + envName
	}
	return ""
}

func providerConfigHasAPIKey(provider ProviderConfig) bool {
	if strings.TrimSpace(provider.Type) == providers.TypeOpenAICodex {
		return strings.TrimSpace(provider.Model) != ""
	}
	_, ok := ResolvedProviderAPIKey(provider)
	return ok
}
