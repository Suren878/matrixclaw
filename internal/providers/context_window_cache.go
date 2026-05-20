package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type contextWindowCacheFile struct {
	ContextWindows map[string]int `json:"context_windows"`
}

func loadContextWindowCache() {
	path := contextWindowCachePath()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return
	}
	var payload contextWindowCacheFile
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	contextWindowOverrides.Lock()
	defer contextWindowOverrides.Unlock()
	for key, value := range payload.ContextWindows {
		if strings.TrimSpace(key) != "" && value > 0 {
			contextWindowOverrides.values[key] = value
		}
	}
}

func saveContextWindowCache() {
	path := contextWindowCachePath()
	if path == "" {
		return
	}
	contextWindowOverrides.RLock()
	payload := contextWindowCacheFile{ContextWindows: make(map[string]int, len(contextWindowOverrides.values))}
	for key, value := range contextWindowOverrides.values {
		if strings.TrimSpace(key) != "" && value > 0 {
			payload.ContextWindows[key] = value
		}
	}
	contextWindowOverrides.RUnlock()
	if len(payload.ContextWindows) == 0 {
		return
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o600)
}

func contextWindowCachePath() string {
	if value := strings.TrimSpace(os.Getenv("MATRIXCLAW_CONTEXT_WINDOW_CACHE")); value != "" {
		if value == "off" || value == "none" || value == "-" {
			return ""
		}
		return value
	}
	if strings.HasSuffix(filepath.Base(os.Args[0]), ".test") {
		return ""
	}
	return filepath.Join(defaultProviderStateDir(), "matrixclaw", "runtime", "context-windows.json")
}

func defaultProviderStateDir() string {
	if value := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".local", "state")
	}
	return os.TempDir()
}
