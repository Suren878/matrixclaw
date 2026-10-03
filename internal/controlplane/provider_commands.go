package controlplane

import (
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func controlplaneCommand(parts ...string) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if len(values) == 0 {
		return ""
	}
	return "/" + strings.Join(values, " ")
}

func providerCommand(parts ...string) string {
	return controlplaneCommand(append([]string{"provider"}, parts...)...)
}

func customProviderCommand(parts ...string) string {
	return controlplaneCommand(append([]string{"provider", "custom"}, parts...)...)
}

// providerEncodedID keeps a provider id one command token.
func providerEncodedID(providerID string) string {
	return url.QueryEscape(strings.TrimSpace(providerID))
}

func decodeProviderID(value string) (string, error) {
	decoded, err := url.QueryUnescape(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(decoded), nil
}

func providerEditCommand(providerID string) string {
	return providerCommand("edit", providerEncodedID(providerID))
}

func providerKeyCommandPrefix(providerID string) string {
	return providerCommand("key", providerEncodedID(providerID)) + " "
}

func firstField(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// customProviderID turns a custom provider's name into its id.
func customProviderID(name string) string {
	var out []rune
	lastDash := false
	for _, r := range providers.NormalizeProviderID(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			lastDash = false
		case !lastDash:
			out = append(out, '-')
			lastDash = true
		}
	}
	return strings.Trim(string(out), "-")
}

// clearableInput reads a typed setting: empty changes nothing, "-" clears it.
func clearableInput(value string) *string {
	value = strings.TrimSpace(value)
	switch value {
	case "":
		return nil
	case "-":
		value = ""
	}
	return &value
}
