package setup

import (
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func cloneConfig(cfg setup.Config) setup.Config {
	cfg.Providers = slices.Clone(cfg.Providers)
	return cfg
}
