package setup

import "strings"

func SummaryFromConfig(cfg Config) Summary {
	stored, configured := cfg.ActiveProvider()
	active, hasKey := stored.Runtime()
	preview := stored.APIKeyPreview()
	if !configured {
		active, preview = ProviderConfig{}, ""
	}
	return Summary{
		Assistant: AssistantSummary{
			Name:   firstNonEmptyTrimmed(cfg.Assistant.Name, "matrixclaw"),
			Status: assistantStatus(cfg.Assistant),
		},
		Provider: ProviderSummary{
			ID:            active.ID,
			Name:          active.Name,
			Model:         active.Model,
			Status:        providerStatus(configured && hasKey),
			APIKeyPreview: preview,
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
	if strings.TrimSpace(assistant.CustomInstructions) != "" {
		return "Configured · Custom"
	}
	return "Configured"
}
