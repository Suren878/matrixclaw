package setup

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

// Validate checks the config's structure; it reads no environment and makes
// no network calls.
func (cfg Config) Validate() error {
	if timezone := strings.TrimSpace(cfg.Daemon.Timezone); timezone != "" {
		if _, err := time.LoadLocation(timezone); err != nil {
			return fmt.Errorf("invalid daemon timezone %q", timezone)
		}
	}
	if telegram := cfg.Clients.Telegram; telegram.Enabled {
		if strings.TrimSpace(telegram.BotToken) == "" {
			return errors.New("telegram bot token is required when Telegram is enabled")
		}
		if strings.TrimSpace(telegram.AllowedUserID) == "" {
			return errors.New("telegram allowed user id is required when Telegram is enabled")
		}
		if _, err := strconv.ParseInt(strings.TrimSpace(telegram.AllowedUserID), 10, 64); err != nil {
			return errors.New("telegram allowed user id must be numeric")
		}
	}
	seen := map[string]bool{}
	for _, provider := range cfg.Providers {
		if err := provider.validate(); err != nil {
			return err
		}
		id := providers.CanonicalProviderID(provider.ID)
		if seen[id] {
			return fmt.Errorf("duplicate provider %q", provider.ID)
		}
		seen[id] = true
	}
	if cfg.ActiveProviderID != "" {
		if _, ok := cfg.ActiveProvider(); !ok {
			return errors.New("active provider is not configured")
		}
	}
	return nil
}

func (s *Service) EnsureDaemonAPIToken() (Config, error) {
	return s.Update(func(cfg *Config) error {
		token, err := existingOrNewAPIToken(cfg.Daemon.APIToken)
		if err != nil {
			return fmt.Errorf("generate api token: %w", err)
		}
		cfg.Daemon.APIToken = token
		return nil
	})
}

func existingOrNewAPIToken(existing string) (string, error) {
	if token := strings.TrimSpace(existing); token != "" {
		return token, nil
	}
	return generateAPIToken()
}

func generateAPIToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func defaultHTTPAddr() string {
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_HTTP_ADDR")); value != "" {
		return value
	}
	return firstAvailableLoopbackHTTPAddr()
}

func defaultDBPath() string {
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_DB_PATH")); value != "" {
		return value
	}

	return filepath.Join(defaultStateDir(), "matrixclaw", "matrixclaw.db")
}

func DefaultDBPath() string {
	return defaultDBPath()
}

func defaultStateDir() string {
	if value := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".local", "state")
	}
	return os.TempDir()
}

func defaultTimezone() string {
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_TIMEZONE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("TZ")); value != "" {
		return value
	}
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		if value := strings.TrimSpace(string(data)); value != "" {
			return value
		}
	}
	return "UTC"
}

func sameProvider(left string, right string) bool {
	return providers.CanonicalProviderID(left) == providers.CanonicalProviderID(right)
}
