package clientcmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

	appsetup "github.com/Suren878/matrixclaw/internal/setup"
)

func handleSetupReadError(stderr io.Writer, binaryName string, service *appsetup.Service, command string, err error) int {
	if errors.Is(err, appsetup.ErrConfigNotFound) {
		_, _ = fmt.Fprintf(stderr, "%s: setup not found at %s\n", binaryName, service.Path())
		_, _ = fmt.Fprintf(stderr, "%s: run `%s setup` first\n", binaryName, binaryName)
		return 1
	}
	if errors.Is(err, appsetup.ErrUnsupportedConfigVersion) {
		_, _ = fmt.Fprintf(stderr, "%s: setup at %s uses an unsupported version\n", binaryName, service.Path())
		_, _ = fmt.Fprintf(stderr, "%s: reopen `%s setup` to recreate the setup file\n", binaryName, binaryName)
		return 1
	}
	_, _ = fmt.Fprintf(stderr, "%s: %s: %v\n", binaryName, command, err)
	return 1
}

func nonEmpty(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func redactSecrets(text string, secrets ...string) string {
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if len(secret) < 4 {
			continue
		}
		text = strings.ReplaceAll(text, secret, appsetup.MaskSecret(secret))
	}
	return text
}

func activeProviderInfo(cfg appsetup.Config) (string, string) {
	provider, ok := cfg.ActiveProvider()
	if !ok {
		return "", ""
	}
	provider = provider.Effective()
	return provider.Name, provider.Model
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
