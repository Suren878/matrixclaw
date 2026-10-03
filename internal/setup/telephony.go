package setup

import (
	"fmt"
	"net/url"
	"strings"
)

const TelephonyModuleID = "telephony"

func (s *Service) UpdateTelephonyModule(update TelephonyModuleUpdate) (TelephonyModuleDescriptor, error) {
	cfg, err := s.Update(func(cfg *Config) error {
		merged := mergeTelephonyConfig(cfg.Modules.Telephony, update)
		if err := validateTelephonyConfig(merged); err != nil {
			return err
		}
		cfg.Modules.Telephony = normalizeTelephonyConfig(merged)
		return nil
	})
	if err != nil {
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
		ID:              TelephonyModuleID,
		Title:           "Telephony",
		Enabled:         cfg.Enabled,
		Status:          status,
		GatewayURL:      cfg.GatewayURL,
		TokenConfigured: strings.TrimSpace(cfg.GatewayToken) != "",
		TokenPreview:    MaskSecret(cfg.GatewayToken),
		DefaultProfile:  cfg.DefaultProfile,
		Config:          descriptorConfig,
	}
}

func normalizeTelephonyConfig(cfg TelephonyConfig) TelephonyConfig {
	cfg.GatewayURL = strings.TrimRight(strings.TrimSpace(cfg.GatewayURL), "/")
	cfg.GatewayToken = normalizeProviderAPIKey(cfg.GatewayToken)
	cfg.DefaultProfile = slugID(cfg.DefaultProfile)
	cfg.PhonePrompt = strings.TrimSpace(cfg.PhonePrompt)
	return cfg
}

func mergeTelephonyConfig(existing TelephonyConfig, update TelephonyModuleUpdate) TelephonyConfig {
	merged := normalizeTelephonyConfig(existing)
	if update.Enabled != nil {
		merged.Enabled = *update.Enabled
	}
	setIfPresent(&merged.GatewayURL, update.GatewayURL)
	setIfPresent(&merged.GatewayToken, update.GatewayToken)
	setIfPresent(&merged.DefaultProfile, update.DefaultProfile)
	setIfPresent(&merged.PhonePrompt, update.PhonePrompt)
	return normalizeTelephonyConfig(merged)
}

func setIfPresent(field *string, value *string) {
	if value != nil {
		*field = *value
	}
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
