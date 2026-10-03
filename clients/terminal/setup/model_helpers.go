package setup

import (
	"slices"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func cloneConfig(cfg setup.Config) setup.Config {
	cfg.Providers = slices.Clone(cfg.Providers)
	return cfg
}
