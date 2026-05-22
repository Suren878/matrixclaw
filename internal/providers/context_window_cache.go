package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type contextWindowCacheFile struct {
	ContextWindows map[string]int                 `json:"context_windows"`
	ModelMetadata  map[string]cachedModelMetadata `json:"model_metadata,omitempty"`
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
	for key, value := range payload.ContextWindows {
		if strings.TrimSpace(key) != "" && value > 0 {
			contextWindowOverrides.values[key] = value
		}
	}
	contextWindowOverrides.Unlock()
	modelMetadataOverrides.Lock()
	defer modelMetadataOverrides.Unlock()
	for key, value := range payload.ModelMetadata {
		if strings.TrimSpace(key) != "" {
			modelMetadataOverrides.values[key] = mergeCachedMetadata(modelMetadataOverrides.values[key], value)
		}
	}
	for key, value := range payload.ContextWindows {
		if strings.TrimSpace(key) != "" && value > 0 {
			existing := modelMetadataOverrides.values[key]
			if existing.ContextWindow <= 0 {
				existing.ContextWindow = value
				modelMetadataOverrides.values[key] = existing
			}
		}
	}
}

func saveContextWindowCache() {
	path := contextWindowCachePath()
	if path == "" {
		return
	}
	contextWindowOverrides.RLock()
	payload := contextWindowCacheFile{
		ContextWindows: make(map[string]int, len(contextWindowOverrides.values)),
		ModelMetadata:  make(map[string]cachedModelMetadata, len(modelMetadataOverrides.values)),
	}
	for key, value := range contextWindowOverrides.values {
		if strings.TrimSpace(key) != "" && value > 0 {
			payload.ContextWindows[key] = value
		}
	}
	contextWindowOverrides.RUnlock()
	modelMetadataOverrides.RLock()
	for key, value := range modelMetadataOverrides.values {
		if strings.TrimSpace(key) != "" && cachedModelMetadataNonZero(value) {
			payload.ModelMetadata[key] = value
			if value.ContextWindow > 0 {
				payload.ContextWindows[key] = value.ContextWindow
			}
		}
	}
	modelMetadataOverrides.RUnlock()
	if len(payload.ModelMetadata) == 0 {
		payload.ModelMetadata = nil
	}
	if len(payload.ContextWindows) == 0 && len(payload.ModelMetadata) == 0 {
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
