package controlplane

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

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
