// Package xdg resolves the XDG base directories MatrixClaw keeps its files in.
package xdg

import (
	"os"
	"path/filepath"
	"strings"
)

// StateHome is $XDG_STATE_HOME, else ~/.local/state, else the temp dir.
func StateHome() string {
	if value := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); value != "" {
		return value
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".local", "state")
	}
	return os.TempDir()
}
