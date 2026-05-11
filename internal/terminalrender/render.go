package terminalrender

import (
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// Configure normalizes terminal rendering for all Bubble Tea/Lip Gloss clients.
func Configure() {
	env := os.Environ()
	profile := colorProfileFor(os.Stdout, env)
	applyColorEnvironment(env, profile)
	lipgloss.Writer = &colorprofile.Writer{
		Forward: os.Stdout,
		Profile: profile,
	}
}

// Environment returns terminal env vars for Bubble Tea without overstating the
// terminal color capabilities. Bubble Tea v2 down-samples colors at the output
// layer, so forcing truecolor here makes colors wrong in tmux/limited terminals.
func Environment() []string {
	env := os.Environ()
	return normalizedEnvironment(env, colorProfileFor(os.Stdout, env))
}

func ColorProfile() colorprofile.Profile {
	return colorProfileFor(os.Stdout, os.Environ())
}

func colorProfileFor(output io.Writer, env []string) colorprofile.Profile {
	if profile, ok := colorProfileOverride(env); ok {
		return profile
	}
	return colorprofile.Detect(output, env)
}

func colorProfileOverride(env []string) (colorprofile.Profile, bool) {
	value := envValue(env, "MATRIXCLAW_COLOR_PROFILE")
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "truecolor", "true-colour", "true-color", "24bit", "24-bit":
		return colorprofile.TrueColor, true
	case "256", "256color", "ansi256", "ansi-256":
		return colorprofile.ANSI256, true
	case "ansi", "16":
		return colorprofile.ANSI, true
	case "ascii", "none", "off":
		return colorprofile.Ascii, true
	default:
		return colorprofile.Unknown, false
	}
}

func applyColorEnvironment(env []string, profile colorprofile.Profile) {
	for _, entry := range normalizedEnvironment(env, profile) {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			_ = os.Setenv(key, value)
		}
	}
}

func normalizedEnvironment(env []string, profile colorprofile.Profile) []string {
	values := make(map[string]string, len(env)+2)
	order := make([]string, 0, len(env)+2)
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	set := func(key, value string) {
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	if profile == colorprofile.TrueColor {
		// Only advertise truecolor after detection/explicit override says it is
		// available. Do not force CLICOLOR_FORCE; it bypasses terminal detection.
		if strings.TrimSpace(values["COLORTERM"]) == "" {
			set("COLORTERM", "truecolor")
		}
		set("CLICOLOR", "1")
	}

	result := make([]string, 0, len(order))
	for _, key := range order {
		result = append(result, key+"="+values[key])
	}
	return result
}

func envValue(env []string, key string) string {
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if ok && k == key {
			return v
		}
	}
	return ""
}
