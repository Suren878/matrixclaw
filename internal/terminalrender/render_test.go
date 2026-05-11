package terminalrender

import (
	"bytes"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

func TestConfigureUsesConfiguredColorProfile(t *testing.T) {
	t.Setenv("MATRIXCLAW_COLOR_PROFILE", "256")

	Configure()

	if got := lipgloss.Writer.Profile; got != colorprofile.ANSI256 {
		t.Fatalf("color profile = %v, want %v", got, colorprofile.ANSI256)
	}
}

func TestColorProfileDetectsTerminalCapabilities(t *testing.T) {
	env := []string{"TERM=xterm-256color", "CLICOLOR_FORCE=1"}

	if got := colorProfileFor(&bytes.Buffer{}, env); got != colorprofile.ANSI256 {
		t.Fatalf("color profile = %v, want %v", got, colorprofile.ANSI256)
	}
}

func TestColorProfileRespectsNoColorEnvironment(t *testing.T) {
	env := []string{"TERM=xterm-256color", "TTY_FORCE=1", "NO_COLOR=1"}

	if got := colorProfileFor(&bytes.Buffer{}, env); got != colorprofile.Ascii {
		t.Fatalf("color profile = %v, want %v", got, colorprofile.Ascii)
	}
}

func TestEnvironmentDoesNotOverstateColorSupport(t *testing.T) {
	env := normalizedEnvironment([]string{"NO_COLOR=1", "TERM=dumb", "PATH=/bin"}, colorprofile.ANSI256)
	joined := "\n" + strings.Join(env, "\n") + "\n"
	if !strings.Contains(joined, "\nNO_COLOR=1\n") {
		t.Fatalf("NO_COLOR should be preserved: %v", env)
	}
	if strings.Contains(joined, "\nCOLORTERM=truecolor\n") {
		t.Fatalf("COLORTERM should not be forced for ANSI256: %v", env)
	}
	if strings.Contains(joined, "\nCLICOLOR_FORCE=") {
		t.Fatalf("CLICOLOR_FORCE should not be forced: %v", env)
	}
}

func TestEnvironmentAdvertisesTrueColorOnlyWhenDetected(t *testing.T) {
	env := normalizedEnvironment([]string{"TERM=xterm-256color"}, colorprofile.TrueColor)
	joined := "\n" + strings.Join(env, "\n") + "\n"
	if !strings.Contains(joined, "\nCOLORTERM=truecolor\n") {
		t.Fatalf("COLORTERM should be set for truecolor: %v", env)
	}
	if strings.Contains(joined, "\nCLICOLOR_FORCE=") {
		t.Fatalf("CLICOLOR_FORCE should not be forced: %v", env)
	}
}
