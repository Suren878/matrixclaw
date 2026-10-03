package setup

import (
	"strings"
)

const TelephonyModuleID = "telephony"

func normalizeTelephonyConfig(cfg TelephonyConfig) TelephonyConfig {
	cfg.GatewayURL = strings.TrimRight(strings.TrimSpace(cfg.GatewayURL), "/")
	cfg.GatewayToken = normalizeProviderAPIKey(cfg.GatewayToken)
	cfg.DefaultProfile = slugID(cfg.DefaultProfile)
	cfg.PhonePrompt = strings.TrimSpace(cfg.PhonePrompt)
	return cfg
}
