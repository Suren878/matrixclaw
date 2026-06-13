package setup

import (
	"fmt"
	"net/url"
	"strings"
)

const TelephonyModuleID = "telephony"

func (s *Service) TelephonyModule() (TelephonyModuleDescriptor, error) {
	cfg, err := s.Load()
	if err != nil {
		return TelephonyModuleDescriptor{}, err
	}
	return TelephonyModuleFromConfig(cfg.Modules), nil
}

func (s *Service) UpdateTelephonyModule(update TelephonyModuleUpdate) (TelephonyModuleDescriptor, error) {
	cfg, err := s.Load()
	if err != nil {
		return TelephonyModuleDescriptor{}, err
	}
	merged := mergeTelephonyConfig(cfg.Modules.Telephony, update)
	if err := validateTelephonyConfig(merged); err != nil {
		return TelephonyModuleDescriptor{}, err
	}
	cfg.Modules.Telephony = normalizeTelephonyConfig(merged)
	if err := s.store.Save(cfg); err != nil {
		return TelephonyModuleDescriptor{}, err
	}
	return TelephonyModuleFromConfig(cfg.Modules), nil
}

func TelephonyModuleFromConfig(modules ModulesConfig) TelephonyModuleDescriptor {
	cfg := normalizeTelephonyConfig(modules.Telephony)
	status := telephonyConfigStatus(cfg)
	descriptorConfig := cfg
	descriptorConfig.GatewayToken = ""
	return TelephonyModuleDescriptor{
		ID:               TelephonyModuleID,
		Title:            "Telephony",
		Enabled:          cfg.Enabled,
		Status:           status,
		GatewayURL:       cfg.GatewayURL,
		TokenConfigured:  strings.TrimSpace(cfg.GatewayToken) != "",
		TokenPreview:     MaskSecret(cfg.GatewayToken),
		DefaultProfile:   cfg.DefaultProfile,
		RealtimeModuleID: VoiceModuleRealtime,
		Config:           descriptorConfig,
	}
}

func normalizeTelephonyConfig(cfg TelephonyConfig) TelephonyConfig {
	cfg.GatewayURL = strings.TrimRight(strings.TrimSpace(cfg.GatewayURL), "/")
	cfg.GatewayToken = normalizeProviderAPIKey(cfg.GatewayToken)
	cfg.DefaultProfile = normalizeTelephonyProfileID(cfg.DefaultProfile)
	cfg.PhonePrompt = strings.TrimSpace(cfg.PhonePrompt)
	return cfg
}

func mergeTelephonyConfig(existing TelephonyConfig, update TelephonyModuleUpdate) TelephonyConfig {
	merged := normalizeTelephonyConfig(existing)
	if update.Enabled != nil {
		merged.Enabled = *update.Enabled
	}
	if strings.TrimSpace(update.GatewayURL) != "" {
		merged.GatewayURL = update.GatewayURL
	}
	if strings.TrimSpace(update.GatewayToken) != "" {
		merged.GatewayToken = update.GatewayToken
	}
	if strings.TrimSpace(update.DefaultProfile) != "" {
		merged.DefaultProfile = update.DefaultProfile
	}
	if strings.TrimSpace(update.PhonePrompt) != "" {
		merged.PhonePrompt = update.PhonePrompt
	}
	if update.ClearToken || strings.TrimSpace(update.GatewayToken) == "-" {
		merged.GatewayToken = ""
	}
	if strings.TrimSpace(update.GatewayURL) == "-" {
		merged.GatewayURL = ""
	}
	if strings.TrimSpace(update.DefaultProfile) == "-" {
		merged.DefaultProfile = ""
	}
	if strings.TrimSpace(update.PhonePrompt) == "-" {
		merged.PhonePrompt = ""
	}
	return normalizeTelephonyConfig(merged)
}

func validateTelephonyConfig(cfg TelephonyConfig) error {
	cfg = normalizeTelephonyConfig(cfg)
	if cfg.GatewayURL == "" {
		if cfg.Enabled {
			return fmt.Errorf("telephony gateway URL is required")
		}
		return nil
	}
	parsed, err := url.Parse(cfg.GatewayURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("telephony gateway URL must be absolute")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("telephony gateway URL must start with http:// or https://")
	}
	return nil
}

func telephonyConfigStatus(cfg TelephonyConfig) string {
	cfg = normalizeTelephonyConfig(cfg)
	if !cfg.Enabled {
		if cfg.GatewayURL != "" {
			return "Disabled"
		}
		return "Not configured"
	}
	if cfg.GatewayURL == "" {
		return "Gateway URL required"
	}
	return "Configured"
}

func normalizeTelephonyProfileID(value string) string {
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
